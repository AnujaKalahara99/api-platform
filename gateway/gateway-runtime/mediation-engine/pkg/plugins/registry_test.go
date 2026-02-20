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
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

// --- mock entrypoint --------------------------------------------------------

type mockEntrypoint struct {
	name       string
	epType     string
	registered bool
}

func (m *mockEntrypoint) Name() string { return m.name }
func (m *mockEntrypoint) Type() string { return m.epType }
func (m *mockEntrypoint) RegisterRoutes(mux *http.ServeMux, _ core.SessionManager) {
	m.registered = true
	mux.HandleFunc("/"+m.name, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(m.name + " ok"))
	})
}
func (m *mockEntrypoint) Stop(_ context.Context) error { return nil }

// --- mock endpoint ----------------------------------------------------------

type mockEndpoint struct {
	name         string
	connected    bool
	connectErr   error
	disconnected bool
}

func (m *mockEndpoint) Name() string { return m.name }
func (m *mockEndpoint) Type() string { return "mock" }
func (m *mockEndpoint) StartConsumer(_ context.Context, _ *core.Session, _ chan<- core.BrokerMessage) error {
	return nil
}
func (m *mockEndpoint) StopConsumer(_ string) error                { return nil }
func (m *mockEndpoint) Send(_ context.Context, _ core.Event) error { return nil }
func (m *mockEndpoint) Connect(_ context.Context) error {
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}
func (m *mockEndpoint) Disconnect(_ context.Context) error {
	m.disconnected = true
	return nil
}

// --- helpers ----------------------------------------------------------------

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func httpGetOverUDS(socketPath, path string) (*http.Response, error) {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}
	return client.Get("http://mediation-engine" + path)
}

// --- tests ------------------------------------------------------------------

func TestNewRegistryDefaultSocketPath(t *testing.T) {
	reg := NewRegistry(testLogger(), "")
	if reg.SocketPath() != core.DefaultMediationSocketPath {
		t.Fatalf("expected default socket path %s, got %s", core.DefaultMediationSocketPath, reg.SocketPath())
	}
}

func TestNewRegistryCustomSocketPath(t *testing.T) {
	custom := "/tmp/custom.sock"
	reg := NewRegistry(testLogger(), custom)
	if reg.SocketPath() != custom {
		t.Fatalf("expected %s, got %s", custom, reg.SocketPath())
	}
}

func TestRegistryRegisterEntrypoint(t *testing.T) {
	reg := NewRegistry(testLogger(), "")
	ep := &mockEntrypoint{name: "ws-in", epType: "websocket"}
	reg.RegisterEntrypoint(ep)

	eps := reg.Entrypoints()
	if len(eps) != 1 {
		t.Fatalf("expected 1 entrypoint, got %d", len(eps))
	}
	if eps["ws-in"] == nil {
		t.Fatal("expected ws-in entrypoint")
	}
}

func TestRegistryRegisterEndpoint(t *testing.T) {
	reg := NewRegistry(testLogger(), "")
	ep := &mockEndpoint{name: "kafka"}
	reg.RegisterEndpoint(ep)

	eps := reg.Endpoints()
	if len(eps) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(eps))
	}
	if eps["kafka"] == nil {
		t.Fatal("expected kafka endpoint")
	}
}

func TestRegistryConnectEndpoints(t *testing.T) {
	reg := NewRegistry(testLogger(), "")
	ep := &mockEndpoint{name: "kafka"}
	reg.RegisterEndpoint(ep)

	connected := reg.ConnectEndpoints(context.Background())
	if connected != 1 {
		t.Fatalf("expected 1 connected, got %d", connected)
	}
	if !ep.connected {
		t.Fatal("expected endpoint to be connected")
	}
	if !reg.IsEndpointHealthy("kafka") {
		t.Fatal("expected kafka to be healthy")
	}
}

func TestRegistryUDSServerStartsAndServesRequests(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test-mediation.sock")
	reg := NewRegistry(testLogger(), socketPath)

	ep := &mockEntrypoint{name: "ws-in", epType: "websocket"}
	reg.RegisterEntrypoint(ep)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg.StartEntrypoints(ctx, nil)

	// Wait for UDS socket to appear
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !ep.registered {
		t.Fatal("expected entrypoint RegisterRoutes to be called")
	}

	// Test entrypoint route
	resp, err := httpGetOverUDS(socketPath, "/ws-in")
	if err != nil {
		t.Fatalf("UDS request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ws-in ok" {
		t.Fatalf("unexpected body: %s", string(body))
	}
}

func TestRegistryHealthEndpoint(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test-healthz.sock")
	reg := NewRegistry(testLogger(), socketPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg.StartEntrypoints(ctx, nil)

	// Wait for socket
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	resp, err := httpGetOverUDS(socketPath, "/healthz")
	if err != nil {
		t.Fatalf("UDS request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"status":"ok"}` {
		t.Fatalf("unexpected health response: %s", string(body))
	}
}

func TestRegistryMultipleEntrypoints(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test-multi.sock")
	reg := NewRegistry(testLogger(), socketPath)

	eps := []*mockEntrypoint{
		{name: "ws-in", epType: "websocket"},
		{name: "sse-in", epType: "sse"},
		{name: "http-post", epType: "httppost"},
	}
	for _, ep := range eps {
		reg.RegisterEntrypoint(ep)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg.StartEntrypoints(ctx, nil)

	// Wait for socket
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	for _, ep := range eps {
		resp, err := httpGetOverUDS(socketPath, "/"+ep.name)
		if err != nil {
			t.Fatalf("UDS request for %s failed: %v", ep.name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d", ep.name, resp.StatusCode)
		}
	}
}

func TestRegistryStopAll(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test-stop.sock")
	reg := NewRegistry(testLogger(), socketPath)

	ep := &mockEntrypoint{name: "ws-in", epType: "websocket"}
	mockEp := &mockEndpoint{name: "kafka"}
	reg.RegisterEntrypoint(ep)
	reg.RegisterEndpoint(mockEp)
	reg.ConnectEndpoints(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg.StartEntrypoints(ctx, nil)

	// Wait for socket
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Verify socket exists
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("socket should exist before StopAll: %v", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	reg.StopAll(stopCtx)

	// Socket should be cleaned up
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatal("expected socket to be removed after StopAll")
	}

	if !mockEp.disconnected {
		t.Fatal("expected endpoint to be disconnected")
	}
}
