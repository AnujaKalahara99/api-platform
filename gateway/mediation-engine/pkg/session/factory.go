package session

import (
	"fmt"

	"mediation-engine/pkg/core"
)

// StoreType identifies the session store backend
type StoreType string

const (
	StoreTypeMemory StoreType = "memory"
	StoreTypeRedis  StoreType = "redis"
)

// Config holds session store configuration
type Config struct {
	Type  StoreType   `yaml:"type"`
	Redis RedisConfig `yaml:"redis"`
}

// NewStore creates a SessionStore based on configuration
func NewStore(cfg Config) (core.SessionStore, error) {
	switch cfg.Type {
	case StoreTypeMemory, "":
		return NewMemoryStore(), nil
	case StoreTypeRedis:
		return NewRedisStore(cfg.Redis)
	default:
		return nil, fmt.Errorf("unknown session store type: %s", cfg.Type)
	}
}
