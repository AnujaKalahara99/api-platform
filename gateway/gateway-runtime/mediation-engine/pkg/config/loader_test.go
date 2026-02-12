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
	"os"
	"path/filepath"
	"testing"

	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

func TestLoad(t *testing.T) {
	content := `
entrypoints:
  - name: ws-in
    type: websocket
    port: 8066
endpoints:
  - name: kafka-main
    type: kafka
    config:
      brokers: "localhost:9092"
routes:
  - source: ws-in
    target: kafka-main
    direction: upstream
    delivery_guarantee: at_least_once
    channel_size: 5
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Entrypoints) != 1 {
		t.Fatalf("expected 1 entrypoint, got %d", len(cfg.Entrypoints))
	}
	if cfg.Entrypoints[0].Name != "ws-in" {
		t.Fatalf("expected ws-in, got %s", cfg.Entrypoints[0].Name)
	}
	if len(cfg.Endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(cfg.Endpoints))
	}
	if len(cfg.Routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(cfg.Routes))
	}

	route := cfg.Routes[0].ToRoute()
	if route.DeliveryGuarantee != core.DeliveryAtLeastOnce {
		t.Fatalf("expected at_least_once, got %d", route.DeliveryGuarantee)
	}
	if route.ChannelSize != 5 {
		t.Fatalf("expected channel size 5, got %d", route.ChannelSize)
	}
}

func TestLoadFileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseDirection(t *testing.T) {
	if ParseDirection("downstream") != core.DirectionDownstream {
		t.Fatal("expected downstream")
	}
	if ParseDirection("upstream") != core.DirectionUpstream {
		t.Fatal("expected upstream")
	}
	if ParseDirection("") != core.DirectionUpstream {
		t.Fatal("expected default upstream")
	}
}

func TestParseDeliveryGuarantee(t *testing.T) {
	tests := []struct {
		input string
		want  core.DeliveryGuarantee
	}{
		{"none", core.DeliveryNone},
		{"at_most_once", core.DeliveryAtMostOnce},
		{"at_least_once", core.DeliveryAtLeastOnce},
		{"auto", core.DeliveryAuto},
		{"", core.DeliveryAuto},
		{"unknown", core.DeliveryAuto},
	}
	for _, tt := range tests {
		if got := ParseDeliveryGuarantee(tt.input); got != tt.want {
			t.Errorf("ParseDeliveryGuarantee(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
