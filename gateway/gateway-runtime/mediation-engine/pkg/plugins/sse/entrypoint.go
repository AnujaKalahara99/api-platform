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

package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
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
	sessions  sync.Map
}

func New(name string, port int, logger *slog.Logger, packetLog *logging.PacketLogger) *Entrypoint {
	return &Entrypoint{name: name, port: port, logger: logger, packetLog: packetLog}
}

func (e *Entrypoint) Name() string { return e.name }
func (e *Entrypoint) Type() string { return "sse" }

func (e *Entrypoint) Start(ctx context.Context, manager core.SessionManager) error {
	e.manager = manager
	mux := http.NewServeMux()
	mux.HandleFunc("/", e.handleSSE)

	e.server = &http.Server{Addr: fmt.Sprintf(":%d", e.port), Handler: mux}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		e.server.Shutdown(shutdownCtx)
	}()

	e.logger.Info("sse entrypoint starting", "name", e.name, "port", e.port)
	if err := e.server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (e *Entrypoint) Stop(ctx context.Context) error {
	e.sessions.Range(func(_, val any) bool {
		sess := val.(*core.Session)
		e.manager.DestroySession(sess.ID)
		return true
	})
	if e.server != nil {
		return e.server.Shutdown(ctx)
	}
	return nil
}

func (e *Entrypoint) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clientID := uuid.New().String()
	sess, err := e.manager.CreateSession(r.Context(), e.name, clientID)
	if err != nil {
		e.logger.Error("sse session creation failed", "error", err)
		http.Error(w, "session creation failed", http.StatusInternalServerError)
		return
	}

	e.sessions.Store(clientID, sess)
	defer func() {
		e.sessions.Delete(clientID)
		e.manager.DestroySession(sess.ID)
		e.logger.Info("sse client disconnected", "client_id", clientID)
	}()

	e.logger.Info("sse client connected", "client_id", clientID, "session_id", sess.ID)

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-sess.Downstream:
			if !ok {
				return
			}
			data, err := json.Marshal(msg.Event)
			if err != nil {
				e.logger.Error("marshal sse event failed", "error", err)
				continue
			}
			fmt.Fprintf(w, "id: %s\ndata: %s\n\n", msg.Event.ID, data)
			flusher.Flush()

			if e.packetLog != nil && sess.Route != nil {
				e.packetLog.Log(msg.Event, sess.Route, "downstream")
			}
			if msg.Ack != nil {
				msg.Ack()
			}
		}
	}
}
