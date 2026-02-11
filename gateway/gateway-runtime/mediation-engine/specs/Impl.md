

# Mediation Engine — Implementation Plan

---

## 1. Architecture Overview

The Mediation Engine is a **lightweight, zero-copy, passthrough mediator** that bridges web protocols (WebSocket, SSE, HTTP GET, HTTP POST) with event brokers (Kafka, RabbitMQ, MQTT5, Solace, JMS). It operates inside the same container as the WSO2 Policy Engine and Envoy Router.

**Design Principles**:
- **No packet storage** — events flow through without buffering to disk
- **Per-connection flow control** — each client connection maps 1:1 to a broker consumer; slow clients throttle broker consumption
- **Broker ack at entrypoint** — message acknowledgment travels back from the entrypoint to the endpoint, enabling configurable delivery guarantees
- **Lifecycle coupling** — when a client disconnects, the corresponding broker consumer stops; when a broker disconnects, the client is notified

---

## 2. Core Data Flow

```
                         ┌─────────────────────────────────┐
                         │        Mediation Engine          │
                         │                                  │
  Client ──┐             │   ┌──────────┐    ┌──────────┐  │             ┌─────────┐
            │  connect   │   │Entrypoint│    │ Endpoint │  │  subscribe  │  Broker  │
            ├───────────▶│   │  (WS/SSE │◄──►│ (Kafka/  │  │◄───────────▶│         │
            │            │   │  HTTP)   │    │  AMQP/   │  │             │         │
            │  data      │   │          │    │  MQTT5/  │  │  consume    │         │
            │◄──────────▶│   │          │    │  Solace/ │  │◄───────────▶│         │
            │            │   │          │    │  JMS)    │  │             │         │
            │  ack ──────│───│──────────│───▶│──────────│──│──── ack ──▶ │         │
            │            │   │          │    │          │  │             │         │
  Client ──┘  disconnect │   └──────────┘    └──────────┘  │  unsub     └─────────┘
                         │       │                │        │
                         │       └───lifecycle────┘        │
                         │          coupling               │
                         └─────────────────────────────────┘

Per-connection flow:
1. Client connects to Entrypoint
2. Entrypoint creates a Session (in-memory, per-connection)
3. Session starts an Endpoint consumer for this client
4. Broker messages flow: Endpoint.Receive() → Session channel → Entrypoint.Send()
5. Entrypoint confirms delivery → Endpoint.Ack() (based on delivery guarantee)
6. Client disconnects → Session tears down → Endpoint consumer stops
7. Slow client → Session channel fills → Endpoint.Receive() blocks → broker backpressure
```

---

## 3. Flow Control Model

Each client connection creates a **Session** that owns exactly one bounded channel. The endpoint consumer for that session writes to this channel. The entrypoint reader for that session reads from it.

```
Broker ──► Endpoint.Receive()
               │
               │ blocks if channel full (backpressure to broker)
               ▼
         Session.Channel (bounded, configurable size)
               │
               │ blocks if client is slow
               ▼
           Entrypoint.Send()
               │
               │ on success: Endpoint.Ack(msg)
               │ on failure: Endpoint.Nack(msg) or close session
               ▼
           Client
```

**Upstream (client → broker)** follows the same pattern in reverse:

```
Client ──► Entrypoint.Receive()
               │
               ▼
         Session.UpstreamChannel (bounded)
               │
               ▼
           Endpoint.Send()
               │
               │ on success: ack back to client (if protocol supports)
               ▼
           Broker
```

When the **channel is full**, the reader side blocks, which in turn causes the writer side to block, which causes the protocol-level flow control to kick in:
- **Kafka**: Consumer poll stops → no new fetches → consumer group rebalance timeout handles it
- **RabbitMQ**: Channel prefetch limit reached → broker stops delivering
- **MQTT5**: QoS flow control, receive maximum
- **Solace**: Flow control via windowed ack
- **JMS/AMQP 1.0**: Link credit exhausted → sender stops

---

## 4. Delivery Guarantees

Acknowledgment flows **from entrypoint back to endpoint**. The delivery guarantee is configured per-route.

| Guarantee | Behavior | Ack Point |
|---|---|---|
| `none` | Fire-and-forget, no ack | Message immediately discarded from broker |
| `at-most-once` | Ack before entrypoint delivery | Endpoint acks on receive, before sending to client |
| `at-least-once` | Ack after entrypoint delivery | Endpoint acks only after client confirms receipt |
| `auto` | Ack on channel write (default) | Endpoint acks when message enters session channel |

````go
package core

import "time"

// DeliveryGuarantee controls when broker messages are acknowledged.
type DeliveryGuarantee int

const (
	// DeliveryAuto acks when the message enters the session channel.
	DeliveryAuto DeliveryGuarantee = iota
	// DeliveryNone acks immediately on receive from broker — fire and forget.
	DeliveryNone
	// DeliveryAtMostOnce acks before delivery to client — message may be lost.
	DeliveryAtMostOnce
	// DeliveryAtLeastOnce acks after client confirms receipt — message may be duplicated.
	DeliveryAtLeastOnce
)

// Event is the protocol-agnostic data unit flowing through the engine.
// It is never stored — only passed through channels between goroutines.
type Event struct {
	ID        string            `json:"id"`
	SourceID  string            `json:"source_id"`
	ClientID  string            `json:"client_id"`
	Payload   []byte            `json:"payload"`
	Metadata  map[string]string `json:"metadata"`
	Timestamp time.Time         `json:"timestamp"`
	Type      EventType         `json:"type"`
}

type EventType int

const (
	EventTypeData       EventType = iota
	EventTypeConnect
	EventTypeDisconnect
)

func (e Event) IsLifecycle() bool {
	return e.Type == EventTypeConnect || e.Type == EventTypeDisconnect
}

// BrokerMessage wraps an Event with a broker-specific ack/nack handle.
// The entrypoint calls Ack() or Nack() after processing, which propagates
// back to the broker through the endpoint.
type BrokerMessage struct {
	Event Event
	Ack   func() error // acknowledge to broker
	Nack  func() error // negative-acknowledge / reject
}

// Route defines a unidirectional mapping from source to target.
type Route struct {
	Source            string            `yaml:"source"`
	Target            string            `yaml:"target"`
	Direction         Direction         `yaml:"direction"`
	DeliveryGuarantee DeliveryGuarantee `yaml:"delivery_guarantee"`
	ChannelSize       int               `yaml:"channel_size"` // per-session flow control buffer
}

type Direction int

const (
	DirectionUpstream   Direction = iota // client → broker
	DirectionDownstream                  // broker → client
)
````

---

## 5. Interfaces

````go
package core

import "context"

// Entrypoint accepts inbound traffic from clients.
// Each client connection is represented by a Session.
type Entrypoint interface {
	Name() string
	Type() string
	// Start begins accepting client connections. Blocking.
	Start(ctx context.Context, manager SessionManager) error
	// Stop gracefully shuts down, closing all client connections.
	Stop(ctx context.Context) error
}

// Endpoint connects to a backend broker system.
type Endpoint interface {
	Name() string
	Type() string
	// StartConsumer creates a consumer for a specific session.
	// It reads from the broker and writes BrokerMessages to the provided channel.
	// It MUST block until ctx is cancelled (session teardown).
	// It MUST respect backpressure: if the channel is full, it stops consuming.
	StartConsumer(ctx context.Context, session *Session, ch chan<- BrokerMessage) error
	// StopConsumer tears down the consumer for a specific session.
	StopConsumer(sessionID string) error
	// Send publishes a message upstream to the broker (client → broker direction).
	Send(ctx context.Context, evt Event) error
	// Connect establishes the broker connection. Called once at startup.
	Connect(ctx context.Context) error
	// Disconnect tears down the broker connection. Called once at shutdown.
	Disconnect(ctx context.Context) error
}

// SessionManager is the central coordinator that creates and destroys sessions.
// The entrypoint calls this when clients connect/disconnect.
type SessionManager interface {
	// CreateSession is called when a client connects to an entrypoint.
	// It resolves the route, starts the endpoint consumer, and returns a Session.
	CreateSession(ctx context.Context, entrypointName string, clientID string) (*Session, error)
	// DestroySession is called when a client disconnects.
	// It stops the endpoint consumer and cleans up.
	DestroySession(sessionID string) error
}

