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
	manager   core.SessionManager
	logger    *slog.Logger
	packetLog *logging.PacketLogger
	sessions  sync.Map
}

func New(name string, logger *slog.Logger, packetLog *logging.PacketLogger) *Entrypoint {
	return &Entrypoint{name: name, logger: logger, packetLog: packetLog}
}

func (e *Entrypoint) Name() string { return e.name }
func (e *Entrypoint) Type() string { return "http_get" }

func (e *Entrypoint) RegisterRoutes(mux *http.ServeMux, manager core.SessionManager) {
	e.manager = manager
	prefix := "/" + e.name + "/"
	subMux := http.NewServeMux()
	subMux.HandleFunc("/subscribe", e.handleSubscribe)
	subMux.HandleFunc("/poll", e.handlePoll)
	subMux.HandleFunc("/unsubscribe", e.handleUnsubscribe)
	mux.Handle(prefix, http.StripPrefix("/"+e.name, subMux))
	e.logger.Info("http_get entrypoint registered", "name", e.name, "prefix", prefix)
}

func (e *Entrypoint) Stop(ctx context.Context) error {
	e.sessions.Range(func(_, val any) bool {
		sess := val.(*core.Session)
		e.manager.DestroySession(sess.ClientID)
		return true
	})
	return nil
}

func (e *Entrypoint) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	clientID := core.GenerateClientID(r)

	sess, err := e.manager.CreateSession(r.Context(), e.name, clientID)
	if err != nil {
		e.logger.Error("http_get subscribe failed", "error", err)
		http.Error(w, "subscription failed", http.StatusInternalServerError)
		return
	}

	e.sessions.Store(clientID, sess)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, `{"client_id":"%s"}`, clientID)
}

func (e *Entrypoint) handlePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}

	clientID := core.GenerateClientID(r)
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

	clientID := core.GenerateClientID(r)
	_, ok := e.sessions.LoadAndDelete(clientID)
	if !ok {
		http.Error(w, "not subscribed", http.StatusNotFound)
		return
	}

	e.manager.DestroySession(clientID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"unsubscribed"}`))
}
