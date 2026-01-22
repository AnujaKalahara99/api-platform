package core

import "context"

// IngressHub is the interface that entrypoints and endpoints use to publish events
type IngressHub interface {
	Publish(evt Event)
}

// Entrypoint accepts external traffic (e.g., WebSocket, SSE)
type Entrypoint interface {
	Name() string
	Type() string
	Start(ctx context.Context, hub IngressHub) error
	SendDownstream(ctx context.Context, evt Event) error
}

// SubscribeOptions configures per-client subscription behavior
type SubscribeOptions struct {
	Topic    string            // Override default topic (optional)
	QoS      byte              // MQTT QoS level (0, 1, 2)
	Metadata map[string]string // Additional subscription metadata
}

// Endpoint connects to backend systems (e.g., Kafka, MQTT)
type Endpoint interface {
	Name() string
	Type() string
	Start(ctx context.Context, hub IngressHub) error
	SendUpstream(ctx context.Context, evt Event) error

	// Per-client subscription management
	SubscribeForClient(ctx context.Context, clientID string, opts SubscribeOptions) error
	UnsubscribeClient(ctx context.Context, clientID string) error
}

// RoutePolicyEngine evaluates policies for a route
type RoutePolicyEngine interface {
	EvaluateWithRoute(ctx context.Context, evt Event, route *Route) (PolicyAction, *Event, error)
}
