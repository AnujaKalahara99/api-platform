package retention

import (
	"fmt"
	"time"

	"mediation-engine/pkg/core"
)

// DefaultConfig returns default retention configuration
func DefaultConfig() core.RetentionConfig {
	return core.RetentionConfig{
		Enabled:      false,
		Mode:         core.ModeDisconnected,
		Store:        "memory",
		TTL:          time.Hour,
		MaxPerClient: 1000,
		ClientExpiry: 24 * time.Hour,
	}
}

// NewStore creates a retention store based on configuration
func NewStore(cfg core.RetentionConfig) (core.RetentionStore, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	// Apply defaults for zero values
	if cfg.TTL == 0 {
		cfg.TTL = time.Hour
	}
	if cfg.MaxPerClient == 0 {
		cfg.MaxPerClient = 1000
	}
	if cfg.ClientExpiry == 0 {
		cfg.ClientExpiry = 24 * time.Hour
	}
	if cfg.Mode == "" {
		cfg.Mode = core.ModeDisconnected
	}

	switch cfg.Store {
	case "redis":
		if cfg.RedisAddr == "" {
			return nil, fmt.Errorf("redis_addr is required when store=redis")
		}
		return NewRedisStore(cfg.RedisAddr, cfg.TTL, cfg.MaxPerClient, cfg.ClientExpiry)

	case "memory", "":
		return NewMemoryStore(cfg.TTL, cfg.MaxPerClient, cfg.ClientExpiry), nil

	default:
		return nil, fmt.Errorf("unknown retention store type: %s", cfg.Store)
	}
}

// ParseRetentionConfigFromPolicy extracts retention config from a policy spec's config map
func ParseRetentionConfigFromPolicy(policyConfig map[string]string, globalConfig core.RetentionConfig) core.RetentionConfig {
	cfg := globalConfig // Start with global defaults

	if mode, ok := policyConfig["mode"]; ok {
		cfg.Mode = core.RetentionMode(mode)
	}

	if ttl, ok := policyConfig["ttl"]; ok {
		if d, err := time.ParseDuration(ttl); err == nil {
			cfg.TTL = d
		}
	}

	if maxStr, ok := policyConfig["max_per_client"]; ok {
		var max int
		if _, err := fmt.Sscanf(maxStr, "%d", &max); err == nil && max > 0 {
			cfg.MaxPerClient = max
		}
	}

	if expiry, ok := policyConfig["client_expiry"]; ok {
		if d, err := time.ParseDuration(expiry); err == nil {
			cfg.ClientExpiry = d
		}
	}

	return cfg
}