// Session represents a single client connection and its paired broker consumer.
// It owns the bounded channels that enforce flow control.
type Session struct {
	ID            string
	ClientID      string
	EntrypointName string
	Route         *Route

	// Downstream: broker → client. Entrypoint reads from this.
	Downstream chan BrokerMessage
	// Upstream: client → broker. Endpoint reads from this.
	Upstream   chan Event

	// Cancel tears down this session's goroutines.
	Cancel context.CancelFunc
}
````

---

## 6. Domain Errors

````go
package core

import "errors"

var (
	ErrNoRoute          = errors.New("no route found for source")
	ErrTargetNotFound   = errors.New("delivery target not found")
	ErrSessionNotFound  = errors.New("session not found")
	ErrSessionClosed    = errors.New("session closed")
	ErrClientSlow       = errors.New("client too slow, session terminated")
	ErrBrokerDisconnect = errors.New("broker disconnected")
)
````

---

## 7. Directory Structure

```
mediation-engine/
├── cmd/
│   └── server/
│       └── main.go                       # Wiring and startup
├── internal/
│   ├── session/
│   │   ├── manager.go                    # Session lifecycle, route resolution, consumer pairing
│   │   └── manager_test.go
│   ├── routing/
│   │   ├── table.go                      # Lock-free route table (sync.Map)
│   │   └── table_test.go
│   └── logging/
│       └── packet.go                     # Structured JSON packet logger for Fluent Bit
├── pkg/
│   ├── core/
│   │   ├── interfaces.go                 # Entrypoint, Endpoint, SessionManager, Session
│   │   ├── types.go                      # Event, BrokerMessage, Route, DeliveryGuarantee
│   │   └── errors.go                     # Domain errors
│   ├── plugins/
│   │   ├── registry.go                   # Plugin registry with lifecycle
│   │   ├── ws/
│   │   │   └── entrypoint.go             # WebSocket entrypoint
│   │   ├── sse/
│   │   │   └── entrypoint.go             # SSE entrypoint
│   │   ├── httppost/
│   │   │   └── entrypoint.go             # HTTP POST webhook entrypoint
│   │   ├── httpget/
│   │   │   └── entrypoint.go             # HTTP GET polling entrypoint
│   │   ├── kafka/
│   │   │   └── endpoint.go               # Kafka endpoint
│   │   ├── rabbitmq/
│   │   │   └── endpoint.go               # RabbitMQ (AMQP 0-9-1) endpoint
│   │   ├── mqtt5/
│   │   │   └── endpoint.go               # MQTT 5.0 endpoint
│   │   ├── solace/
│   │   │   └── endpoint.go               # Solace endpoint
│   │   └── jms/
│   │       └── endpoint.go               # JMS via AMQP 1.0 endpoint
│   └── config/
│       ├── loader.go                     # YAML config parsing
│       ├── loader_test.go
│       └── watcher.go                    # File watcher for route hot-reload
├── config.yaml
├── go.mod
├── Makefile
└── README.md
```

---

## 8. Session Manager

The Session Manager is the heart of the engine. It replaces the Hub/Bus from traditional pub-sub architectures with a **per-connection pairing model**.

````go
package session

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"mediation-engine/internal/logging"
	"mediation-engine/internal/routing"
	"mediation-engine/pkg/core"
)

type Manager struct {
	sessions   sync.Map // sessionID → *activeSession
	routes     *routing.Table
	endpoints  map[string]core.Endpoint
	logger     *slog.Logger
	packetLog  *logging.PacketLogger
}

