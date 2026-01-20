package core

import "context"

// Event represents a message flowing through the mediation engine
type Event struct {
	ID       string
	SourceID string
	ClientID string // NEW: Client identifier for session tracking
	Payload  []byte
	Metadata map[string]string
}

// PolicyAction represents the result of policy evaluation
type PolicyAction int

const (
	ActionAllow PolicyAction = iota
	ActionBlock
	ActionTransform
)

// PolicySpec defines a policy configuration
type PolicySpec struct {
	Name   string
	Type   string
	Config map[string]string
}

// Route defines a routing rule from source to destination
type Route struct {
	Source      string
	Destination string
	Policies    []PolicySpec
}

// Policy represents a single executable policy
type Policy interface {
	Name() string
	Type() string
	Execute(ctx context.Context, evt *Event, config map[string]string) (PolicyAction, error)
}
