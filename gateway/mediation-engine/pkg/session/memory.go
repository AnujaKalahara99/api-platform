package session

import (
	"context"
	"sync"
	"time"

	"mediation-engine/pkg/core"
)

// MemoryStore is an in-memory implementation of SessionStore
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*core.Session
	closed   bool
}

// NewMemoryStore creates a new in-memory session store
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[string]*core.Session),
	}
}

func (m *MemoryStore) Create(ctx context.Context, session *core.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return core.ErrStoreClosed
	}
	session.UpdatedAt = time.Now().UTC()
	m.sessions[session.ClientID] = session
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, clientID string) (*core.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, core.ErrStoreClosed
	}
	session, exists := m.sessions[clientID]
	if !exists {
		return nil, core.ErrSessionNotFound
	}
	return session, nil
}

func (m *MemoryStore) Update(ctx context.Context, session *core.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return core.ErrStoreClosed
	}
	if _, exists := m.sessions[session.ClientID]; !exists {
		return core.ErrSessionNotFound
	}
	session.UpdatedAt = time.Now().UTC()
	m.sessions[session.ClientID] = session
	return nil
}

func (m *MemoryStore) Delete(ctx context.Context, clientID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return core.ErrStoreClosed
	}
	delete(m.sessions, clientID)
	return nil
}

func (m *MemoryStore) UpdateState(ctx context.Context, clientID string, state string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return core.ErrStoreClosed
	}
	session, exists := m.sessions[clientID]
	if !exists {
		return core.ErrSessionNotFound
	}
	session.State = state
	session.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *MemoryStore) UpdateActivity(ctx context.Context, clientID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return core.ErrStoreClosed
	}
	session, exists := m.sessions[clientID]
	if !exists {
		return core.ErrSessionNotFound
	}
	session.LastActivityAt = time.Now().UTC()
	return nil
}

func (m *MemoryStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.sessions = nil
	return nil
}
