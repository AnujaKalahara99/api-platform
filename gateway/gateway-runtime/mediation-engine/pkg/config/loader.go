// Copyright (c) 2025, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied. See the License for the
// specific language governing permissions and limitations
// under the License.

package config

import (
	"fmt"
	"os"

	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Entrypoints []EntrypointConfig `yaml:"entrypoints"`
	Endpoints   []EndpointConfig   `yaml:"endpoints"`
	Routes      []RouteConfig      `yaml:"routes"`
}

type EntrypointConfig struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	Port int    `yaml:"port"`
}

type EndpointConfig struct {
	Name   string            `yaml:"name"`
	Type   string            `yaml:"type"`
	Config map[string]string `yaml:"config"`
}

type RouteConfig struct {
	Source            string `yaml:"source"`
	Target            string `yaml:"target"`
	Direction         string `yaml:"direction"`
	DeliveryGuarantee string `yaml:"delivery_guarantee"`
	ChannelSize       int    `yaml:"channel_size"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

func ParseDirection(s string) core.Direction {
	if s == "downstream" {
		return core.DirectionDownstream
	}
	return core.DirectionUpstream
}

func ParseDeliveryGuarantee(s string) core.DeliveryGuarantee {
	switch s {
	case "none":
		return core.DeliveryNone
	case "at_most_once":
		return core.DeliveryAtMostOnce
	case "at_least_once":
		return core.DeliveryAtLeastOnce
	default:
		return core.DeliveryAuto
	}
}

func (rc RouteConfig) ToRoute() *core.Route {
	return &core.Route{
		Source:            rc.Source,
		Target:            rc.Target,
		Direction:         ParseDirection(rc.Direction),
		DeliveryGuarantee: ParseDeliveryGuarantee(rc.DeliveryGuarantee),
		ChannelSize:       rc.ChannelSize,
	}
}
