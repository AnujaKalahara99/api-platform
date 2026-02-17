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

package httppost

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/internal/logging"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

type Entrypoint struct {
	name      string
	port      int
	manager   core.SessionManager
	server    *http.Server
	logger    *slog.Logger
	packetLog *logging.PacketLogger
	maxBody   int64
}

func New(name string, port int, logger *slog.Logger, packetLog *logging.PacketLogger) *Entrypoint {
	return &Entrypoint{
		name:      name,
		port:      port,
		logger:    logger,
		packetLog: packetLog,
		maxBody:   1 << 20,
	}
}

func (e *Entrypoint) Name() string { return e.name }
func (e *Entrypoint) Type() string { return "http_post" }

func (e *Entrypoint) Start(ctx context.Context, manager core.SessionManager) error {
	e.manager = manager
	mux := http.NewServeMux()
	mux.HandleFunc("/", e.handlePost)

	e.server = &http.Server{Addr: fmt.Sprintf(":%d", e.port), Handler: mux}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		e.server.Shutdown(shutdownCtx)
	}()

	e.logger.Info("http_post entrypoint starting", "name", e.name, "port", e.port)
	if err := e.server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (e *Entrypoint) Stop(ctx context.Context) error {
	if e.server != nil {
		return e.server.Shutdown(ctx)
	}
	return nil
}

func (e *Entrypoint) handlePost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, e.maxBody))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	clientID := core.GenerateClientID(r)

	sess, err := e.manager.CreateSession(r.Context(), e.name, clientID)
	if err != nil {
		e.logger.Error("http_post session failed", "error", err)
		http.Error(w, "session creation failed", http.StatusInternalServerError)
		return
	}
	defer e.manager.DestroySession(clientID)

	evt := core.Event{
		ID:        uuid.New().String(),
		SourceID:  e.name,
		ClientID:  clientID,
		Payload:   body,
		Metadata:  make(map[string]string),
		Timestamp: time.Now().UTC(),
		Type:      core.EventTypeData,
	}

	if e.packetLog != nil && sess.Route != nil {
		e.packetLog.Log(evt, sess.Route, "upstream")
	}

	select {
	case sess.Upstream <- evt:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"accepted"}`))
	case <-r.Context().Done():
		http.Error(w, "timeout", http.StatusGatewayTimeout)
	}
}
