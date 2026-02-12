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

package session

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/internal/logging"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/internal/routing"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

type activeSession struct {
	session *core.Session
	cancel  context.CancelFunc
}

type Manager struct {
	sessions  sync.Map
	routes    *routing.Table
	endpoints map[string]core.Endpoint
	logger    *slog.Logger
	packetLog *logging.PacketLogger
}

func NewManager(
	routes *routing.Table,
	endpoints map[string]core.Endpoint,
	logger *slog.Logger,
	packetLog *logging.PacketLogger,
) *Manager {
	return &Manager{
		routes:    routes,
		endpoints: endpoints,
		logger:    logger,
		packetLog: packetLog,
	}
}

func (m *Manager) CreateSession(
	ctx context.Context,
	entrypointName string,
	clientID string,
) (*core.Session, error) {
	route, ok := m.routes.Lookup(entrypointName)
	if !ok {
		return nil, fmt.Errorf("%w: source=%s", core.ErrNoRoute, entrypointName)
	}

	ep, ok := m.endpoints[route.Target]
	if !ok {
		return nil, fmt.Errorf("%w: endpoint=%s", core.ErrTargetNotFound, route.Target)
	}

	channelSize := route.ChannelSize
	if channelSize <= 0 {
		channelSize = 1
	}

	sessionCtx, sessionCancel := context.WithCancel(ctx)
	sessionID := uuid.New().String()

	sess := &core.Session{
		ID:             sessionID,
		ClientID:       clientID,
		EntrypointName: entrypointName,
		Route:          route,
		Downstream:     make(chan core.BrokerMessage, channelSize),
		Upstream:       make(chan core.Event, channelSize),
		Cancel:         sessionCancel,
	}

	m.sessions.Store(sessionID, &activeSession{
		session: sess,
		cancel:  sessionCancel,
	})

	go func() {
		defer func() {
			if r := recover(); r != nil {
				m.logger.Error("consumer panic recovered", "session_id", sessionID, "error", r)
			}
		}()
		if err := ep.StartConsumer(sessionCtx, sess, sess.Downstream); err != nil {
			if sessionCtx.Err() == nil {
				m.logger.Error("consumer error", "session_id", sessionID, "error", err)
			}
		}
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				m.logger.Error("upstream relay panic recovered", "session_id", sessionID, "error", r)
			}
		}()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case evt, ok := <-sess.Upstream:
				if !ok {
					return
				}
				if m.packetLog != nil {
					m.packetLog.Log(evt, route, "upstream")
				}
				if err := ep.Send(sessionCtx, evt); err != nil {
					if sessionCtx.Err() == nil {
						m.logger.Error("upstream send failed", "session_id", sessionID, "error", err)
					}
					return
				}
			}
		}
	}()

	m.logger.Info("session created",
		"session_id", sessionID,
		"client_id", clientID,
		"entrypoint", entrypointName,
		"target", route.Target,
		"channel_size", channelSize,
	)

	return sess, nil
}

func (m *Manager) DestroySession(sessionID string) error {
	val, ok := m.sessions.LoadAndDelete(sessionID)
	if !ok {
		return fmt.Errorf("%w: id=%s", core.ErrSessionNotFound, sessionID)
	}

	as := val.(*activeSession)
	as.cancel()

	if as.session.Route != nil {
		if ep, ok := m.endpoints[as.session.Route.Target]; ok {
			if err := ep.StopConsumer(sessionID); err != nil {
				m.logger.Warn("stop consumer error", "session_id", sessionID, "error", err)
			}
		}
	}

	m.logger.Info("session destroyed",
		"session_id", sessionID,
		"client_id", as.session.ClientID,
	)

	return nil
}

func (m *Manager) DestroyAll() {
	m.sessions.Range(func(key, _ any) bool {
		_ = m.DestroySession(key.(string))
		return true
	})
}

func (m *Manager) ActiveCount() int {
	count := 0
	m.sessions.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func (m *Manager) SessionByClientID(clientID string) (*core.Session, bool) {
	var found *core.Session
	m.sessions.Range(func(_, val any) bool {
		as := val.(*activeSession)
		if as.session.ClientID == clientID {
			found = as.session
			return false
		}
		return true
	})
	return found, found != nil
}
