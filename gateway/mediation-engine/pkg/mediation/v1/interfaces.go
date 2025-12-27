package v1

import "context"

// Packet is the universal data unit flowing through the engine
type Packet struct {
	SourceID string            // e.g., "ws-client-123"
	Payload  []byte            // The raw data
	Metadata map[string]string // e.g., {"topic": "sensor/data"}
}

// Entrypoint (The Front Door): Web Protocols (WS, SSE, Webhook)
// It manages many client connections.
type Entrypoint interface {
	Name() string
	// Start listens for web clients and pushes their data upstream (to Backend)
	Start(ctx context.Context, toBackend chan<- Packet) error
	// SendToClient accepts data from Backend and pushes it to web clients
	SendToClient(ctx context.Context, pkt Packet) error
}

// Endpoint (The Back Office): Messaging Protocols (MQTT, Kafka, RabbitMQ)
// It manages a persistent connection to a broker.
type Endpoint interface {
	Name() string
	// Start subscribes to the broker and pushes data downstream (to Web)
	Start(ctx context.Context, fromBackend chan<- Packet) error
	// Publish accepts data from Web and sends it to the broker
	Publish(ctx context.Context, pkt Packet) error
}
