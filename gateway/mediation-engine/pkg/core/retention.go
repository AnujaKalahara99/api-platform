package core

import (
	"context"
	"time"
)

// RetentionMode determines when events are stored in the retention layer
type RetentionMode string

const (
	// ModeDisconnected stores events only when the target client is offline (default)
	ModeDisconnected RetentionMode = "disconnected"
	// ModeAlways stores events before every delivery attempt, acknowledges on success
	ModeAlways RetentionMode = "always"
)

// RetentionEntry wraps an Event with retention metadata
type RetentionEntry struct {
	Event     Event     `json:"event"`
	Timestamp time.Time `json:"timestamp"`
	RouteKey  string    `json:"route_key"`
	Attempts  int       `json:"attempts"`
}

// RetentionConfig holds retention policy settings (per-route or global)
type RetentionConfig struct {
	Enabled      bool          `yaml:"enabled" json:"enabled"`
	Mode         RetentionMode `yaml:"mode" json:"mode"`
	Store        string        `yaml:"store" json:"store"`                   // "memory" or "redis"
	RedisAddr    string        `yaml:"redis_addr" json:"redis_addr"`         // Redis address if store=redis
	TTL          time.Duration `yaml:"ttl" json:"ttl"`                       // How long to retain events
	MaxPerClient int           `yaml:"max_per_client" json:"max_per_client"` // Max events per client (FIFO eviction)
	ClientExpiry time.Duration `yaml:"client_expiry" json:"client_expiry"`   // Clear buffer if client never returns
}

// RetentionStore is the interface for pluggable retention backends
type RetentionStore interface {
	Store(ctx context.Context, clientID string, entry RetentionEntry) error
	Retrieve(ctx context.Context, clientID string) ([]RetentionEntry, error)
	// Acknowledge removes a successfully delivered event
	Acknowledge(ctx context.Context, clientID string, eventID string) error
	AcknowledgeAll(ctx context.Context, clientID string) error
	// Cleanup removes expired entries based on TTL and client expiry
	Cleanup(ctx context.Context) error
	Close() error
}
