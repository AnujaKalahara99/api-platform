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

package ws

import (
	"context"
	// "encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/internal/logging"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

type Entrypoint struct {
	name      string
	port      int
	upgrader  websocket.Upgrader
	manager   core.SessionManager
	server    *http.Server
	logger    *slog.Logger
	packetLog *logging.PacketLogger
	sessions  sync.Map
}

func New(name string, port int, logger *slog.Logger, packetLog *logging.PacketLogger) *Entrypoint {
	return &Entrypoint{
		name: name,
		port: port,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		logger:    logger,
		packetLog: packetLog,
	}
}

func (e *Entrypoint) Name() string { return e.name }
func (e *Entrypoint) Type() string { return "websocket" }

func (e *Entrypoint) Start(ctx context.Context, manager core.SessionManager) error {
	e.manager = manager

	mux := http.NewServeMux()
	mux.HandleFunc("/", e.handleConnection)

	e.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", e.port),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		e.server.Shutdown(shutdownCtx)
	}()

	e.logger.Info("websocket entrypoint starting", "name", e.name, "port", e.port)
	if err := e.server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (e *Entrypoint) Stop(ctx context.Context) error {
	e.sessions.Range(func(_, val any) bool {
		sess := val.(*core.Session)
		e.manager.DestroySession(sess.ClientID)
		return true
	})
	if e.server != nil {
		return e.server.Shutdown(ctx)
	}
	return nil
}

func (e *Entrypoint) handleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := e.upgrader.Upgrade(w, r, nil)
	if err != nil {
		e.logger.Error("ws upgrade failed", "error", err)
		return
	}

	clientID := core.GenerateClientID(r)

	sess, err := e.manager.CreateSession(r.Context(), e.name, clientID)
	if err != nil {
		e.logger.Error("session creation failed", "client_id", clientID, "error", err)
		conn.Close()
		return
	}

	e.sessions.Store(clientID, sess)

	defer func() {
		conn.Close()
		e.sessions.Delete(clientID)
		e.manager.DestroySession(clientID)
		e.logger.Info("ws client disconnected", "client_id", clientID)
	}()

	e.logger.Info("ws client connected", "client_id", clientID)

	go e.downstreamLoop(conn, sess)
	e.upstreamLoop(conn, sess)
}

func (e *Entrypoint) downstreamLoop(conn *websocket.Conn, sess *core.Session) {
	for msg := range sess.Downstream {
		data := msg.Event.Payload
		// data, err := json.Marshal(msg.Event)
		// if err != nil {
		// 	e.logger.Error("marshal downstream event failed", "client_id", sess.ClientID, "error", err)
		// 	continue
		// }
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			e.logger.Error("ws write failed", "client_id", sess.ClientID, "error", err)
			return
		}
		if e.packetLog != nil && sess.Route != nil {
			e.packetLog.Log(msg.Event, sess.Route, "downstream")
		}
		if msg.Ack != nil {
			if err := msg.Ack(); err != nil {
				e.logger.Warn("ack failed", "client_id", sess.ClientID, "error", err)
			}
		}
	}
}

func (e *Entrypoint) upstreamLoop(conn *websocket.Conn, sess *core.Session) {
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				e.logger.Error("ws read error", "client_id", sess.ClientID, "error", err)
			}
			return
		}

		evt := core.Event{
			ID:        uuid.New().String(),
			SourceID:  e.name,
			ClientID:  sess.ClientID,
			Payload:   payload,
			Metadata:  make(map[string]string),
			Timestamp: time.Now().UTC(),
			Type:      core.EventTypeData,
		}

		select {
		case sess.Upstream <- evt:
		default:
			e.logger.Warn("upstream channel full, dropping event", "client_id", sess.ClientID)
		}
	}
}
