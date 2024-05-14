# bulker

This project consists of a Go-based service for sending emails using an HTTP endpoint and RabbitMQ for message queuing. The service reads from a RabbitMQ queue and sends emails using the specified SMTP server. The service ensures reliable message processing, including retry mechanisms and persistence in the event of crashes.

## Features

- HTTP API to receive email data and queue it for processing.
- Uses RabbitMQ for message queuing to handle email sending asynchronously.
- SMTP integration to send emails.
- Retry logic for failed email deliveries.
- Error logging for troubleshooting.

## Prerequisites

- Go installed on your device.
- A running RabbitMQ instance.
- An SMTP server for sending emails.

## Installation

1. Clone the Repository:

```bash
git clone https://github.com/qnton/bulker
```

```bash
cd email-service
```

2. Set Up Environment Variables:

Create a `.env` file in the root directory of the project and set the following environment variables:

```bash
RABBITMQ_HOST=rabbitmq
RABBITMQ_PORT=5672
RABBITMQ_USER=guest
RABBITMQ_PASS=guest
SMTP_SERVER=email-smtp.eu-central-1.amazonaws.com
SMTP_PORT=465
SMTP_USER=user
SMTP_PASS=pass
SMTP_FROM=test@example.com
AUTH_KEY=qwerty
```

3. Example Docker Compose Setup

Create a `docker-compose.yml` file with the following content:

```yaml
services:
  rabbitmq:
    image: rabbitmq:latest
    ports:
      - "5672:5672"
    environment:
      RABBITMQ_DEFAULT_USER: guest
      RABBITMQ_DEFAULT_PASS: guest

  app:
    build: .
    ports:
      - "8080:8080"
    depends_on:
      - rabbitmq
    env_file:
      - .env
    restart: always
```

4. Build and Run the Services:

Ensure Docker and Docker Compose are installed, then use the following command to build and start the services:

```bash
docker-compose up --build
```

## Usage

### HTTP API Endpoint

The service exposes an HTTP POST endpoint at `http://localhost:8080/send-mail` to receive email data and queue it for processing.

### Example Request

```curl
curl -X POST \
  http://localhost:8080/send-mail \
  -H 'Content-Type: application/json' \
  -H 'Authorization: qwerty' \
  -d '{
	"recipient_list": [
		{"name": "Max", "to": "email1@example.com"},
		{"name": "Mustermann", "to": "email2@example.com"}
	],
	"subject": "Hello {{.name}}",
	"content": "<p>Hello <b>{{.name}}</b></p>"
}'
```
