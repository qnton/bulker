package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"text/template"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"gopkg.in/gomail.v2"
)

type Mail struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type Config struct {
	SMTPFrom     string
	SMTPServer   string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	RMQPort      int
	RMQHost      string
	RMQUser      string
	RMQPassword  string
	AuthKey      string
}

var (
	cfg         Config
	rmqChannel  *amqp.Channel
	retryCounts = make(map[string]int)
	mutex       sync.Mutex
)

const (
	maxRetries = 3
	rateLimit  = 1
	queueName  = "testing"
)

func initConfig() {
	cfg = Config{
		SMTPFrom:     os.Getenv("SMTP_FROM"),
		SMTPServer:   os.Getenv("SMTP_SERVER"),
		SMTPUsername: os.Getenv("SMTP_USER"),
		SMTPPassword: os.Getenv("SMTP_PASS"),
		RMQHost:      os.Getenv("RABBITMQ_HOST"),
		RMQUser:      os.Getenv("RABBITMQ_USER"),
		RMQPassword:  os.Getenv("RABBITMQ_PASS"),
		AuthKey:      os.Getenv("AUTH_KEY"),
	}

	cfg.SMTPPort, _ = strconv.Atoi(os.Getenv("SMTP_PORT"))
	cfg.RMQPort, _ = strconv.Atoi(os.Getenv("RABBITMQ_PORT"))
}

func connectRabbitMQ() *amqp.Connection {
	for retries := 0; retries < maxRetries; retries++ {
		conn, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.RMQUser, cfg.RMQPassword, cfg.RMQHost, cfg.RMQPort))
		if err == nil {
			return conn
		}
		log.Printf("Failed to connect to RabbitMQ, retrying in 10 seconds... (%d/%d)", retries+1, maxRetries)
		time.Sleep(10 * time.Second)
	}
	log.Fatalf("Failed to connect to RabbitMQ after %d attempts", maxRetries)
	return nil
}

func handleMessages(msgs <-chan amqp.Delivery, limiter <-chan time.Time) {
	for msg := range msgs {
		<-limiter

		var mail Mail
		if err := json.Unmarshal(msg.Body, &mail); err != nil {
			log.Printf("Failed to unmarshal message: %v", err)
			msg.Nack(false, true)
			continue
		}

		go retrySendMail(mail, msg)
	}
}

func retrySendMail(mail Mail, originalMsg amqp.Delivery) {
	msgBody, _ := json.Marshal(mail)
	msgStr := string(msgBody)

	mutex.Lock()
	retryCount := retryCounts[msgStr]
	mutex.Unlock()

	for retryCount <= maxRetries {
		if err := sendMail(mail); err == nil {
			mutex.Lock()
			delete(retryCounts, msgStr)
			mutex.Unlock()
			originalMsg.Ack(false)
			return
		}

		log.Printf("Failed to send mail to %s", mail.To)
		retryCount++
		mutex.Lock()
		retryCounts[msgStr] = retryCount
		mutex.Unlock()

		if retryCount > maxRetries {
			log.Printf("Failed to send mail to %s after %d attempts, giving up.", mail.To, maxRetries)
			originalMsg.Ack(false)
			return
		}

		time.Sleep(1 * time.Second)
	}
}

func sendMail(mail Mail) error {
	msg := gomail.NewMessage()
	msg.SetHeader("From", cfg.SMTPFrom)
	msg.SetHeader("To", mail.To)
	msg.SetHeader("Subject", mail.Subject)
	msg.SetBody("text/html", mail.Body)

	dialer := gomail.NewDialer(cfg.SMTPServer, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword)
	return dialer.DialAndSend(msg)
}

func handleHTTPRequests() {
	http.HandleFunc("/send-mail", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader != cfg.AuthKey {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Error reading request body", http.StatusInternalServerError)
			return
		}

		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			http.Error(w, "Failed to parse JSON", http.StatusBadRequest)
			return
		}

		recipients := data["recipient_list"].([]interface{})
		for _, recipient := range recipients {
			recipientMap := recipient.(map[string]interface{})
			subjectTemplate := data["subject"].(string)
			contentTemplate := data["content"].(string)

			subject, content, err := renderTemplates(subjectTemplate, contentTemplate, recipientMap)
			if err != nil {
				continue
			}

			to := recipientMap["to"].(string)
			sendToRabbitMQ(to, subject, content)
		}
	})

	log.Println("Starting HTTP server on :8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}
}

func renderTemplates(subjectTemplate, contentTemplate string, data map[string]interface{}) (string, string, error) {
	tmplSubject, err := template.New("subject").Parse(subjectTemplate)
	if err != nil {
		log.Printf("Failed to parse subject template: %v", err)
		return "", "", err
	}

	tmplContent, err := template.New("content").Parse(contentTemplate)
	if err != nil {
		log.Printf("Failed to parse content template: %v", err)
		return "", "", err
	}

	var subjectBuf, contentBuf bytes.Buffer
	if err := tmplSubject.Execute(&subjectBuf, data); err != nil {
		log.Printf("Failed to execute subject template: %v", err)
		return "", "", err
	}

	if err := tmplContent.Execute(&contentBuf, data); err != nil {
		log.Printf("Failed to execute content template: %v", err)
		return "", "", err
	}

	return subjectBuf.String(), contentBuf.String(), nil
}

func formatJSONString(to, subject, content string) (string, error) {
	msg := map[string]string{
		"to":      to,
		"subject": subject,
		"body":    content,
	}

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}

	return string(jsonData), nil
}

func sendToRabbitMQ(to, subject, content string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	messageBody, err := formatJSONString(to, subject, content)
	if err != nil {
		log.Printf("Failed to format JSON string: %v", err)
		return
	}

	err = rmqChannel.PublishWithContext(
		ctx,
		"",
		queueName,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         []byte(messageBody),
		},
	)

	if err != nil {
		log.Printf("Failed to publish message: %v", err)
	}
}

func main() {
	initConfig()

	connection := connectRabbitMQ()
	defer connection.Close()

	var err error
	rmqChannel, err = connection.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer rmqChannel.Close()

	queue, err := rmqChannel.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	msgs, err := rmqChannel.Consume(
		queue.Name,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	limiter := time.Tick(time.Second / rateLimit)
	go handleMessages(msgs, limiter)

	handleHTTPRequests()
}
