package config

import (
    "os"

    "gopkg.in/yaml.v3"
)

type Config struct {
    Entrypoints []EntrypointConfig `yaml:"entrypoints"`
    Endpoints   []EndpointConfig   `yaml:"endpoints"`
    Routes      []RouteConfig      `yaml:"routes"`
    Session     SessionConfig      `yaml:"session"`
}

type EntrypointConfig struct {
    Type string `yaml:"type"`
    Name string `yaml:"name"`
    Port string `yaml:"port"`
}

type EndpointConfig struct {
    Type   string            `yaml:"type"`
    Name   string            `yaml:"name"`
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