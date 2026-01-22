package core

import (
	"context"
	"errors"
	"time"
)

const (
	SessionStateConnected    = "connected"
	SessionStateDisconnected = "disconnected"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrStoreClosed     = errors.New("session store closed")
)

// Session represents a client session with its current state
type Session struct {
	ClientID             string            `json:"client_id"`
	Identity             *ClientIdentity   `json:"identity"`
	State                string            `json:"state"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
	LastActivityAt       time.Time         `json:"last_activity_at"`
	DisconnectedAt       *time.Time        `json:"disconnected_at,omitempty"`
	ReconnectionDeadline *time.Time        `json:"reconnection_deadline,omitempty"`
	Subscriptions        []Subscription    `json:"subscriptions,omitempty"`
	Metadata             map[string]string `json:"metadata,omitempty"`
}

// NewSession creates a new session for a client identity
func NewSession(identity *ClientIdentity) *Session {
	now := time.Now().UTC()
	return &Session{
		ClientID:       identity.ID,
		Identity:       identity,
		State:          SessionStateConnected,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
		Metadata:       make(map[string]string),
	}
}

// SessionStore is the abstract interface for session persistence
type SessionStore interface {
	Create(ctx context.Context, session *Session) error
	Get(ctx context.Context, clientID string) (*Session, error)
	Update(ctx context.Context, session *Session) error
	Delete(ctx context.Context, clientID string) error
	UpdateState(ctx context.Context, clientID string, state string) error
	UpdateActivity(ctx context.Context, clientID string) error
	ListExpired(ctx context.Context, deadline time.Time) ([]*Session, error)
	Close() error
}
