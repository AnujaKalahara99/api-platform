package core

import "context"

// Event is the abstract unit of data.
type Event struct {
	ID       string
	SourceID string
	TargetID string
	Payload  []byte
	Metadata map[string]string
	Context  context.Context
}

// Route represents a configured route with policies
type Route struct {
	Source      string
	Destination string
	Policies    []PolicySpec
}

// PolicySpec defines a policy configuration
type PolicySpec struct {
	Name   string
	Type   string
	Config map[string]string
}

// PolicyAction tells the Engine what to do with the Event
type PolicyAction int

const (
	ActionAllow PolicyAction = iota
	ActionBlock
	ActionDrop
	ActionTransform
)

// PolicyEngine is the interface for policy evaluation
type PolicyEngine interface {
	Evaluate(ctx context.Context, evt Event) (PolicyAction, error)
}

// RoutePolicyEngine extends PolicyEngine with route-aware evaluation
type RoutePolicyEngine interface {
	PolicyEngine
	EvaluateWithRoute(ctx context.Context, evt Event, route *Route) (PolicyAction, *Event, error)
}

// Policy represents a single executable policy
type Policy interface {
	Name() string
	Type() string
	Execute(ctx context.Context, evt *Event, config map[string]string) (PolicyAction, error)
}
