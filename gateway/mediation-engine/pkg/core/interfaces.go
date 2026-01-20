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

// RoutePolicyEngine evaluates policies for a route
type RoutePolicyEngine interface {
	EvaluateWithRoute(ctx context.Context, evt Event, route *Route) (PolicyAction, *Event, error)
}
