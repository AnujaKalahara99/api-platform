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

package httpget

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

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
func (e *Entrypoint) Type() string { return "http_get" }

func (e *Entrypoint) Start(ctx context.Context, manager core.SessionManager) error {
	e.manager = manager
	mux := http.NewServeMux()
	mux.HandleFunc("/subscribe", e.handleSubscribe)
	mux.HandleFunc("/poll", e.handlePoll)
	mux.HandleFunc("/unsubscribe", e.handleUnsubscribe)

	e.server = &http.Server{Addr: fmt.Sprintf(":%d", e.port), Handler: mux}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		e.server.Shutdown(shutdownCtx)
	}()

	e.logger.Info("http_get entrypoint starting", "name", e.name, "port", e.port)
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

func (e *Entrypoint) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	clientID := r.Header.Get("X-Client-ID")
	if clientID == "" {
		http.Error(w, "X-Client-ID header required", http.StatusBadRequest)
		return
	}

	sess, err := e.manager.CreateSession(r.Context(), e.name, clientID)
	if err != nil {
		e.logger.Error("http_get subscribe failed", "error", err)
		http.Error(w, "subscription failed", http.StatusInternalServerError)
		return
	}

	e.sessions.Store(clientID, sess)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, `{"session_id":"%s","client_id":"%s"}`, sess.ID, clientID)
}

func (e *Entrypoint) handlePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}

	clientID := r.Header.Get("X-Client-ID")
	val, ok := e.sessions.Load(clientID)
	if !ok {
		http.Error(w, "not subscribed, call /subscribe first", http.StatusNotFound)
		return
	}

	sess := val.(*core.Session)

	timeout := 30 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	select {
	case msg, ok := <-sess.Downstream:
		if !ok {
			http.Error(w, "session closed", http.StatusGone)
			return
		}
		if e.packetLog != nil && sess.Route != nil {
			e.packetLog.Log(msg.Event, sess.Route, "downstream")
		}
		if msg.Ack != nil {
			msg.Ack()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msg.Event)
	case <-ctx.Done():
		w.WriteHeader(http.StatusNoContent)
	}
}

func (e *Entrypoint) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "DELETE required", http.StatusMethodNotAllowed)
		return
	}

	clientID := r.Header.Get("X-Client-ID")
	val, ok := e.sessions.LoadAndDelete(clientID)
	if !ok {
		http.Error(w, "not subscribed", http.StatusNotFound)
		return
	}

	sess := val.(*core.Session)
	e.manager.DestroySession(sess.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"unsubscribed"}`))
}
