package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"mediation-engine/pkg/core"

	"github.com/redis/go-redis/v9"
)

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Addr      string        `yaml:"addr"`
	Password  string        `yaml:"password"`
	DB        int           `yaml:"db"`
	KeyPrefix string        `yaml:"keyPrefix"`
	TTL       time.Duration `yaml:"ttl"`
}

// RedisStore implements SessionStore using Redis
type RedisStore struct {
	client    *redis.Client
	keyPrefix string
	ttl       time.Duration
}

// NewRedisStore creates a new Redis-backed session store
func NewRedisStore(cfg RedisConfig) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	keyPrefix := cfg.KeyPrefix
	if keyPrefix == "" {
		keyPrefix = "mediation:session:"
	}

	ttl := cfg.TTL
	if ttl == 0 {
		ttl = 24 * time.Hour
	}

	return &RedisStore{
		client:    client,
		keyPrefix: keyPrefix,
		ttl:       ttl,
	}, nil
}

func (r *RedisStore) key(clientID string) string {
	return r.keyPrefix + clientID
}

func (r *RedisStore) Create(ctx context.Context, session *core.Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	return r.client.Set(ctx, r.key(session.ClientID), data, r.ttl).Err()
}

func (r *RedisStore) Get(ctx context.Context, clientID string) (*core.Session, error) {
	data, err := r.client.Get(ctx, r.key(clientID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, core.ErrSessionNotFound
		}
		return nil, err
	}
	var session core.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *RedisStore) Update(ctx context.Context, session *core.Session) error {
	session.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.key(session.ClientID), data, redis.KeepTTL).Err()
}

func (r *RedisStore) Delete(ctx context.Context, clientID string) error {
	return r.client.Del(ctx, r.key(clientID)).Err()
}

func (r *RedisStore) UpdateState(ctx context.Context, clientID string, state string) error {
	session, err := r.Get(ctx, clientID)
	if err != nil {
		return err
	}
	session.State = state
	return r.Update(ctx, session)
}

func (r *RedisStore) UpdateActivity(ctx context.Context, clientID string) error {
	session, err := r.Get(ctx, clientID)
	if err != nil {
		return err
	}
	session.LastActivityAt = time.Now().UTC()
	return r.Update(ctx, session)
}

func (r *RedisStore) ListExpired(ctx context.Context, deadline time.Time) ([]*core.Session, error) {
	var expired []*core.Session
	var cursor uint64
	pattern := r.keyPrefix + "*"

	for {
		keys, nextCursor, err := r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("scan keys: %w", err)
		}

		for _, key := range keys {
			data, err := r.client.Get(ctx, key).Bytes()
			if err != nil {
				continue // Key might have expired between SCAN and GET
			}

			var session core.Session
			if err := json.Unmarshal(data, &session); err != nil {
				continue
			}

			if session.ReconnectionDeadline != nil && session.ReconnectionDeadline.Before(deadline) {
				expired = append(expired, &session)
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return expired, nil
}

func (r *RedisStore) Close() error {
	return r.client.Close()
}
