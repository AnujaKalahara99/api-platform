package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the top-level structure of config.yaml
type Config struct {
	Version     string            `yaml:"version"`
	Entrypoints []ComponentConfig `yaml:"entrypoints"`
	Endpoints   []ComponentConfig `yaml:"endpoints"`
	Routes      []RouteConfig     `yaml:"routes"`
}

// ComponentConfig is reused for both Entrypoints and Endpoints
type ComponentConfig struct {
	Name   string            `yaml:"name"`
	Type   string            `yaml:"type"`   // e.g., "websocket", "kafka"
	Port   string            `yaml:"port"`   // Optional (for Entrypoints)
	Config map[string]string `yaml:"config"` // Optional (for Endpoints like Broker URL)
}

// RouteConfig maps a source to a destination with optional policies
type RouteConfig struct {
	Source      string         `yaml:"source"`
	Destination string         `yaml:"destination"`
	Policies    []PolicyConfig `yaml:"policies,omitempty"`
}

// PolicyConfig defines a policy to apply on a route
type PolicyConfig struct {
	Name   string            `yaml:"name"`
	Type   string            `yaml:"type"`
	Config map[string]string `yaml:"config,omitempty"`
}

// Load reads the YAML file and unmarshals it into the Config struct
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}