// activeSession tracks a live session and its cancellation
type activeSession struct {
	session *core.Session
	cancel  context.CancelFunc
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

// CreateSession pairs a client connection with a broker consumer.
// The returned Session contains bounded channels for flow control.
func (m *Manager) CreateSession(
	ctx context.Context,
	entrypointName string,
	clientID string,
) (*core.Session, error) {
	// 1. Resolve route
	route, ok := m.routes.Lookup(entrypointName)
	if !ok {
		return nil, fmt.Errorf("%w: source=%s", core.ErrNoRoute, entrypointName)
	}

	// 2. Find target endpoint
	ep, ok := m.endpoints[route.Target]
	if !ok {
		return nil, fmt.Errorf("%w: endpoint=%s", core.ErrTargetNotFound, route.Target)
	}

	// 3. Create session with bounded channels
	channelSize := route.ChannelSize
	if channelSize <= 0 {
		channelSize = 1 // unbuffered by default — maximum backpressure
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

	// 4. Start endpoint consumer for this session (downstream: broker → client)
	go func() {
		defer func() {
			m.logger.Info("endpoint consumer stopped",
				"session_id", sessionID,
				"client_id", clientID,
				"endpoint", route.Target,
			)
		}()

		if err := ep.StartConsumer(sessionCtx, sess, sess.Downstream); err != nil {
			if sessionCtx.Err() == nil {
				m.logger.Error("endpoint consumer error",
					"session_id", sessionID,
					"error", err,
				)
			}
		}
	}()

	// 5. Start upstream relay (client → broker)
	go func() {
		for {
			select {
			case <-sessionCtx.Done():
				return
			case evt, ok := <-sess.Upstream:
				if !ok {
					return
				}
				m.packetLog.Log(evt, route, "upstream")
				if err := ep.Send(sessionCtx, evt); err != nil {
					m.logger.Error("upstream send failed",
						"session_id", sessionID,
						"error", err,
					)
				}
			}
		}
	}()

	m.logger.Info("session created",
		"session_id", sessionID,
		"client_id", clientID,
		"entrypoint", entrypointName,
		"endpoint", route.Target,
		"direction", route.Direction,
		"delivery_guarantee", route.DeliveryGuarantee,
		"channel_size", channelSize,
	)

	return sess, nil
}

// DestroySession stops the broker consumer and closes channels.
func (m *Manager) DestroySession(sessionID string) error {
	val, ok := m.sessions.LoadAndDelete(sessionID)
	if !ok {
		return fmt.Errorf("%w: id=%s", core.ErrSessionNotFound, sessionID)
	}

	as := val.(*activeSession)
	as.cancel() // cancels sessionCtx → stops consumer + relay goroutines

	// Find endpoint and explicitly stop consumer
	if as.session.Route != nil {
		if ep, ok := m.endpoints[as.session.Route.Target]; ok {
			ep.StopConsumer(sessionID)
		}
	}

	m.logger.Info("session destroyed",
		"session_id", sessionID,
		"client_id", as.session.ClientID,
	)

	return nil
}

// DestroyAll tears down all active sessions. Used during shutdown.
func (m *Manager) DestroyAll() {
	m.sessions.Range(func(key, _ any) bool {
		m.DestroySession(key.(string))
		return true
	})
}

// ActiveCount returns the number of active sessions.
func (m *Manager) ActiveCount() int {
	count := 0
	m.sessions.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// SessionByClientID finds a session by client ID (for downstream routing).
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
````

---

## 9. Route Table

````go
package routing

import (
	"sync"

	"mediation-engine/pkg/core"
)

// Table provides lock-free route lookups. Optimized for read-heavy workloads.
type Table struct {
	routes sync.Map // string → *core.Route
}

func NewTable() *Table {
	return &Table{}
}

func (t *Table) Add(route *core.Route) {
	t.routes.Store(route.Source, route)
}

func (t *Table) Remove(source string) {
	t.routes.Delete(source)
}

func (t *Table) Lookup(source string) (*core.Route, bool) {
	v, ok := t.routes.Load(source)
	if !ok {
		return nil, false
	}
	return v.(*core.Route), true
}

// ReplaceAll atomically replaces all routes (for hot-reload).
func (t *Table) ReplaceAll(routes []*core.Route) {
	t.routes.Range(func(key, _ any) bool {
		t.routes.Delete(key)
		return true
	})
	for _, r := range routes {
		t.routes.Store(r.Source, r)
	}
}
````

---

## 10. Packet Logger

Structured JSON logging for external extraction by Fluent Bit. This is the only observation point — no metrics, no policy engine.

````go
package logging

import (
	"log/slog"

	"mediation-engine/pkg/core"
)

// PacketLogger emits structured JSON logs for every data packet.
// Fluent Bit or a similar log shipper extracts these from stdout.
type PacketLogger struct {
	logger *slog.Logger
}

func NewPacketLogger(logger *slog.Logger) *PacketLogger {
	return &PacketLogger{logger: logger}
}

func (p *PacketLogger) Log(evt core.Event, route *core.Route, direction string) {
	p.logger.Info("packet",
		"event_id", evt.ID,
		"source_id", evt.SourceID,
		"client_id", evt.ClientID,
		"target", route.Target,
		"direction", direction,
		"payload_bytes", len(evt.Payload),
		"timestamp", evt.Timestamp,
	)
}
````

---

## 11. Entrypoint Plugins

### 11.1 WebSocket Entrypoint

The WebSocket entrypoint creates a session per connection and runs two goroutines per client: one reading from the client (upstream), one writing to the client (downstream). The downstream goroutine reads from the session's `Downstream` channel — when it's empty, it blocks, which propagates backpressure to the endpoint consumer.

````go
package ws

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"mediation-engine/internal/logging"
	"mediation-engine/pkg/core"
)

type Entrypoint struct {
	name      string
	port      int
	upgrader  websocket.Upgrader
	manager   core.SessionManager
	server    *http.Server
	logger    *slog.Logger
	packetLog *logging.PacketLogger
	sessions  sync.Map // clientID → *core.Session
}

func New(name string, port int, logger *slog.Logger, packetLog *logging.PacketLogger) *Entrypoint {
	return &Entrypoint{
		name:      name,
		port:      port,
		logger:    logger,
		packetLog: packetLog,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
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
	// Destroy all sessions — this stops all paired endpoint consumers
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

func (e *Entrypoint) handleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := e.upgrader.Upgrade(w, r, nil)
	if err != nil {
		e.logger.Error("ws upgrade failed", "error", err)
		return
	}

	clientID := uuid.New().String()

	// Create session — this starts the paired endpoint consumer
	sess, err := e.manager.CreateSession(r.Context(), e.name, clientID)
	if err != nil {
		e.logger.Error("session creation failed", "client_id", clientID, "error", err)
		conn.Close()
		return
	}

	e.sessions.Store(clientID, sess)

	// Cleanup on disconnect
	defer func() {
		conn.Close()
		e.sessions.Delete(clientID)
		e.manager.DestroySession(sess.ID)
		e.logger.Info("ws client disconnected", "client_id", clientID, "session_id", sess.ID)
	}()

	e.logger.Info("ws client connected", "client_id", clientID, "session_id", sess.ID)

	// Downstream goroutine: broker → client
	// This goroutine blocks on sess.Downstream when empty.
	// The block propagates to the endpoint consumer, which stops consuming from the broker.
	go e.downstreamLoop(conn, sess)

	// Upstream loop: client → broker (runs on this goroutine)
	e.upstreamLoop(conn, sess)
}

func (e *Entrypoint) downstreamLoop(conn *websocket.Conn, sess *core.Session) {
	for {
		select {
		case msg, ok := <-sess.Downstream:
			if !ok {
				return // session closed
			}

			e.packetLog.Log(msg.Event, sess.Route, "downstream")

			switch sess.Route.DeliveryGuarantee {
			case core.DeliveryNone:
				// Fire and forget — ack immediately, don't care about write result
				msg.Ack()
				conn.WriteMessage(websocket.TextMessage, msg.Event.Payload)

			case core.DeliveryAtMostOnce:
				// Ack before delivery — message may be lost if write fails
				msg.Ack()
				if err := conn.WriteMessage(websocket.TextMessage, msg.Event.Payload); err != nil {
					e.logger.Warn("downstream write failed (at-most-once, message lost)",
						"session_id", sess.ID, "error", err)
					return
				}

			case core.DeliveryAtLeastOnce:
				// Ack after delivery — message may be redelivered if we crash after write
				if err := conn.WriteMessage(websocket.TextMessage, msg.Event.Payload); err != nil {
					e.logger.Warn("downstream write failed, nacking",
						"session_id", sess.ID, "error", err)
					msg.Nack()
					return
				}
				msg.Ack()

			default: // DeliveryAuto — ack happened on channel write in endpoint
				if err := conn.WriteMessage(websocket.TextMessage, msg.Event.Payload); err != nil {
					e.logger.Warn("downstream write failed",
						"session_id", sess.ID, "error", err)
					return
				}
			}
		}
	}
}

func (e *Entrypoint) upstreamLoop(conn *websocket.Conn, sess *core.Session) {
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return // client disconnected
		}

		evt := core.Event{
			ID:        uuid.New().String(),
			SourceID:  e.name,
			ClientID:  sess.ClientID,
			Payload:   payload,
			Type:      core.EventTypeData,
			Timestamp: time.Now(),
			Metadata:  map[string]string{"protocol": "websocket"},
		}

		e.packetLog.Log(evt, sess.Route, "upstream")

		select {
		case sess.Upstream <- evt:
		default:
			// Upstream channel full — broker is slower than client.
			// Block until space is available (backpressure to client via TCP).
			sess.Upstream <- evt
		}
	}
}
````

### 11.2 SSE Entrypoint

SSE is downstream-only (server → client). Upstream is not applicable.

````go
package sse

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"mediation-engine/internal/logging"
	"mediation-engine/pkg/core"
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
		http.Error(w, "no route", http.StatusBadGateway)
		return
	}

	e.sessions.Store(clientID, sess)
	defer func() {
		e.sessions.Delete(clientID)
		e.manager.DestroySession(sess.ID)
		e.logger.Info("sse client disconnected", "client_id", clientID)
	}()

	e.logger.Info("sse client connected", "client_id", clientID, "session_id", sess.ID)

	// Downstream loop: broker → SSE stream
	// Blocks on sess.Downstream when empty → backpressure to broker
	for {
		select {
		case <-r.Context().Done():
			return // client disconnected

		case msg, ok := <-sess.Downstream:
			if !ok {
				return
			}

			e.packetLog.Log(msg.Event, sess.Route, "downstream")

			data := fmt.Sprintf("id: %s\ndata: %s\n\n", msg.Event.ID, string(msg.Event.Payload))

			switch sess.Route.DeliveryGuarantee {
			case core.DeliveryNone:
				msg.Ack()
				fmt.Fprint(w, data)
				flusher.Flush()

			case core.DeliveryAtMostOnce:
				msg.Ack()
				if _, err := fmt.Fprint(w, data); err != nil {
					return
				}
				flusher.Flush()

			case core.DeliveryAtLeastOnce:
				if _, err := fmt.Fprint(w, data); err != nil {
					msg.Nack()
					return
				}
				flusher.Flush()
				msg.Ack()

			default: // DeliveryAuto
				fmt.Fprint(w, data)
				flusher.Flush()
			}
		}
	}
}
````

### 11.3 HTTP POST Entrypoint (Webhook Receiver)

HTTP POST is upstream-only: each POST body becomes an event sent to the broker. No persistent session — request-scoped.

````go
package httppost

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"mediation-engine/internal/logging"
	"mediation-engine/pkg/core"
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
		maxBody:   1 << 20, // 1MB
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

	clientID := r.Header.Get("X-Client-ID")
	if clientID == "" {
		clientID = r.RemoteAddr
	}

	// Create ephemeral session for this request
	sess, err := e.manager.CreateSession(r.Context(), e.name, clientID)
	if err != nil {
		e.logger.Error("http_post session failed", "error", err)
		http.Error(w, "no route configured", http.StatusBadGateway)
		return
	}
	defer e.manager.DestroySession(sess.ID)

	evt := core.Event{
		ID:        uuid.New().String(),
		SourceID:  e.name,
		ClientID:  clientID,
		Payload:   body,
		Type:      core.EventTypeData,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"protocol":     "http_post",
			"content_type": r.Header.Get("Content-Type"),
		},
	}

	e.packetLog.Log(evt, sess.Route, "upstream")

	// Send upstream synchronously — backpressure blocks the HTTP response
	select {
	case sess.Upstream <- evt:
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"accepted"}`))
	case <-r.Context().Done():
		http.Error(w, "timeout", http.StatusGatewayTimeout)
	}
}
````

### 11.4 HTTP GET Entrypoint (Polling)

HTTP GET is downstream-only: clients poll for the next available message from the broker.

````go
package httpget

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"mediation-engine/internal/logging"
	"mediation-engine/pkg/core"
)

type Entrypoint struct {
	name      string
	port      int
	manager   core.SessionManager
	server    *http.Server
	logger    *slog.Logger
	packetLog *logging.PacketLogger
	sessions  sync.Map // clientID → *core.Session
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

// handleSubscribe creates a session and starts consuming. Returns a client ID for polling.
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
		http.Error(w, "no route", http.StatusBadGateway)
		return
	}

	e.sessions.Store(clientID, sess)
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(fmt.Sprintf(`{"session_id":"%s","client_id":"%s"}`, sess.ID, clientID)))
}

// handlePoll returns the next available message. Blocks up to timeout.
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

	// Long poll with timeout
	timeout := 30 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	select {
	case msg, ok := <-sess.Downstream:
		if !ok {
			http.Error(w, "session closed", http.StatusGone)
			return
		}

		e.packetLog.Log(msg.Event, sess.Route, "downstream")

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Event-ID", msg.Event.ID)
		w.WriteHeader(http.StatusOK)
		w.Write(msg.Event.Payload)

		// Ack based on delivery guarantee
		switch sess.Route.DeliveryGuarantee {
		case core.DeliveryNone, core.DeliveryAtMostOnce, core.DeliveryAuto:
			msg.Ack()
		case core.DeliveryAtLeastOnce:
			// For HTTP GET polling, ack after write is the best we can do.
			// True at-least-once would need a separate /ack endpoint.
			msg.Ack()
		}

	case <-ctx.Done():
		w.WriteHeader(http.StatusNoContent) // timeout, no data
	}
}

// handleUnsubscribe destroys the session and stops the broker consumer.
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
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"unsubscribed"}`))
}
````

