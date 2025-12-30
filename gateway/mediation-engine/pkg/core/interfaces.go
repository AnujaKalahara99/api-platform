package core

import "context"

// IngressHub is the write-only interface for components to push data INTO the engine.
type IngressHub interface {
	Publish(evt Event)
}

// Entrypoint (Source): Accepts traffic from external clients.
type Entrypoint interface {
	Name() string
	Type() string
	Start(ctx context.Context, hub IngressHub) error
	// SendDownstream sends data FROM the engine back to the client (e.g. WS Reply)
	SendDownstream(ctx context.Context, evt Event) error
}

// Endpoint (Sink): Connects to backend systems.
type Endpoint interface {
	Name() string
	Type() string
	Start(ctx context.Context, hub IngressHub) error
	// SendUpstream sends data FROM the engine to the backend (e.g. Kafka Produce)
	SendUpstream(ctx context.Context, evt Event) error
}
