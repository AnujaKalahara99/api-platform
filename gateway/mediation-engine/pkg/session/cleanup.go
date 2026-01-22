package session

import (
	"context"
	"log"
	"time"

	"mediation-engine/pkg/core"
)

// CleanupWorker periodically scans for expired sessions and cleans up resources
type CleanupWorker struct {
	store     core.SessionStore
	endpoints map[string]core.LifecycleBinder
	interval  time.Duration
	stopChan  chan struct{}
}

// NewCleanupWorker creates a new session cleanup worker
func NewCleanupWorker(store core.SessionStore, endpoints map[string]core.LifecycleBinder, interval time.Duration) *CleanupWorker {
	if interval == 0 {
		interval = 30 * time.Second
	}

	return &CleanupWorker{
		store:     store,
		endpoints: endpoints,
		interval:  interval,
		stopChan:  make(chan struct{}),
	}
}

// Start begins the cleanup worker background process
func (c *CleanupWorker) Start(ctx context.Context) {
	log.Printf("[CleanupWorker] Started with interval %s", c.interval)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[CleanupWorker] Stopping")
			return
		case <-c.stopChan:
			log.Println("[CleanupWorker] Stopped")
			return
		case <-ticker.C:
			c.cleanupExpiredSessions(ctx)
		}
	}
}

// Stop gracefully stops the cleanup worker
func (c *CleanupWorker) Stop() {
	close(c.stopChan)
}

// cleanupExpiredSessions finds and removes sessions past their reconnection deadline
func (c *CleanupWorker) cleanupExpiredSessions(ctx context.Context) {
	now := time.Now().UTC()

	expired, err := c.store.ListExpired(ctx, now)
	if err != nil {
		log.Printf("[CleanupWorker] Error listing expired sessions: %v", err)
		return
	}

	if len(expired) == 0 {
		return
	}

	log.Printf("[CleanupWorker] Found %d expired sessions", len(expired))

	for _, session := range expired {
		log.Printf("[CleanupWorker] Cleaning up session: client=%s expired_at=%s",
			session.ClientID, session.ReconnectionDeadline.Format(time.RFC3339))

		// Unbind from all endpoints
		for _, sub := range session.Subscriptions {
			if binder, exists := c.endpoints[sub.EndpointName]; exists {
				if err := binder.UnbindClient(ctx, session.ClientID); err != nil {
					log.Printf("[CleanupWorker] Failed to unbind client %s from endpoint %s: %v",
						session.ClientID, sub.EndpointName, err)
				} else {
					log.Printf("[CleanupWorker] Unbound client %s from endpoint %s",
						session.ClientID, sub.EndpointName)
				}
			}
		}

		// Delete session
		if err := c.store.Delete(ctx, session.ClientID); err != nil {
			log.Printf("[CleanupWorker] Failed to delete session for client %s: %v",
				session.ClientID, err)
		} else {
			log.Printf("[CleanupWorker] Deleted session for client %s", session.ClientID)
		}
	}
}