---

## 12. Endpoint Plugins

Each endpoint implements per-session consumers. The `StartConsumer` method blocks until the session context is cancelled (client disconnected), providing lifecycle coupling.

### 12.1 Kafka Endpoint

````go
package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"mediation-engine/pkg/core"
)

type Endpoint struct {
	name     string
	brokers  []string
	topicIn  string
	topicOut string
	writer   *kafka.Writer
	logger   *slog.Logger
	consumers sync.Map // sessionID → *kafka.Reader
}

func New(name string, brokers []string, topicIn, topicOut string, logger *slog.Logger) *Endpoint {
	return &Endpoint{
		name:    name,
		brokers: brokers,
		topicIn: topicIn,
		topicOut: topicOut,
		logger:  logger,
	}
}

func (e *Endpoint) Name() string { return e.name }
func (e *Endpoint) Type() string { return "kafka" }

func (e *Endpoint) Connect(ctx context.Context) error {
	e.writer = &kafka.Writer{
		Addr:     kafka.TCP(e.brokers...),
		Topic:    e.topicOut,
		Balancer: &kafka.LeastBytes{},
	}
	e.logger.Info("kafka endpoint connected",
		"name", e.name,
		"brokers", strings.Join(e.brokers, ","),
	)
	return nil
}

func (e *Endpoint) Disconnect(ctx context.Context) error {
	// Stop all consumers
	e.consumers.Range(func(key, val any) bool {
		reader := val.(*kafka.Reader)
		reader.Close()
		e.consumers.Delete(key)
		return true
	})
	if e.writer != nil {
		return e.writer.Close()
	}
	return nil
}

// StartConsumer creates a dedicated Kafka reader for this session.
// It blocks until ctx is cancelled (client disconnect).
// When the output channel is full, FetchMessage blocks → Kafka consumer stops polling → backpressure.
func (e *Endpoint) StartConsumer(
	ctx context.Context,
	session *core.Session,
	ch chan<- core.BrokerMessage,
) error {
	if e.topicIn == "" {
		<-ctx.Done()
		return nil
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  e.brokers,
		Topic:    e.topicIn,
		GroupID:  fmt.Sprintf("mediation-%s-%s", e.name, session.ID),
		MaxWait:  500 * time.Millisecond,
	})

	e.consumers.Store(session.ID, reader)
	defer func() {
		e.consumers.Delete(session.ID)
		reader.Close()
	}()

	for {
		// FetchMessage does NOT auto-commit. We control ack timing.
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil // session cancelled — clean exit
			}
			e.logger.Error("kafka fetch error", "session_id", session.ID, "error", err)
			continue
		}

		evt := core.Event{
			ID:        uuid.New().String(),
			SourceID:  e.name,
			ClientID:  session.ClientID,
			Payload:   msg.Value,
			Type:      core.EventTypeData,
			Timestamp: time.Now(),
			Metadata: map[string]string{
				"protocol":  "kafka",
				"topic":     msg.Topic,
				"partition": fmt.Sprintf("%d", msg.Partition),
				"offset":    fmt.Sprintf("%d", msg.Offset),
			},
		}

		brokerMsg := core.BrokerMessage{
			Event: evt,
			Ack: func() error {
				return reader.CommitMessages(ctx, msg)
			},
			Nack: func() error {
				// Kafka doesn't have nack — message will be redelivered on rebalance
				return nil
			},
		}

		// Handle delivery guarantee at the endpoint level
		switch session.Route.DeliveryGuarantee {
		case core.DeliveryNone:
			// Ack immediately, put on channel, don't care
			reader.CommitMessages(ctx, msg)
			select {
			case ch <- brokerMsg:
			case <-ctx.Done():
				return nil
			}

		case core.DeliveryAuto:
			// Ack on channel write
			select {
			case ch <- brokerMsg:
				reader.CommitMessages(ctx, msg)
			case <-ctx.Done():
				return nil
			}

		case core.DeliveryAtMostOnce, core.DeliveryAtLeastOnce:
			// Ack is deferred to the entrypoint via brokerMsg.Ack()
			// This write blocks if channel is full → backpressure to Kafka
			select {
			case ch <- brokerMsg:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func (e *Endpoint) StopConsumer(sessionID string) error {
	val, ok := e.consumers.LoadAndDelete(sessionID)
	if !ok {
		return nil
	}
	return val.(*kafka.Reader).Close()
}

func (e *Endpoint) Send(ctx context.Context, evt core.Event) error {
	return e.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(evt.ID),
		Value: evt.Payload,
	})
}
````

### 12.2 RabbitMQ Endpoint

````go
package rabbitmq

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"mediation-engine/pkg/core"
)

type Endpoint struct {
	name     string
	url      string
	queueIn  string
	queueOut string
	conn     *amqp.Connection
	pubCh    *amqp.Channel // shared publish channel
	logger   *slog.Logger
	consumers sync.Map // sessionID → *amqp.Channel
}

func New(name, url, queueIn, queueOut string, logger *slog.Logger) *Endpoint {
	return &Endpoint{
		name:     name,
		url:      url,
		queueIn:  queueIn,
		queueOut: queueOut,
		logger:   logger,
	}
}

func (e *Endpoint) Name() string { return e.name }
func (e *Endpoint) Type() string { return "rabbitmq" }

func (e *Endpoint) Connect(ctx context.Context) error {
	var err error
	e.conn, err = amqp.Dial(e.url)
	if err != nil {
		return fmt.Errorf("rabbitmq dial: %w", err)
	}

	e.pubCh, err = e.conn.Channel()
	if err != nil {
		return fmt.Errorf("rabbitmq publish channel: %w", err)
	}

	// Declare queues
	for _, q := range []string{e.queueIn, e.queueOut} {
		if q != "" {
			_, err = e.pubCh.QueueDeclare(q, true, false, false, false, nil)
			if err != nil {
				return fmt.Errorf("rabbitmq queue declare %s: %w", q, err)
			}
		}
	}

	e.logger.Info("rabbitmq endpoint connected", "name", e.name, "url", e.url)
	return nil
}

func (e *Endpoint) Disconnect(ctx context.Context) error {
	e.consumers.Range(func(key, val any) bool {
		ch := val.(*amqp.Channel)
		ch.Close()
		e.consumers.Delete(key)
		return true
	})
	if e.pubCh != nil {
		e.pubCh.Close()
	}
	if e.conn != nil {
		return e.conn.Close()
	}
	return nil
}

