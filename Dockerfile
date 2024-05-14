# Base image
FROM golang:1.22 AS builder

# Set destination for COPY
WORKDIR /app

# Download Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code.
COPY . .

# Build the main sub-project
WORKDIR /app/cmd
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/bulker

# Final image
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/bin/bulker .
CMD ["./bulker"]