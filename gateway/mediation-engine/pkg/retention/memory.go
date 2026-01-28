package retention

import (
	"context"
	"log"
	"sync"
	"time"

	"mediation-engine/pkg/core"
)

// MemoryStore is an in-memory retention store implementation
type MemoryStore struct {
	entries      map[string][]core.RetentionEntry // clientID -> entries (ordered oldest-first)
	clientSeen   map[string]time.Time             // clientID -> last seen time
	ttl          time.Duration
	maxPerClient int
	clientExpiry time.Duration
	mu           sync.RWMutex
	stopCleanup  chan struct{}
}

// NewMemoryStore creates a new in-memory retention store
func NewMemoryStore(ttl time.Duration, maxPerClient int, clientExpiry time.Duration) *MemoryStore {
	store := &MemoryStore{
		entries:      make(map[string][]core.RetentionEntry),
		clientSeen:   make(map[string]time.Time),
		ttl:          ttl,
		maxPerClient: maxPerClient,
		clientExpiry: clientExpiry,
		stopCleanup:  make(chan struct{}),
	}
	go store.cleanupLoop()
	return store
}

// Store saves an event for a client
func (m *MemoryStore) Store(ctx context.Context, clientID string, entry core.RetentionEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries := m.entries[clientID]

	// Enforce max size with FIFO eviction
	if m.maxPerClient > 0 && len(entries) >= m.maxPerClient {
		evicted := entries[0]
		entries = entries[1:]
		log.Printf("[Retention] FIFO eviction for client %s, dropped event %s", clientID, evicted.Event.ID)
	}

	m.entries[clientID] = append(entries, entry)
	m.clientSeen[clientID] = time.Now()

	log.Printf("[Retention] Stored event %s for client %s (total: %d)",
		entry.Event.ID, clientID, len(m.entries[clientID]))

	return nil
}

// Retrieve gets all pending events for a client (ordered oldest-first)
func (m *MemoryStore) Retrieve(ctx context.Context, clientID string) ([]core.RetentionEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := m.entries[clientID]
	if entries == nil {
		return nil, nil
	}

	// Return a copy to avoid mutation issues
	result := make([]core.RetentionEntry, len(entries))
	copy(result, entries)
	return result, nil
}

// Acknowledge removes a successfully delivered event
func (m *MemoryStore) Acknowledge(ctx context.Context, clientID string, eventID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries := m.entries[clientID]
	for i, e := range entries {
		if e.Event.ID == eventID {
			m.entries[clientID] = append(entries[:i], entries[i+1:]...)
			log.Printf("[Retention] Acknowledged event %s for client %s", eventID, clientID)
			return nil
		}
	}

	return nil
}

// AcknowledgeAll clears all events for a client
func (m *MemoryStore) AcknowledgeAll(ctx context.Context, clientID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := len(m.entries[clientID])
	delete(m.entries, clientID)

	if count > 0 {
		log.Printf("[Retention] Acknowledged all %d events for client %s", count, clientID)
	}

	return nil
}

// Cleanup removes expired entries based on TTL and client expiry
func (m *MemoryStore) Cleanup(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	expiredEvents := 0
	expiredClients := 0

	for clientID, entries := range m.entries {
		// Check client expiry - remove entire buffer if client hasn't been seen
		if lastSeen, ok := m.clientSeen[clientID]; ok {
			if m.clientExpiry > 0 && now.Sub(lastSeen) > m.clientExpiry {
				expiredEvents += len(entries)
				delete(m.entries, clientID)
				delete(m.clientSeen, clientID)
				expiredClients++
				continue
			}
		}

		// Filter out TTL-expired events
		var valid []core.RetentionEntry
		for _, e := range entries {
			if m.ttl > 0 && now.Sub(e.Timestamp) > m.ttl {
				expiredEvents++
				continue
			}
			valid = append(valid, e)
		}

		if len(valid) == 0 {
			delete(m.entries, clientID)
		} else {
			m.entries[clientID] = valid
		}
	}

	if expiredEvents > 0 || expiredClients > 0 {
		log.Printf("[Retention] Cleanup: removed %d expired events, %d abandoned client buffers",
			expiredEvents, expiredClients)
	}

	return nil
}

// Close stops the cleanup goroutine
func (m *MemoryStore) Close() error {
	close(m.stopCleanup)
	return nil
}

// cleanupLoop periodically runs cleanup
func (m *MemoryStore) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCleanup:
			return
		case <-ticker.C:
			m.Cleanup(context.Background())
		}
	}
}

// UpdateClientSeen updates the last seen time for a client (called on connect)
func (m *MemoryStore) UpdateClientSeen(clientID string) {
	m.mu.Lock()
	m.clientSeen[clientID] = time.Now()
	m.mu.Unlock()
}

// Count returns the number of pending events for a client
func (m *MemoryStore) Count(clientID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries[clientID])
}

// TotalCount returns the total number of events across all clients
func (m *MemoryStore) TotalCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0
	for _, entries := range m.entries {
		total += len(entries)
	}
	return total
}
