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
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

type Registry struct {
	entrypoints map[string]core.Entrypoint
	endpoints   map[string]core.Endpoint
	healthy     map[string]bool
	logger      *slog.Logger
	mu          sync.RWMutex
	server      *http.Server
	socketPath  string
}

func NewRegistry(logger *slog.Logger, socketPath string) *Registry {
	if socketPath == "" {
		socketPath = core.DefaultMediationSocketPath
	}
	return &Registry{
		entrypoints: make(map[string]core.Entrypoint),
		endpoints:   make(map[string]core.Endpoint),
		healthy:     make(map[string]bool),
		logger:      logger,
		socketPath:  socketPath,
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

func (r *Registry) SocketPath() string {
	return r.socketPath
}

func (r *Registry) StartEntrypoints(ctx context.Context, manager core.SessionManager) {
	mux := http.NewServeMux()

	// Register routes for all entrypoints
	for _, ep := range r.entrypoints {
		ep.RegisterRoutes(mux, manager)
	}

	// Health check endpoint
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	r.server = &http.Server{Handler: mux}

	// Ensure the socket directory exists
	socketDir := filepath.Dir(r.socketPath)
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		r.logger.Error("failed to create socket directory", "path", socketDir, "error", err)
		return
	}

	// Remove stale socket
	os.Remove(r.socketPath)

	ln, err := net.Listen("unix", r.socketPath)
	if err != nil {
		r.logger.Error("failed to listen on UDS", "path", r.socketPath, "error", err)
		return
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r.server.Shutdown(shutdownCtx)
	}()

	r.logger.Info("mediation engine UDS server starting", "socket", r.socketPath)
	go func() {
		if err := r.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			r.logger.Error("UDS server failed", "error", err)
		}
	}()
}

func (r *Registry) StopAll(ctx context.Context) {
	// Stop the UDS server
	if r.server != nil {
		r.logger.Info("stopping UDS server")
		r.server.Shutdown(ctx)
		os.Remove(r.socketPath)
	}
	for name, ep := range r.entrypoints {
		r.logger.Info("stopping entrypoint", "name", name)
		ep.Stop(ctx)
	}
	for name, ep := range r.endpoints {
		r.logger.Info("stopping endpoint", "name", name)
		ep.Disconnect(ctx)
	}
}