// StartConsumer creates a dedicated AMQP channel with prefetch=1 for this session.
// Prefetch=1 ensures the broker sends at most 1 unacked message — maximum backpressure.
func (e *Endpoint) StartConsumer(
	ctx context.Context,
	session *core.Session,
	ch chan<- core.BrokerMessage,
) error {
	if e.queueIn == "" {
		<-ctx.Done()
		return nil
	}

	consumerCh, err := e.conn.Channel()
	if err != nil {
		return fmt.Errorf("rabbitmq consumer channel: %w", err)
	}

	// Prefetch 1 — broker delivers exactly 1 message at a time per consumer.
	// Combined with the bounded session channel, this creates end-to-end backpressure.
	consumerCh.Qos(1, 0, false)

	consumerTag := fmt.Sprintf("mediation-%s-%s", e.name, session.ID)
	deliveries, err := consumerCh.Consume(
		e.queueIn,
		consumerTag,
		false, // autoAck=false — we control ack timing
		false, false, false, nil,
	)
	if err != nil {
		consumerCh.Close()
		return fmt.Errorf("rabbitmq consume: %w", err)
	}

	e.consumers.Store(session.ID, consumerCh)
	defer func() {
		e.consumers.Delete(session.ID)
		consumerCh.Cancel(consumerTag, false)
		consumerCh.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil

		case delivery, ok := <-deliveries:
			if !ok {
				return nil
			}

			evt := core.Event{
				ID:        uuid.New().String(),
				SourceID:  e.name,
				ClientID:  session.ClientID,
				Payload:   delivery.Body,
				Type:      core.EventTypeData,
				Timestamp: time.Now(),
				Metadata: map[string]string{
					"protocol":   "rabbitmq",
					"queue":      e.queueIn,
					"message_id": delivery.MessageId,
				},
			}

			// Capture delivery tag for ack/nack
			dtag := delivery.DeliveryTag
			brokerMsg := core.BrokerMessage{
				Event: evt,
				Ack: func() error {
					return consumerCh.Ack(dtag, false)
				},
				Nack: func() error {
					return consumerCh.Nack(dtag, false, true) // requeue=true
				},
			}

			switch session.Route.DeliveryGuarantee {
			case core.DeliveryNone:
				consumerCh.Ack(dtag, false)
				select {
				case ch <- brokerMsg:
				case <-ctx.Done():
					return nil
				}

			case core.DeliveryAuto:
				select {
				case ch <- brokerMsg:
					consumerCh.Ack(dtag, false)
				case <-ctx.Done():
					return nil
				}

			case core.DeliveryAtMostOnce, core.DeliveryAtLeastOnce:
				// Ack deferred to entrypoint
				// This send blocks if channel full → Qos(1) means broker stops delivering
				select {
				case ch <- brokerMsg:
				case <-ctx.Done():
					return nil
				}
			}
		}
	}
}

func (e *Endpoint) StopConsumer(sessionID string) error {
	val, ok := e.consumers.LoadAndDelete(sessionID)
	if !ok {
		return nil
	}
	return val.(*amqp.Channel).Close()
}

func (e *Endpoint) Send(ctx context.Context, evt core.Event) error {
	return e.pubCh.PublishWithContext(ctx,
		"", e.queueOut, false, false,
		amqp.Publishing{
			ContentType: "application/octet-stream",
			Body:        evt.Payload,
			MessageId:   evt.ID,
			Timestamp:   time.Now(),
		},
	)
}
````

### 12.3 MQTT 5.0 Endpoint

````go
package mqtt5

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/google/uuid"
	"mediation-engine/pkg/core"
)

type Endpoint struct {
	name      string
	brokerURL string
	topicIn   string
	topicOut  string
	cm        *autopaho.ConnectionManager
	logger    *slog.Logger
	// Per-session message channels — MQTT5 shared subscriptions deliver per-message
	consumers sync.Map // sessionID → chan paho.PublishReceived
}

func New(name, brokerURL, topicIn, topicOut string, logger *slog.Logger) *Endpoint {
	return &Endpoint{
		name:      name,
		brokerURL: brokerURL,
		topicIn:   topicIn,
		topicOut:  topicOut,
		logger:    logger,
	}
}

func (e *Endpoint) Name() string { return e.name }
func (e *Endpoint) Type() string { return "mqtt5" }

func (e *Endpoint) Connect(ctx context.Context) error {
	serverURL, err := url.Parse(e.brokerURL)
	if err != nil {
		return fmt.Errorf("mqtt5 invalid URL: %w", err)
	}

	cfg := autopaho.ClientConfig{
		ServerUrls: []*url.URL{serverURL},
		KeepAlive:  30,
		OnConnectionUp: func(cm *autopaho.ConnectionManager, connAck *paho.Connack) {
			e.logger.Info("mqtt5 connected", "name", e.name)
		},
		ClientConfig: paho.ClientConfig{
			ClientID: fmt.Sprintf("mediation-%s", e.name),
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				func(pr paho.PublishReceived) (bool, error) {
					// Dispatch to the correct session consumer
					// For MQTT5 shared subscriptions, messages are distributed.
					// We fan out to all active consumers for this endpoint.
					e.consumers.Range(func(_, val any) bool {
						ch := val.(chan paho.PublishReceived)
						select {
						case ch <- pr:
						default:
							// Consumer channel full — drop for this session (backpressure)
						}
						return true
					})
					return true, nil
				},
			},
		},
	}

	e.cm, err = autopaho.NewConnection(ctx, cfg)
	if err != nil {
		return fmt.Errorf("mqtt5 connection: %w", err)
	}

	e.logger.Info("mqtt5 endpoint connected", "name", e.name, "broker", e.brokerURL)
	return nil
}

func (e *Endpoint) Disconnect(ctx context.Context) error {
	if e.cm != nil {
		return e.cm.Disconnect(ctx)
	}
	return nil
}

