package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Entrypoints []EntrypointConfig `yaml:"entrypoints"`
	Endpoints   []EndpointConfig   `yaml:"endpoints"`
	Routes      []RouteConfig      `yaml:"routes"`
	Session     SessionConfig      `yaml:"session"`
	Retention   RetentionConfig    `yaml:"retention"`
}

type EntrypointConfig struct {
	Type string `yaml:"type"`
	Name string `yaml:"name"`
	Port string `yaml:"port"`
}

// EndpointConfig represents an endpoint configuration
type EndpointConfig struct {
	Name   string            `yaml:"name"`
	Type   string            `yaml:"type"` // "kafka", "mqtt", "mock"
	Config map[string]string `yaml:"config"`
}

type RouteConfig struct {
	Source      string         `yaml:"source"`
	Destination string         `yaml:"destination"`
	Policies    []PolicyConfig `yaml:"policies"`
}

type PolicyConfig struct {
	Name   string            `yaml:"name"`
	Type   string            `yaml:"type"`
	Config map[string]string `yaml:"config"`
}

type SessionConfig struct {
	Type  string      `yaml:"type"` // "memory" or "redis"
	Redis RedisConfig `yaml:"redis"`
}

type RedisConfig struct {
	Addr      string `yaml:"addr"`
	Password  string `yaml:"password"`
	DB        int    `yaml:"db"`
	KeyPrefix string `yaml:"keyPrefix"`
}

// RetentionConfig holds global retention settings
type RetentionConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Mode         string `yaml:"mode"`       // "disconnected" or "always"
	Store        string `yaml:"store"`      // "memory" or "redis"
	RedisAddr    string `yaml:"redis_addr"` // Redis address if store=redis
	TTL          string `yaml:"ttl"`        // Duration string e.g., "1h", "30m"
	MaxPerClient int    `yaml:"max_per_client"`
	ClientExpiry string `yaml:"client_expiry"` // Duration string e.g., "24h"
}

// ParseTTL parses the TTL string into a time.Duration
func (r *RetentionConfig) ParseTTL() time.Duration {
	if r.TTL == "" {
		return time.Hour // default
	}
	d, err := time.ParseDuration(r.TTL)
	if err != nil {
		return time.Hour
	}
	return d
}

// ParseClientExpiry parses the client expiry string into a time.Duration
func (r *RetentionConfig) ParseClientExpiry() time.Duration {
	if r.ClientExpiry == "" {
		return 24 * time.Hour // default
	}
	d, err := time.ParseDuration(r.ClientExpiry)
	if err != nil {
		return 24 * time.Hour
	}
	return d
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
