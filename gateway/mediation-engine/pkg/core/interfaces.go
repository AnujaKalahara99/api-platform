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

// Endpoint connects to backend systems (e.g., Kafka, MQTT)
type Endpoint interface {
	Name() string
	Type() string
	Start(ctx context.Context, hub IngressHub) error
	SendUpstream(ctx context.Context, evt Event) error
}

// LifecycleBinder manages per-client endpoint resources (subscriptions, consumers)
// Endpoints implement this to support client-bound lifecycle management
type LifecycleBinder interface {
	// BindClient creates endpoint resources for a specific client (e.g., MQTT persistent session)
	BindClient(ctx context.Context, clientID string, params map[string]string) error

	// UnbindClient cleans up endpoint resources when client session expires
	UnbindClient(ctx context.Context, clientID string) error

	// ResumeClient reactivates endpoint resources for reconnected client within window
	ResumeClient(ctx context.Context, clientID string) error
}

// RoutePolicyEngine evaluates policies for a route
type RoutePolicyEngine interface {
	EvaluateWithRoute(ctx context.Context, evt Event, route *Route) (PolicyAction, *Event, error)
}