func (e *Endpoint) StartConsumer(
	ctx context.Context,
	session *core.Session,
	ch chan<- core.BrokerMessage,
) error {
	if e.topicIn == "" {
		<-ctx.Done()
		return nil
	}

	// Subscribe for this session
	_, err := e.cm.Subscribe(ctx, &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{Topic: e.topicIn, QoS: 1},
		},
	})
	if err != nil {
		return fmt.Errorf("mqtt5 subscribe: %w", err)
	}

	// Per-session receive channel
	mqttCh := make(chan paho.PublishReceived, 1)
	e.consumers.Store(session.ID, mqttCh)
	defer func() {
		e.consumers.Delete(session.ID)
		close(mqttCh)
	}()

	for {
		select {
		case <-ctx.Done():
			return nil

		case pr := <-mqttCh:
			evt := core.Event{
				ID:        uuid.New().String(),
				SourceID:  e.name,
				ClientID:  session.ClientID,
				Payload:   pr.Packet.Payload,
				Type:      core.EventTypeData,
				Timestamp: time.Now(),
				Metadata: map[string]string{
					"protocol": "mqtt5",
					"topic":    pr.Packet.Topic,
				},
			}

			brokerMsg := core.BrokerMessage{
				Event: evt,
				Ack:   func() error { return nil }, // MQTT QoS ack handled by paho
				Nack:  func() error { return nil },
			}

			select {
			case ch <- brokerMsg:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func (e *Endpoint) StopConsumer(sessionID string) error {
	e.consumers.Delete(sessionID)
	return nil
}

func (e *Endpoint) Send(ctx context.Context, evt core.Event) error {
	_, err := e.cm.Publish(ctx, &paho.Publish{
		Topic:   e.topicOut,
		QoS:     1,
		Payload: evt.Payload,
	})
	return err
}
````

### 12.4 Solace Endpoint

````go
package solace

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"mediation-engine/pkg/core"
	"solace.dev/go/messaging"
	"solace.dev/go/messaging/pkg/solace/config"
	"solace.dev/go/messaging/pkg/solace/message"
	"solace.dev/go/messaging/pkg/solace/resource"
)

type Endpoint struct {
	name     string
	host     string
	vpn      string
	username string
	password string
	topicIn  string
	topicOut string
	service  messaging.MessagingService
	logger   *slog.Logger
	consumers sync.Map // sessionID → context.CancelFunc
}

func New(name, host, vpn, username, password, topicIn, topicOut string, logger *slog.Logger) *Endpoint {
	return &Endpoint{
		name: name, host: host, vpn: vpn,
		username: username, password: password,
		topicIn: topicIn, topicOut: topicOut,
		logger: logger,
	}
}

func (e *Endpoint) Name() string { return e.name }
func (e *Endpoint) Type() string { return "solace" }

func (e *Endpoint) Connect(ctx context.Context) error {
	var err error
	e.service, err = messaging.NewMessagingServiceBuilder().
		FromConfigurationProvider(config.ServicePropertyMap{
			config.TransportLayerPropertyHost:                e.host,
			config.ServicePropertyVPNName:                    e.vpn,
			config.AuthenticationPropertySchemeBasicUserName: e.username,
			config.AuthenticationPropertySchemeBasicPassword: e.password,
		}).Build()
	if err != nil {
		return fmt.Errorf("solace build: %w", err)
	}
	if err = e.service.Connect(); err != nil {
		return fmt.Errorf("solace connect: %w", err)
	}
	e.logger.Info("solace endpoint connected", "name", e.name, "host", e.host)
	return nil
}

func (e *Endpoint) Disconnect(ctx context.Context) error {
	if e.service != nil {
		e.service.Disconnect()
	}
	return nil
}

func (e *Endpoint) StartConsumer(
	ctx context.Context,
	session *core.Session,
	ch chan<- core.BrokerMessage,
) error {
	if e.topicIn == "" {
		<-ctx.Done()
		return nil
	}

	receiver, err := e.service.CreateDirectMessageReceiverBuilder().
		WithSubscriptions(resource.TopicSubscriptionOf(e.topicIn)).
		Build()
	if err != nil {
		return fmt.Errorf("solace receiver build: %w", err)
	}
	if err = receiver.Start(); err != nil {
		return fmt.Errorf("solace receiver start: %w", err)
	}

	e.consumers.Store(session.ID, struct{}{})
	defer func() {
		e.consumers.Delete(session.ID)
		receiver.Terminate(5 * time.Second)
	}()

	for {
		inMsg, err := receiver.ReceiveMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			e.logger.Error("solace receive error", "error", err)
			continue
		}

		payload, _ := inMsg.GetPayloadAsBytes()
		dest := ""
		if inMsg.GetDestinationName() != "" {
			dest = inMsg.GetDestinationName()
		}

		evt := core.Event{
			ID:        uuid.New().String(),
			SourceID:  e.name,
			ClientID:  session.ClientID,
			Payload:   payload,
			Type:      core.EventTypeData,
			Timestamp: time.Now(),
			Metadata: map[string]string{
				"protocol":    "solace",
				"destination": dest,
			},
		}

		brokerMsg := core.BrokerMessage{
			Event: evt,
			Ack:   func() error { return nil }, // Direct messaging — no ack needed
			Nack:  func() error { return nil },
		}

		select {
		case ch <- brokerMsg:
		case <-ctx.Done():
			return nil
		}
	}
}

func (e *Endpoint) StopConsumer(sessionID string) error {
	e.consumers.Delete(sessionID)
	return nil
}

func (e *Endpoint) Send(ctx context.Context, evt core.Event) error {
	publisher, err := e.service.CreateDirectMessagePublisherBuilder().Build()
	if err != nil {
		return err
	}
	if err = publisher.Start(); err != nil {
		return err
	}
	defer publisher.Terminate(5 * time.Second)

	msg, err := e.service.MessageBuilder().BuildWithByteArrayPayload(evt.Payload)
	if err != nil {
		return err
	}
	return publisher.Publish(msg, resource.TopicOf(e.topicOut))
}
````

### 12.5 JMS Endpoint (via AMQP 1.0)

````go
package jms

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Azure/go-amqp"
	"github.com/google/uuid"
	"mediation-engine/pkg/core"
)

type Endpoint struct {
	name     string
	url      string
	queueIn  string
	queueOut string
	conn     *amqp.Conn
	sendSess *amqp.Session
	sender   *amqp.Sender
	logger   *slog.Logger
	consumers sync.Map // sessionID → *amqp.Receiver
}

func New(name, url, queueIn, queueOut string, logger *slog.Logger) *Endpoint {
	return &Endpoint{
		name: name, url: url,
		queueIn: queueIn, queueOut: queueOut,
		logger: logger,
	}
}

func (e *Endpoint) Name() string { return e.name }
func (e *Endpoint) Type() string { return "jms" }

func (e *Endpoint) Connect(ctx context.Context) error {
	var err error
	e.conn, err = amqp.Dial(ctx, e.url, nil)
	if err != nil {
		return fmt.Errorf("jms dial: %w", err)
	}

	if e.queueOut != "" {
		e.sendSess, err = e.conn.NewSession(ctx, nil)
		if err != nil {
			return fmt.Errorf("jms send session: %w", err)
		}
		e.sender, err = e.sendSess.NewSender(ctx, e.queueOut, nil)
		if err != nil {
			return fmt.Errorf("jms sender: %w", err)
		}
	}

	e.logger.Info("jms endpoint connected", "name", e.name, "url", e.url)
	return nil
}

func (e *Endpoint) Disconnect(ctx context.Context) error {
	e.consumers.Range(func(key, val any) bool {
		receiver := val.(*amqp.Receiver)
		receiver.Close(ctx)
		e.consumers.Delete(key)
		return true
	})
	if e.sender != nil {
		e.sender.Close(ctx)
	}
	if e.sendSess != nil {
		e.sendSess.Close(ctx)
	}
	if e.conn != nil {
		return e.conn.Close()
	}
	return nil
}

