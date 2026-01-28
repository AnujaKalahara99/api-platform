package retention

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"mediation-engine/pkg/core"

	"github.com/redis/go-redis/v9"
)

const (
	// Redis key patterns
	retentionKeyPrefix = "retention:"
	clientSeenPrefix   = "retention:seen:"
)

// RedisStore is a Redis-backed retention store implementation
type RedisStore struct {
	client       *redis.Client
	ttl          time.Duration
	maxPerClient int
	clientExpiry time.Duration
	stopCleanup  chan struct{}
}

// NewRedisStore creates a new Redis-backed retention store
func NewRedisStore(addr string, ttl time.Duration, maxPerClient int, clientExpiry time.Duration) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	store := &RedisStore{
		client:       client,
		ttl:          ttl,
		maxPerClient: maxPerClient,
		clientExpiry: clientExpiry,
		stopCleanup:  make(chan struct{}),
	}

	go store.cleanupLoop()

	log.Printf("[Retention] Redis store connected to %s", addr)
	return store, nil
}

// retentionKey returns the Redis key for a client's retention queue
func (r *RedisStore) retentionKey(clientID string) string {
	return retentionKeyPrefix + clientID
}

// clientSeenKey returns the Redis key for tracking client last seen time
func (r *RedisStore) clientSeenKey(clientID string) string {
	return clientSeenPrefix + clientID
}

// Store saves an event for a client using a Redis sorted set (score = timestamp)
func (r *RedisStore) Store(ctx context.Context, clientID string, entry core.RetentionEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal entry: %w", err)
	}

	key := r.retentionKey(clientID)
	score := float64(entry.Timestamp.UnixNano())

	pipe := r.client.Pipeline()

	// Add to sorted set
	pipe.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: data,
	})

	// Update last seen
	pipe.Set(ctx, r.clientSeenKey(clientID), time.Now().Unix(), r.clientExpiry)

	// Enforce max size with FIFO eviction (keep only most recent maxPerClient entries)
	if r.maxPerClient > 0 {
		// Remove entries beyond max (oldest first)
		pipe.ZRemRangeByRank(ctx, key, 0, int64(-r.maxPerClient-1))
	}

	// Set TTL on the key itself
	if r.ttl > 0 {
		pipe.Expire(ctx, key, r.ttl)
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to store entry: %w", err)
	}

	log.Printf("[Retention] Stored event %s for client %s", entry.Event.ID, clientID)
	return nil
}

// Retrieve gets all pending events for a client (ordered oldest-first)
func (r *RedisStore) Retrieve(ctx context.Context, clientID string) ([]core.RetentionEntry, error) {
	key := r.retentionKey(clientID)

	results, err := r.client.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve entries: %w", err)
	}

	entries := make([]core.RetentionEntry, 0, len(results))
	for _, data := range results {
		var entry core.RetentionEntry
		if err := json.Unmarshal([]byte(data), &entry); err != nil {
			log.Printf("[Retention] Failed to unmarshal entry: %v", err)
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// Acknowledge removes a successfully delivered event
func (r *RedisStore) Acknowledge(ctx context.Context, clientID string, eventID string) error {
	key := r.retentionKey(clientID)

	// Retrieve all entries to find the one to remove
	entries, err := r.Retrieve(ctx, clientID)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.Event.ID == eventID {
			data, _ := json.Marshal(entry)
			if err := r.client.ZRem(ctx, key, data).Err(); err != nil {
				return fmt.Errorf("failed to acknowledge event: %w", err)
			}
			log.Printf("[Retention] Acknowledged event %s for client %s", eventID, clientID)
			return nil
		}
	}

	return nil
}

// AcknowledgeAll clears all events for a client
func (r *RedisStore) AcknowledgeAll(ctx context.Context, clientID string) error {
	key := r.retentionKey(clientID)

	count, err := r.client.Del(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to acknowledge all: %w", err)
	}

	if count > 0 {
		log.Printf("[Retention] Acknowledged all events for client %s", clientID)
	}

	return nil
}

// Cleanup removes expired entries based on TTL
// Note: Redis TTL on keys handles most expiration automatically
func (r *RedisStore) Cleanup(ctx context.Context) error {
	// For sorted sets, we can remove entries older than TTL using score
	if r.ttl == 0 {
		return nil
	}

	cutoff := float64(time.Now().Add(-r.ttl).UnixNano())

	// Scan all retention keys and remove old entries
	iter := r.client.Scan(ctx, 0, retentionKeyPrefix+"*", 100).Iterator()
	cleanedCount := 0

	for iter.Next(ctx) {
		key := iter.Val()
		// Skip client seen keys
		if len(key) > len(clientSeenPrefix) && key[:len(clientSeenPrefix)] == clientSeenPrefix {
			continue
		}

		removed, err := r.client.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%f", cutoff)).Result()
		if err != nil {
			log.Printf("[Retention] Cleanup error for %s: %v", key, err)
			continue
		}
		cleanedCount += int(removed)
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("cleanup scan error: %w", err)
	}

	if cleanedCount > 0 {
		log.Printf("[Retention] Cleanup: removed %d expired events", cleanedCount)
	}

	return nil
}

// Close stops the cleanup goroutine and closes the Redis connection
func (r *RedisStore) Close() error {
	close(r.stopCleanup)
	return r.client.Close()
}

// cleanupLoop periodically runs cleanup
func (r *RedisStore) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCleanup:
			return
		case <-ticker.C:
			r.Cleanup(context.Background())
		}
	}
}

// UpdateClientSeen updates the last seen time for a client
func (r *RedisStore) UpdateClientSeen(ctx context.Context, clientID string) error {
	return r.client.Set(ctx, r.clientSeenKey(clientID), time.Now().Unix(), r.clientExpiry).Err()
}

// Count returns the number of pending events for a client
func (r *RedisStore) Count(ctx context.Context, clientID string) (int64, error) {
	return r.client.ZCard(ctx, r.retentionKey(clientID)).Result()
}
