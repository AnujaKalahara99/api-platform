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

package plugins

import (
	"context"
	"log/slog"
	"sync"

	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

type Registry struct {
	entrypoints map[string]core.Entrypoint
	endpoints   map[string]core.Endpoint
	healthy     map[string]bool
	logger      *slog.Logger
	mu          sync.RWMutex
}

func NewRegistry(logger *slog.Logger) *Registry {
	return &Registry{
		entrypoints: make(map[string]core.Entrypoint),
		endpoints:   make(map[string]core.Endpoint),
		healthy:     make(map[string]bool),
		logger:      logger,
	}
}

func (r *Registry) RegisterEntrypoint(e core.Entrypoint) {
	r.mu.Lock()
	r.entrypoints[e.Name()] = e
	r.mu.Unlock()
	r.logger.Info("registered entrypoint", "name", e.Name(), "type", e.Type())
}

func (r *Registry) RegisterEndpoint(e core.Endpoint) {
	r.mu.Lock()
	r.endpoints[e.Name()] = e
	r.mu.Unlock()
	r.logger.Info("registered endpoint", "name", e.Name(), "type", e.Type())
}

func (r *Registry) Entrypoints() map[string]core.Entrypoint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cp := make(map[string]core.Entrypoint, len(r.entrypoints))
	for k, v := range r.entrypoints {
		cp[k] = v
	}
	return cp
}

func (r *Registry) Endpoints() map[string]core.Endpoint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cp := make(map[string]core.Endpoint, len(r.endpoints))
	for k, v := range r.endpoints {
		cp[k] = v
	}
	return cp
}

func (r *Registry) ConnectEndpoints(ctx context.Context) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	connected := 0
	for name, ep := range r.endpoints {
		if err := ep.Connect(ctx); err != nil {
			r.logger.Error("endpoint connect failed", "name", name, "error", err)
			r.healthy[name] = false
		} else {
			r.healthy[name] = true
			connected++
		}
	}
	return connected
}

func (r *Registry) IsEndpointHealthy(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.healthy[name]
}

func (r *Registry) StartEntrypoints(ctx context.Context, manager core.SessionManager) {
	for name, ep := range r.entrypoints {
		go func(n string, e core.Entrypoint) {
			if err := e.Start(ctx, manager); err != nil {
				r.logger.Error("entrypoint failed", "name", n, "error", err)
			}
		}(name, ep)
	}
}

func (r *Registry) StopAll(ctx context.Context) {
	for name, ep := range r.entrypoints {
		r.logger.Info("stopping entrypoint", "name", name)
		ep.Stop(ctx)
	}
	for name, ep := range r.endpoints {
		r.logger.Info("stopping endpoint", "name", name)
		ep.Disconnect(ctx)
	}
}