// StartConsumer creates a per-session AMQP 1.0 receiver with link credit = 1.
// Link credit controls flow: broker sends exactly 1 message until we accept it.
func (e *Endpoint) StartConsumer(
	ctx context.Context,
	session *core.Session,
	ch chan<- core.BrokerMessage,
) error {
	if e.queueIn == "" {
		<-ctx.Done()
		return nil
	}

	recvSess, err := e.conn.NewSession(ctx, nil)
	if err != nil {
		return fmt.Errorf("jms consumer session: %w", err)
	}

	receiver, err := recvSess.NewReceiver(ctx, e.queueIn, &amqp.ReceiverOptions{
		Credit: 1, // max 1 unacked message — maximum backpressure
	})
	if err != nil {
		recvSess.Close(ctx)
		return fmt.Errorf("jms receiver: %w", err)
	}

	e.consumers.Store(session.ID, receiver)
	defer func() {
		e.consumers.Delete(session.ID)
		receiver.Close(ctx)
		recvSess.Close(ctx)
	}()

	for {
		msg, err := receiver.Receive(ctx, nil)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			e.logger.Error("jms receive error", "error", err)
			continue
		}

		var payload []byte
		if len(msg.Data) > 0 {
			payload = msg.Data[0]
		}

		evt := core.Event{
			ID:        uuid.New().String(),
			SourceID:  e.name,
			ClientID:  session.ClientID,
			Payload:   payload,
			Type:      core.EventTypeData,
			Timestamp: time.Now(),
			Metadata: map[string]string{
				"protocol": "jms_amqp10",
			},
		}

		capturedMsg := msg
		brokerMsg := core.BrokerMessage{
			Event: evt,
			Ack: func() error {
				return receiver.AcceptMessage(ctx, capturedMsg)
			},
			Nack: func() error {
				return receiver.RejectMessage(ctx, capturedMsg, nil)
			},
		}

		switch session.Route.DeliveryGuarantee {
		case core.DeliveryNone:
			receiver.AcceptMessage(ctx, capturedMsg)
			select {
			case ch <- brokerMsg:
			case <-ctx.Done():
				return nil
			}

		case core.DeliveryAuto:
			select {
			case ch <- brokerMsg:
				receiver.AcceptMessage(ctx, capturedMsg)
			case <-ctx.Done():
				return nil
			}

		case core.DeliveryAtMostOnce, core.DeliveryAtLeastOnce:
			// Ack deferred to entrypoint
			// credit=1 → broker won't send next until this is acked
			select {
			case ch <- brokerMsg:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func (e *Endpoint) StopConsumer(sessionID string) error {
	val, ok := e.consumers.LoadAndDelete(sessionID)
	if !ok {
		return nil
	}
	return val.(*amqp.Receiver).Close(context.Background())
}

func (e *Endpoint) Send(ctx context.Context, evt core.Event) error {
	return e.sender.Send(ctx, &amqp.Message{
		Data: [][]byte{evt.Payload},
		Properties: &amqp.MessageProperties{
			MessageID: evt.ID,
		},
	}, nil)
}
````

---

## 13. Plugin Registry

````go
package plugins

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"mediation-engine/pkg/core"
)

type Registry struct {
	entrypoints map[string]core.Entrypoint
	endpoints   map[string]core.Endpoint
	logger      *slog.Logger
	mu          sync.RWMutex
}

func NewRegistry(logger *slog.Logger) *Registry {
	return &Registry{
		entrypoints: make(map[string]core.Entrypoint),
		endpoints:   make(map[string]core.Endpoint),
		logger:      logger,
	}
}

func (r *Registry) RegisterEntrypoint(e core.Entrypoint) {
	r.mu.Lock()
	r.entrypoints[e.Name()] = e
	r.mu.Unlock()
}

func (r *Registry) RegisterEndpoint(e core.Endpoint) {
	r.mu.Lock()
	r.endpoints[e.Name()] = e
	r.mu.Unlock()
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

// ConnectEndpoints connects all endpoints to their brokers.
func (r *Registry) ConnectEndpoints(ctx context.Context) error {
	for name, ep := range r.endpoints {
		if err := ep.Connect(ctx); err != nil {
			return fmt.Errorf("endpoint %s connect: %w", name, err)
		}
	}
	return nil
}

// StartEntrypoints starts all entrypoints. Non-blocking — each runs in its own goroutine.
func (r *Registry) StartEntrypoints(ctx context.Context, manager core.SessionManager) {
	for name, ep := range r.entrypoints {
		go func(n string, e core.Entrypoint) {
			r.logger.Info("starting entrypoint", "name", n, "type", e.Type())
			if err := e.Start(ctx, manager); err != nil && ctx.Err() == nil {
				r.logger.Error("entrypoint failed", "name", n, "error", err)
			}
		}(name, ep)
	}
}

// StopAll stops entrypoints first (stop accepting), then endpoints (drain brokers).
func (r *Registry) StopAll(ctx context.Context) {
	for name, ep := range r.entrypoints {
		r.logger.Info("stopping entrypoint", "name", name)
		ep.Stop(ctx)
	}
	for name, ep := range r.endpoints {
		r.logger.Info("stopping endpoint", "name", name)
		ep.Disconnect(ctx)
	}
}
````

---

## 14. Configuration

### 14.1 YAML Format

````yaml
entrypoints:
  - name: ws-inbound
    type: websocket
    port: 8066
  - name: sse-feed
    type: sse
    port: 8067
  - name: webhook-receiver
    type: http_post
    port: 8068
  - name: polling-api
    type: http_get
    port: 8069

endpoints:
  - name: kafka-main
    type: kafka
    config:
      brokers: "localhost:9092"
      topic_in: "events-in"
      topic_out: "events-out"
  - name: rabbitmq-orders
    type: rabbitmq
    config:
      url: "amqp://guest:guest@localhost:5672/"
      queue_in: "orders-in"
      queue_out: "orders-out"
  - name: mqtt5-iot
    type: mqtt5
    config:
      broker: "tcp://localhost:1883"
      topic_in: "sensors/+"
      topic_out: "commands"
  - name: solace-events
    type: solace
    config:
      host: "tcp://localhost:55555"
      vpn: "default"
      username: "admin"
      password: "admin"
      topic_in: "events/>"
      topic_out: "processed/events"
  - name: jms-legacy
    type: jms
    config:
      url: "amqp://localhost:5672"
      queue_in: "legacy.requests"
      queue_out: "legacy.responses"

routes:
  - source: ws-inbound
    target: kafka-main
    direction: upstream
    delivery_guarantee: at_least_once
    channel_size: 1          # maximum backpressure

  - source: kafka-main
    target: ws-inbound
    direction: downstream
    delivery_guarantee: at_least_once
    channel_size: 1

  - source: webhook-receiver
    target: rabbitmq-orders
    direction: upstream
    delivery_guarantee: auto
    channel_size: 10

  - source: mqtt5-iot
    target: solace-events
    direction: upstream
    delivery_guarantee: none
    channel_size: 5
````

### 14.2 Config Loader

````go
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	"mediation-engine/pkg/core"
)

type Config struct {
	Entrypoints []EntrypointConfig `yaml:"entrypoints"`
	Endpoints   []EndpointConfig   `yaml:"endpoints"`
	Routes      []RouteConfig      `yaml:"routes"`
}

type EntrypointConfig struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	Port int    `yaml:"port"`
}

type EndpointConfig struct {
	Name   string            `yaml:"name"`
	Type   string            `yaml:"type"`
	Config map[string]string `yaml:"config"`
}

type RouteConfig struct {
	Source            string `yaml:"source"`
	Target            string `yaml:"target"`
	Direction         string `yaml:"direction"`          // "upstream" | "downstream"
	DeliveryGuarantee string `yaml:"delivery_guarantee"` // "none" | "auto" | "at_most_once" | "at_least_once"
	ChannelSize       int    `yaml:"channel_size"`       // per-session buffer size
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

func ParseDirection(s string) core.Direction {
	if s == "downstream" {
		return core.DirectionDownstream
	}
	return core.DirectionUpstream
}

func ParseDeliveryGuarantee(s string) core.DeliveryGuarantee {
	switch s {
	case "none":
		return core.DeliveryNone
	case "at_most_once":
		return core.DeliveryAtMostOnce
	case "at_least_once":
		return core.DeliveryAtLeastOnce
	default:
		return core.DeliveryAuto
	}
}
````

### 14.3 Config Watcher (Hot-Reload)

````go
package config

import (
	"context"
	"log/slog"
	"os"
	"time"

	"mediation-engine/internal/routing"
	"mediation-engine/pkg/core"
)

type Watcher struct {
	path     string
	table    *routing.Table
	interval time.Duration
	logger   *slog.Logger
	lastMod  time.Time
}

func NewWatcher(path string, table *routing.Table, logger *slog.Logger) *Watcher {
	return &Watcher{
		path:     path,
		table:    table,
		interval: 5 * time.Second,
		logger:   logger,
	}
}

func (w *Watcher) Watch(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info, err := os.Stat(w.path)
			if err != nil {
				continue
			}
			if info.ModTime().After(w.lastMod) {
				w.lastMod = info.ModTime()
				cfg, err := Load(w.path)
				if err != nil {
					w.logger.Error("config reload failed", "error", err)
					continue
				}
				routes := make([]*core.Route, 0, len(cfg.Routes))
				for _, r := range cfg.Routes {
					routes = append(routes, &core.Route{
						Source:            r.Source,
						Target:            r.Target,
						Direction:         ParseDirection(r.Direction),
						DeliveryGuarantee: ParseDeliveryGuarantee(r.DeliveryGuarantee),
						ChannelSize:       r.ChannelSize,
					})
				}
				w.table.ReplaceAll(routes)
				w.logger.Info("routes reloaded", "count", len(routes))
			}
		}
	}
}
````

---

## 15. Main Entry Point

````go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"mediation-engine/internal/logging"
	"mediation-engine/internal/routing"
	"mediation-engine/internal/session"
	"mediation-engine/pkg/config"
	"mediation-engine/pkg/core"
	"mediation-engine/pkg/plugins"
	"mediation-engine/pkg/plugins/httpget"
	"mediation-engine/pkg/plugins/httppost"
	"mediation-engine/pkg/plugins/jms"
	"mediation-engine/pkg/plugins/kafka"
	"mediation-engine/pkg/plugins/mqtt5"
	"mediation-engine/pkg/plugins/rabbitmq"
	"mediation-engine/pkg/plugins/solace"
	"mediation-engine/pkg/plugins/sse"
	"mediation-engine/pkg/plugins/ws"
)

