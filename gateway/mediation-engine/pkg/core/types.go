package core

import "context"

// Event is the abstract unit of data.
// The Policy Engine will inspect THIS struct to make decisions.
type Event struct {
	ID       string            // Unique Event ID (UUID)
	SourceID string            // Name of the Entrypoint/Endpoint that sent it
	TargetID string            // (Optional) Specific intended target
	Payload  []byte            // The raw data body
	Metadata map[string]string // Protocol headers normalized (e.g., "auth-token", "content-type")
	Context  context.Context   // Trace context
}

// PolicyAction tells the Engine what to do with the Event
type PolicyAction int

const (
	ActionAllow PolicyAction = iota
	ActionBlock
	ActionDrop
)

// PolicyEngine is the interface your future custom engine will implement.
// For now, we will use a default "Allow All".
type PolicyEngine interface {
	Evaluate(ctx context.Context, evt Event) (PolicyAction, error)
}
