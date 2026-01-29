package mock

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"mediation-engine/pkg/core"
)

// MockEndpoint is a configurable mock endpoint for testing
// Config options:
//   - mode: "push" | "echo" | "both" (default: "echo")
//   - push_interval: duration string e.g., "1s", "500ms" (default: "1s")
//   - push_prefix: prefix for pushed messages (default: "mock-msg-")
//   - broadcast: "true" | "false" (default: "false") - if true, pushes to all clients continuously
type MockEndpoint struct {
	name   string
	config map[string]string
	hub    core.IngressHub

	mode         string
	pushInterval time.Duration
	pushPrefix   string
	broadcast    bool // If true, continuously broadcast regardless of client state

	clients map[string]context.CancelFunc
	mu      sync.RWMutex

	counter   int64
	counterMu sync.Mutex

	// For broadcast mode
	broadcastCtx    context.Context
	broadcastCancel context.CancelFunc
}

func New(name string, config map[string]string) *MockEndpoint {
	// Parse configuration with defaults
	mode := config["mode"]
	if mode == "" {
		mode = "echo"
	}

	interval := time.Second
	if intervalStr := config["push_interval"]; intervalStr != "" {
		if d, err := time.ParseDuration(intervalStr); err == nil {
			interval = d
		}
	}

	prefix := config["push_prefix"]
	broadcast := config["broadcast"] == "true"

	return &MockEndpoint{
		name:         name,
		config:       config,
		mode:         mode,
		pushInterval: interval,
		pushPrefix:   prefix,
		broadcast:    broadcast,
		clients:      make(map[string]context.CancelFunc),
		counter:      0,
	}
}

func (m *MockEndpoint) Name() string { return m.name }
func (m *MockEndpoint) Type() string { return "mock" }

func (m *MockEndpoint) Start(ctx context.Context, hub core.IngressHub) error {
	m.hub = hub

	if m.broadcast && (m.mode == "push" || m.mode == "both") {
		m.broadcastCtx, m.broadcastCancel = context.WithCancel(ctx)
		go m.broadcastLoop(m.broadcastCtx)
	}

	<-ctx.Done()
	if m.broadcastCancel != nil {
		m.broadcastCancel()
	}
	m.cleanupAllClients()
	return nil
}

// Broadcast messages are sent regardless of client connectivity - the hub/retention handles offline clients
func (m *MockEndpoint) broadcastLoop(ctx context.Context) {
	ticker := time.NewTicker(m.pushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.counterMu.Lock()
			m.counter++
			count := m.counter
			m.counterMu.Unlock()

			// Get all registered client IDs
			m.mu.RLock()
			clientIDs := make([]string, 0, len(m.clients))
			for clientID := range m.clients {
				clientIDs = append(clientIDs, clientID)
			}
			m.mu.RUnlock()

			if len(clientIDs) == 0 {
				log.Printf("[%s] Broadcast tick %d: no clients registered", m.name, count)
				continue
			}

			payload := fmt.Sprintf("%s%d", m.pushPrefix, count)
			for _, clientID := range clientIDs {
				evt := core.Event{
					ID:       fmt.Sprintf("%s-%s-%d", m.name, clientID, count),
					Type:     core.EventTypeMessage,
					SourceID: m.name,
					ClientID: clientID,
					Payload:  []byte(payload),
					Metadata: map[string]string{
						"mock_counter": strconv.FormatInt(count, 10),
						"mock_mode":    "broadcast",
					},
				}
				m.hub.Publish(evt)
			}
			log.Printf("[%s] Broadcast to %d clients: %s", m.name, len(clientIDs), payload)
		}
	}
}

// SubscribeForClient registers a client for message pushing
func (m *MockEndpoint) SubscribeForClient(ctx context.Context, clientID string, opts core.SubscribeOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cancel, exists := m.clients[clientID]; exists {
		cancel()
	}

	clientCtx, cancel := context.WithCancel(ctx)
	m.clients[clientID] = cancel

	if !m.broadcast && (m.mode == "push" || m.mode == "both") {
		go m.pushLoop(clientCtx, clientID)
	}

	log.Printf("[%s] Client %s subscribed (mode=%s, broadcast=%v)", m.name, clientID, m.mode, m.broadcast)
	return nil
}

// pushLoop sends incrementing integers at the configured interval (per-client mode)
func (m *MockEndpoint) pushLoop(ctx context.Context, clientID string) {
	ticker := time.NewTicker(m.pushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[%s] Push loop stopped for client %s", m.name, clientID)
			return
		case <-ticker.C:
			m.counterMu.Lock()
			m.counter++
			count := m.counter
			m.counterMu.Unlock()

			payload := fmt.Sprintf("%s%d", m.pushPrefix, count)
			evt := core.Event{
				ID:       fmt.Sprintf("%s-%s-%d", m.name, clientID, count),
				Type:     core.EventTypeMessage,
				SourceID: m.name,
				ClientID: clientID,
				Payload:  []byte(payload),
				Metadata: map[string]string{
					"mock_counter": strconv.FormatInt(count, 10),
					"mock_mode":    "push",
				},
			}

			log.Printf("[%s] Pushing to client %s: %s", m.name, clientID, payload)
			m.hub.Publish(evt)
		}
	}
}

// UnsubscribeClient stops pushing messages to the client (Not in broadcast mode)
func (m *MockEndpoint) UnsubscribeClient(ctx context.Context, clientID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cancel, exists := m.clients[clientID]; exists && !m.broadcast {
		cancel()
		delete(m.clients, clientID)
		log.Printf("[%s] Client %s unsubscribed", m.name, clientID)
	}

	return nil
}

// SendUpstream handles messages sent to the mock endpoint
// In echo mode, it publishes the message back to the hub
func (m *MockEndpoint) SendUpstream(ctx context.Context, evt core.Event) error {
	log.Printf("[%s] Received from client %s: %s", m.name, evt.ClientID, string(evt.Payload))

	if m.mode == "echo" || m.mode == "both" {
		echoEvt := core.Event{
			ID:       fmt.Sprintf("%s-echo-%s", m.name, evt.ID),
			Type:     core.EventTypeMessage,
			SourceID: m.name,
			ClientID: evt.ClientID,
			Payload:  evt.Payload,
			Metadata: map[string]string{
				"mock_mode":   "echo",
				"original_id": evt.ID,
				"echo_of":     string(evt.Payload),
			},
		}

		log.Printf("[%s] Echoing back to client %s: %s", m.name, evt.ClientID, string(evt.Payload))
		m.hub.Publish(echoEvt)
	}

	return nil
}

func (m *MockEndpoint) cleanupAllClients() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for clientID, cancel := range m.clients {
		cancel()
		delete(m.clients, clientID)
		log.Printf("[%s] Cleaned up client %s", m.name, clientID)
	}
}
