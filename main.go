package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// SMTPConfig represents SMTP server configuration
type SMTPConfig struct {
	Server   string
	Port     string
	Username string
	Password string
}

// Mail represents email content
type Mail struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func main() {
	// Load SMTP configuration from environment variables
	smtpConfig := SMTPConfig{
		Server:   os.Getenv("SMTP_SERVER"),
		Port:     os.Getenv("SMTP_PORT"),
		Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"),
	}

	// Check if SMTP configuration is valid
	if smtpConfig.Server == "" || smtpConfig.Port == "" || smtpConfig.Username == "" || smtpConfig.Password == "" {
		log.Fatal("SMTP configuration missing or invalid")
	}

	// Connect to RabbitMQ
	conn, err := amqp.Dial("amqp://guest:guest@rabbitmq:5672/")
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer ch.Close()

	// Declare a queue
	q, err := ch.QueueDeclare(
		"email_queue", // queue name
		true,          // durable
		false,         // delete when unused
		false,         // exclusive
		false,         // no-wait
		nil,           // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	// Rate limiter to limit email sending to 10 emails per second
	limiter := time.NewTicker(time.Second / 10)
	defer limiter.Stop()

	// Mutex for synchronizing access to the rate limiter
	var mu sync.Mutex

	// HTTP server for receiving email requests
	http.HandleFunc("/send-emails", func(w http.ResponseWriter, r *http.Request) {
		// Rate limit email sending
		mu.Lock()
		<-limiter.C
		mu.Unlock()

		// Decode request body
		var mails []Mail
		err := json.NewDecoder(r.Body).Decode(&mails)
		if err != nil {
			http.Error(w, "Failed to decode request body", http.StatusBadRequest)
			return
		}

		// Enqueue emails to RabbitMQ
		for _, mail := range mails {
			err := ch.Publish("", q.Name, false, false, amqp.Publishing{
				ContentType: "text/plain",
				Body:        []byte(mail.To + "\n" + mail.Subject + "\n\n" + mail.Body),
			})
			if err != nil {
				log.Printf("Failed to publish message to RabbitMQ: %v", err)
				continue
			}
			log.Printf("Email enqueued: %s", mail.To)
		}

		w.WriteHeader(http.StatusOK)
	})

	// Start HTTP server
	go func() {
		log.Fatal(http.ListenAndServe(":8080", nil))
	}()

	log.Println("Email service started")

	// Wait for termination signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("Email service stopped")
}
