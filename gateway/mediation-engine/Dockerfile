# Stage 1: Build the Go Binary
FROM golang:1.25.5-alpine3.23 AS builder

WORKDIR /app

# Copy dependency files first to cache download layer
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the binary (targeting the main.go in cmd/server)
RUN go build -o mediation-engine cmd/server/main.go

# Stage 2: Create the Lightweight Runtime Image
FROM alpine:latest

WORKDIR /app

# Install CA certificates (Required for connecting to public TLS endpoints like MQTT/HTTPS)
RUN apk --no-cache add ca-certificates

# Copy the binary from the builder stage
COPY --from=builder /app/mediation-engine .

# Expose the WebSocket port defined in your code (ws/entrypoint.go)
EXPOSE 8066

# Run the engine
CMD ["./mediation-engine"]