package core

import (
	"context"
)

// Event types for lifecycle management
const (
	EventTypeMessage    = "message"
	EventTypeConnect    = "connect"
	EventTypeDisconnect = "disconnect"
)

// Event represents a message flowing through the mediation engine
type Event struct {
	ID       string
	Type     string // EventTypeMessage, EventTypeConnect, EventTypeDisconnect
	SourceID string
	ClientID string
	Payload  []byte
	Metadata map[string]string
}

// IsLifecycle returns true if the event is a lifecycle event
func (e Event) IsLifecycle() bool {
	return e.Type == EventTypeConnect || e.Type == EventTypeDisconnect
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