func main() {
	// Structured JSON logger — Fluent Bit extracts from stdout
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// 1. Load configuration
	configPath := "config.yaml"
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		configPath = p
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// 2. Signal-aware context
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 3. Route table
	routeTable := routing.NewTable()
	for _, r := range cfg.Routes {
		routeTable.Add(&core.Route{
			Source:            r.Source,
			Target:            r.Target,
			Direction:         config.ParseDirection(r.Direction),
			DeliveryGuarantee: config.ParseDeliveryGuarantee(r.DeliveryGuarantee),
			ChannelSize:       r.ChannelSize,
		})
	}

	// 4. Packet logger
	packetLog := logging.NewPacketLogger(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// 5. Plugin registry — register all entrypoints and endpoints
	reg := plugins.NewRegistry(logger)
	registerEntrypoints(cfg, reg, logger, packetLog)
	registerEndpoints(cfg, reg, logger)

	// 6. Connect all endpoints to their brokers
	if err := reg.ConnectEndpoints(ctx); err != nil {
		logger.Error("failed to connect endpoints", "error", err)
		os.Exit(1)
	}

	// 7. Session manager — pairs client connections with broker consumers
	mgr := session.NewManager(routeTable, reg.Endpoints(), logger, packetLog)

	// 8. Start entrypoints (they use the session manager when clients connect)
	reg.StartEntrypoints(ctx, mgr)

	// 9. Config hot-reload watcher
	watcher := config.NewWatcher(configPath, routeTable, logger)
	go watcher.Watch(ctx)

	// 10. Health endpoint
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"status":"ok","sessions":%d}`, mgr.ActiveCount())))
		})
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ready"}`))
		})
		srv := &http.Server{Addr: ":9091", Handler: mux}
		go func() { <-ctx.Done(); srv.Shutdown(context.Background()) }()
		logger.Info("health server starting", "addr", ":9091")
		srv.ListenAndServe()
	}()

	logger.Info("mediation engine started",
		"entrypoints", len(cfg.Entrypoints),
		"endpoints", len(cfg.Endpoints),
		"routes", len(cfg.Routes),
	)

	// 11. Block until shutdown signal
	<-ctx.Done()
	logger.Info("shutting down...")

	// 12. Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	mgr.DestroyAll()                // destroy all sessions → stop all consumers
	reg.StopAll(shutdownCtx)        // stop entrypoints, then disconnect endpoints

	logger.Info("mediation engine stopped")
}

func registerEntrypoints(cfg *config.Config, reg *plugins.Registry, logger *slog.Logger, packetLog *logging.PacketLogger) {
	for _, e := range cfg.Entrypoints {
		switch e.Type {
		case "websocket":
			reg.RegisterEntrypoint(ws.New(e.Name, e.Port, logger, packetLog))
		case "sse":
			reg.RegisterEntrypoint(sse.New(e.Name, e.Port, logger, packetLog))
		case "http_post":
			reg.RegisterEntrypoint(httppost.New(e.Name, e.Port, logger, packetLog))
		case "http_get":
			reg.RegisterEntrypoint(httpget.New(e.Name, e.Port, logger, packetLog))
		default:
			logger.Warn("unknown entrypoint type", "type", e.Type, "name", e.Name)
		}
	}
}

func registerEndpoints(cfg *config.Config, reg *plugins.Registry, logger *slog.Logger) {
	for _, e := range cfg.Endpoints {
		switch e.Type {
		case "kafka":
			brokers := strings.Split(e.Config["brokers"], ",")
			reg.RegisterEndpoint(kafka.New(e.Name, brokers, e.Config["topic_in"], e.Config["topic_out"], logger))
		case "rabbitmq":
			reg.RegisterEndpoint(rabbitmq.New(e.Name, e.Config["url"], e.Config["queue_in"], e.Config["queue_out"], logger))
		case "mqtt5":
			reg.RegisterEndpoint(mqtt5.New(e.Name, e.Config["broker"], e.Config["topic_in"], e.Config["topic_out"], logger))
		case "solace":
			reg.RegisterEndpoint(solace.New(e.Name, e.Config["host"], e.Config["vpn"],
				e.Config["username"], e.Config["password"],
				e.Config["topic_in"], e.Config["topic_out"], logger))
		case "jms":
			reg.RegisterEndpoint(jms.New(e.Name, e.Config["url"], e.Config["queue_in"], e.Config["queue_out"], logger))
		default:
			logger.Warn("unknown endpoint type", "type", e.Type, "name", e.Name)
		}
	}
}
````

---

## 16. Container Co-location

The mediation engine binary is built separately and placed alongside the policy engine and Envoy router in a single `gateway-runtime` container.

````dockerfile
FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o mediation-engine cmd/server/main.go
````

The `gateway-runtime` container includes all three:

```
gateway-runtime/
├── /usr/local/bin/envoy
├── /usr/local/bin/policy-engine
├── /usr/local/bin/mediation-engine
├── /etc/envoy/envoy.yaml
├── /etc/mediation/config.yaml
└── /usr/local/bin/entrypoint.sh
```

```bash
#!/bin/sh
# entrypoint.sh — starts all processes in the gateway-runtime container
envoy -c /etc/envoy/envoy.yaml &
policy-engine --extproc-port 9001 --xds-port 9002 &
CONFIG_PATH=/etc/mediation/config.yaml mediation-engine &
wait
```

---

## 17. Go Module Dependencies

```
# Entrypoints
github.com/gorilla/websocket         # WebSocket
# SSE, HTTP GET, HTTP POST use net/http only

# Endpoints
github.com/segmentio/kafka-go         # Kafka
github.com/rabbitmq/amqp091-go        # RabbitMQ (AMQP 0-9-1)
github.com/eclipse/paho.golang        # MQTT 5.0
solace.dev/go/messaging               # Solace
github.com/Azure/go-amqp              # JMS via AMQP 1.0

# Core
github.com/google/uuid                # Event IDs
gopkg.in/yaml.v3                      # Config
```

---

## 18. Phased Implementation Timeline

| Phase | Days | Deliverable |
|---|---|---|
| **1. Core types & interfaces** | 1–2 | `pkg/core/` — Event, BrokerMessage, Session, Route, all interfaces |
| **2. Session manager & routing** | 2–3 | `internal/session/manager.go`, `internal/routing/table.go` |
| **3. Packet logger** | 3 | `internal/logging/packet.go` |
| **4. WebSocket + SSE entrypoints** | 3–5 | `pkg/plugins/ws/`, `pkg/plugins/sse/` — with flow control |
| **5. HTTP POST + GET entrypoints** | 5–6 | `pkg/plugins/httppost/`, `pkg/plugins/httpget/` |
| **6. Kafka + RabbitMQ endpoints** | 6–8 | `pkg/plugins/kafka/`, `pkg/plugins/rabbitmq/` — with per-session consumers |
| **7. MQTT5 + Solace + JMS endpoints** | 8–10 | `pkg/plugins/mqtt5/`, `pkg/plugins/solace/`, `pkg/plugins/jms/` |
| **8. Plugin registry & main wiring** | 10–11 | `pkg/plugins/registry.go`, `cmd/server/main.go` |
| **9. Config hot-reload** | 11 | `pkg/config/watcher.go` |
| **10. Container integration** | 12 | Dockerfile, entry script |
| **11. Tests** | 12–14 | Unit + integration |

---

## 19. Testing Strategy

| Layer | What | How |
|---|---|---|
| **Session manager** | Session create/destroy, lifecycle coupling | Unit tests with mock endpoints |
| **Route table** | Concurrent read/write, ReplaceAll | Race detector (`go test -race`) |
| **Flow control** | Slow client blocks broker consumer | Integration test: slow WS client + Kafka producer |
| **Delivery guarantees** | Ack timing for each mode | Unit test per guarantee mode with mock broker ack |
| **Entrypoints** | WS upgrade, SSE stream, HTTP POST, HTTP GET poll | `httptest.Server` |
| **Endpoints** | Kafka/RabbitMQ/MQTT5 consume and produce | Docker Compose + testcontainers |
| **Lifecycle coupling** | Client disconnect → consumer stops | Integration test: connect, send, disconnect, verify consumer gone |
| **Config watcher** | File change → route reload | Temp file + timer |
| **Full flow** | WS → Kafka, Kafka → SSE, POST → RabbitMQ | Docker Compose end-to-end |

---

## 20. Flow Control Summary

| Mechanism | Where | Effect |
|---|---|---|
| **Bounded session channel** | `Session.Downstream` (configurable size) | Endpoint consumer blocks when channel full |
| **Endpoint consumer blocks** | `StartConsumer` channel write | Broker-specific backpressure kicks in |
| **Kafka**: FetchMessage blocks | Consumer poll stops | Broker stops sending partitions |
| **RabbitMQ**: Qos(1) prefetch | Only 1 unacked delivery | Broker holds other messages |
| **MQTT5**: Per-session channel | Consumer drops if full | Receive maximum flow control |
| **Solace**: ReceiveMessage blocks | Direct receiver pauses | Broker flow control |
| **JMS/AMQP 1.0**: Credit=1 | 1 unacked message per link | Sender stops until credit issued |
| **Upstream channel** | `Session.Upstream` (bounded) | Client read blocks via TCP backpressure |
| **Session cancel** | `context.CancelFunc` | All goroutines exit on client disconnect |