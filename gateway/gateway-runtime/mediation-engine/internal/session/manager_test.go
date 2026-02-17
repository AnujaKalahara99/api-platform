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
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/internal/routing"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

type mockEndpoint struct {
	name      string
	startErr  error
	sendErr   error
	mu        sync.Mutex
	consumers map[string]bool
}

func (m *mockEndpoint) Name() string { return m.name }
func (m *mockEndpoint) Type() string { return "mock" }

func (m *mockEndpoint) StartConsumer(ctx context.Context, session *core.Session, ch chan<- core.BrokerMessage) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.mu.Lock()
	if m.consumers == nil {
		m.consumers = make(map[string]bool)
	}
	m.consumers[session.ID] = true
	m.mu.Unlock()
	<-ctx.Done()
	return nil
}

func (m *mockEndpoint) StopConsumer(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.consumers != nil {
		delete(m.consumers, sessionID)
	}
	return nil
}

func (m *mockEndpoint) Send(ctx context.Context, evt core.Event) error {
	return m.sendErr
}

func (m *mockEndpoint) Connect(ctx context.Context) error    { return nil }
func (m *mockEndpoint) Disconnect(ctx context.Context) error { return nil }

type mockHealthChecker struct {
	healthy map[string]bool
}

func newAllHealthy(names ...string) *mockHealthChecker {
	h := &mockHealthChecker{healthy: make(map[string]bool)}
	for _, n := range names {
		h.healthy[n] = true
	}
	return h
}

func (m *mockHealthChecker) IsEndpointHealthy(name string) bool {
	return m.healthy[name]
}

func TestCreateSession(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	routes := routing.NewTable()
	routes.Add(&core.Route{Source: "ws-in", Target: "kafka", ChannelSize: 5})

	ep := &mockEndpoint{name: "kafka"}
	endpoints := map[string]core.Endpoint{"kafka": ep}

	mgr := NewManager(routes, endpoints, newAllHealthy("kafka"), logger, nil)
	ctx := context.Background()

	sess, err := mgr.CreateSession(ctx, "ws-in", "client-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.ClientID != "client-1" {
		t.Fatalf("expected client-1, got %s", sess.ClientID)
	}
	if sess.Route.Target != "kafka" {
		t.Fatalf("expected kafka target, got %s", sess.Route.Target)
	}
	if cap(sess.Downstream) != 5 {
		t.Fatalf("expected channel size 5, got %d", cap(sess.Downstream))
	}
	if mgr.ActiveCount() != 1 {
		t.Fatalf("expected 1 active session, got %d", mgr.ActiveCount())
	}

	_ = mgr.DestroySession("client-1")
	time.Sleep(10 * time.Millisecond)
	if mgr.ActiveCount() != 0 {
		t.Fatalf("expected 0 active sessions after destroy, got %d", mgr.ActiveCount())
	}
}

func TestCreateSessionNoRoute(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	routes := routing.NewTable()
	mgr := NewManager(routes, nil, nil, logger, nil)

	_, err := mgr.CreateSession(context.Background(), "nonexistent", "client-1")
	if !errors.Is(err, core.ErrNoRoute) {
		t.Fatalf("expected ErrNoRoute, got %v", err)
	}
}

func TestCreateSessionNoEndpoint(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	routes := routing.NewTable()
	routes.Add(&core.Route{Source: "ws-in", Target: "missing"})

	mgr := NewManager(routes, map[string]core.Endpoint{}, nil, logger, nil)

	_, err := mgr.CreateSession(context.Background(), "ws-in", "client-1")
	if !errors.Is(err, core.ErrTargetNotFound) {
		t.Fatalf("expected ErrTargetNotFound, got %v", err)
	}
}

func TestDestroyNonexistentSession(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	mgr := NewManager(routing.NewTable(), nil, nil, logger, nil)

	err := mgr.DestroySession("nonexistent")
	if !errors.Is(err, core.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestDestroyAll(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	routes := routing.NewTable()
	routes.Add(&core.Route{Source: "ws-in", Target: "kafka", ChannelSize: 1})

	ep := &mockEndpoint{name: "kafka"}
	endpoints := map[string]core.Endpoint{"kafka": ep}
	mgr := NewManager(routes, endpoints, newAllHealthy("kafka"), logger, nil)

	for i := 0; i < 5; i++ {
		_, _ = mgr.CreateSession(context.Background(), "ws-in", "client")
	}

	mgr.DestroyAll()
	time.Sleep(10 * time.Millisecond)
	if mgr.ActiveCount() != 0 {
		t.Fatalf("expected 0 sessions, got %d", mgr.ActiveCount())
	}
}

func TestCreateSessionEndpointUnavailable(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	routes := routing.NewTable()
	routes.Add(&core.Route{Source: "ws-in", Target: "rabbitmq", ChannelSize: 1})

	ep := &mockEndpoint{name: "rabbitmq"}
	endpoints := map[string]core.Endpoint{"rabbitmq": ep}
	unhealthy := &mockHealthChecker{healthy: map[string]bool{"rabbitmq": false}}
	mgr := NewManager(routes, endpoints, unhealthy, logger, nil)

	_, err := mgr.CreateSession(context.Background(), "ws-in", "client-1")
	if !errors.Is(err, core.ErrEndpointUnavailable) {
		t.Fatalf("expected ErrEndpointUnavailable, got %v", err)
	}
}
