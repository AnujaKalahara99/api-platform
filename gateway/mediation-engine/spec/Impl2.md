# Mediation Engine — Revised Reimplementation Plan

After reviewing the original plan thoroughly and incorporating your changes, here is the restructured plan.

---

## Key Changes from Original Plan

| Original | Revised |
|---|---|
| Entrypoints: WS, SSE only | Entrypoints: **WebSocket, SSE, HTTP GET, HTTP POST** |
| Endpoints: Kafka, MQTT, Mock | Endpoints: **Kafka, RabbitMQ, MQTT5, Solace, JMS** |
| Per-packet policy engine | **Removed entirely** — no policy evaluation on data packets |
| Retention/session stores | **Removed** — no persistence layer (needed later only for WebSub) |
| Standalone container | **Co-located** in same container as policy-engine + router (Envoy) |
| Silent event processing | **Structured logging** of packets for external extraction (Fluent Bit) |
| `internal/policy/` package | **Deleted** |
| `pkg/session/`, `pkg/retention/` | **Deleted** |
| Pipeline stages: 5 (lifecycle, route, policy, retention, delivery) | Pipeline stages: **3** (lifecycle, route, delivery) + logging middleware |
| WebSub protocol support | **Deferred** to future phase (brings back persistence layer) |

---

## 1. Revised Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    Single Container (gateway-runtime)                    │
│                                                                         │
│  ┌───────────┐    ┌─────────────┐    ┌──────────────────────────────┐  │
│  │  Envoy    │───▶│   Policy    │    │      Mediation Engine        │  │
│  │  Router   │    │   Engine    │    │                              │  │
│  │ (8080)    │    │  (9002)     │    │  ┌────────┐   ┌──────────┐  │  │
│  └───────────┘    └─────────────┘    │  │  Entry  │──▶│  Event   │  │  │
│                                      │  │  points │   │   Bus    │  │  │
│        HTTP traffic ─────────────────┼─▶│ WS,SSE  │   │ (worker  │  │  │
│                                      │  │ HTTP    │   │  pool)   │  │  │
│                                      │  └────────┘   └────┬─────┘  │  │
│                                      │                     │        │  │
│                                      │               ┌─────▼─────┐  │  │
│                                      │               │  Pipeline  │  │  │
│                                      │               │ Route+Log  │  │  │
│                                      │               │ +Deliver   │  │  │
│                                      │               └─────┬─────┘  │  │
│                                      │                     │        │  │
│                                      │               ┌─────▼─────┐  │  │
│                                      │               │ Endpoints  │  │  │
│                                      │               │ Kafka,AMQP │  │  │
│                                      │               │ MQTT5,     │  │  │
│                                      │               │ Solace,JMS │  │  │
│                                      │               └───────────┘  │  │
│                                      └──────────────────────────────┘  │
│                                                                         │
│  stdout → Fluent Bit (sidecar or host-level) → observability stack     │
└─────────────────────────────────────────────────────────────────────────┘
```

**Data flow**: Client → Envoy Router → (HTTP traffic to backends OR protocol upgrade/POST to Mediation Engine entrypoints) → Event Bus → Route lookup → Structured log → Endpoint delivery → Broker

---

## 2. Directory Structure

```
mediation-engine/
├── cmd/
│   └── server/
│       └── main.go                    # Minimal wiring, DI
├── internal/
│   ├── bus/
│   │   ├── bus.go                     # Event bus with worker pool
│   │   └── bus_test.go
│   ├── pipeline/
│   │   ├── pipeline.go               # Ordered stage execution
│   │   ├── pipeline_test.go
│   │   ├── stage_lifecycle.go         # Connect/disconnect handling
│   │   ├── stage_route.go            # Route resolution stage
│   │   ├── stage_log.go              # Structured packet logging (for Fluent Bit)
│   │   └── stage_delivery.go         # Final delivery stage
│   ├── routing/
│   │   ├── table.go                  # Lock-free route table (sync.Map)
│   │   └── table_test.go
│   ├── logging/
│   │   ├── packet_logger.go          # Structured JSON packet logger
│   │   └── packet_logger_test.go
│   └── health/
│       └── health.go                 # Health + readiness endpoints
├── pkg/
│   ├── core/
│   │   ├── interfaces.go            # All interfaces (NO policy, NO session, NO retention)
│   │   ├── types.go                 # Event, Route, EventContext
│   │   └── errors.go                # Domain errors
│   ├── plugins/
│   │   ├── registry.go              # Plugin registry with lifecycle
│   │   ├── ws/
│   │   │   └── entrypoint.go        # WebSocket entrypoint
│   │   ├── sse/
│   │   │   └── entrypoint.go        # SSE entrypoint
│   │   ├── httpget/
│   │   │   └── entrypoint.go        # HTTP GET polling entrypoint
│   │   ├── httppost/
│   │   │   └── entrypoint.go        # HTTP POST webhook entrypoint
│   │   ├── kafka/
│   │   │   └── endpoint.go          # Kafka endpoint
│   │   ├── rabbitmq/
│   │   │   └── endpoint.go          # RabbitMQ (AMQP 0-9-1) endpoint
│   │   ├── mqtt5/
│   │   │   └── endpoint.go          # MQTT 5.0 endpoint
│   │   ├── solace/
│   │   │   └── endpoint.go          # Solace (SMF/AMQP) endpoint
│   │   └── jms/
│   │       └── endpoint.go          # JMS endpoint (via AMQP 1.0 or native)
│   └── config/
│       ├── loader.go                # YAML configuration parsing
│       ├── loader_test.go
│       └── watcher.go               # File watcher for hot-reload
├── config.yaml
├── go.mod
├── Makefile
└── README.md
```

**What's removed vs original plan**:
- `internal/policy/` — entire package deleted (no packet-level policies)
- `pkg/session/` — deleted (no persistence layer)
- `pkg/retention/` — deleted (no persistence layer)
- `internal/metrics/` — replaced by structured logging to stdout (Fluent Bit extracts)
- `pkg/plugins/mock/` — removed (use real brokers or test doubles in tests)

**What's added**:
- `pkg/plugins/httpget/` — HTTP GET polling entrypoint
- `pkg/plugins/httppost/` — HTTP POST webhook receiver entrypoint
- `pkg/plugins/rabbitmq/` — RabbitMQ endpoint
- `pkg/plugins/mqtt5/` — MQTT 5.0 (replaces MQTT 3.1.1)
- `pkg/plugins/solace/` — Solace endpoint
- `pkg/plugins/jms/` — JMS endpoint
- `internal/logging/` — Structured packet logger for Fluent Bit consumption
- `internal/pipeline/stage_log.go` — Logging stage in pipeline

---

## 3. Core Abstractions (Simplified)

### 3.1 Interfaces — No Policy, No Session, No Retention

````go
package core

import "context"

// EventPublisher is the interface plugins use to submit events to the bus
type EventPublisher interface {
	Publish(ctx context.Context, evt Event) error // returns error on backpressure
}

// Entrypoint accepts inbound traffic from clients (WS, SSE, HTTP GET, HTTP POST)
type Entrypoint interface {
	Name() string
	Type() string
	Start(ctx context.Context, publisher EventPublisher) error // blocking, returns on ctx cancel
	Stop(ctx context.Context) error                            // graceful stop
	SendDownstream(ctx context.Context, evt Event) error       // send response to client
}

// Endpoint connects to backend broker systems (Kafka, RabbitMQ, MQTT5, Solace, JMS)
type Endpoint interface {
	Name() string
	Type() string
	Start(ctx context.Context, publisher EventPublisher) error // blocking, returns on ctx cancel
	Stop(ctx context.Context) error                            // graceful stop
	SendUpstream(ctx context.Context, evt Event) error         // send to broker
}

// PipelineStage is a single processing step in the event pipeline
type PipelineStage interface {
	Name() string
	Process(ctx context.Context, ec *EventContext) error
}

// Pipeline executes stages in order for each event
type Pipeline interface {
	Execute(ctx context.Context, evt Event)
}
````

### 3.2 Types — Simplified

````go
package core

import "time"

// Event is the protocol-agnostic data unit flowing through the engine
type Event struct {
	ID        string            `json:"id"`
	SourceID  string            `json:"source_id"`  // matches route.Source
	ClientID  string            `json:"client_id"`  // connection/session tracking
	Payload   []byte            `json:"payload"`
	Metadata  map[string]string `json:"metadata"`
	Timestamp time.Time         `json:"timestamp"`
	Type      EventType         `json:"type"`
}

// EventType distinguishes data events from lifecycle events
type EventType int

const (
	EventTypeData       EventType = iota // normal data packet
	EventTypeConnect                     // client connected
	EventTypeDisconnect                  // client disconnected
)

func (e Event) IsLifecycle() bool {
	return e.Type == EventTypeConnect || e.Type == EventTypeDisconnect
}

// EventContext carries the event through the pipeline with accumulated state
type EventContext struct {
	Event Event
	Route *Route // resolved in route stage, nil if no route found
	Err   error  // first error encountered
}

// Route defines a mapping from a source to a target with direction
type Route struct {
	Source    string    `yaml:"source"`
	Target   string    `yaml:"target"`
	Direction Direction `yaml:"direction"` // explicit: upstream or downstream
}

// Direction indicates whether events flow to a broker or back to a client
type Direction int

const (
	DirectionUpstream   Direction = iota // entrypoint → endpoint (client to broker)
	DirectionDownstream                  // endpoint → entrypoint (broker to client)
)
````

### 3.3 Domain Errors

````go
package core

import "errors"

var (
	ErrNoRoute        = errors.New("no route found for source")
	ErrTargetNotFound = errors.New("delivery target not found")
	ErrBusFull        = errors.New("event bus queue full")
	ErrShuttingDown   = errors.New("engine is shutting down")
)
````

**Key differences from original plan**:
- No `PolicyAction`, `PolicySpec`, `Session`, `RetentionConfig`, `RetentionStore`, `SessionStore`, `RoutePolicyEngine` types
- `Route` has explicit `Direction` field instead of guessing from target name lookups
- `EventContext` has no `ModifiedEvent`, `PolicyAction`, or `Targets` fields — events pass through unmodified

---

## 4. Phased Implementation Plan

### Phase 1: Core Types & Event Bus (Days 1–2)

**Goal**: Foundation types + bounded worker pool replacing unbounded goroutines.

#### 1.1 Event Bus

````go
package bus

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"mediation-engine/pkg/core"
)

// Config controls bus capacity and concurrency
type Config struct {
	QueueSize   int `yaml:"queue_size"`   // default: 4096
	WorkerCount int `yaml:"worker_count"` // default: 8
}

// Bus is a bounded event queue with a fixed worker pool
type Bus struct {
	queue    chan core.Event
	pipeline core.Pipeline
	config   Config
	wg       sync.WaitGroup
	logger   *slog.Logger
}

func New(cfg Config, pipeline core.Pipeline, logger *slog.Logger) *Bus {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 4096
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 8
	}
	return &Bus{
		queue:    make(chan core.Event, cfg.QueueSize),
		pipeline: pipeline,
		config:   cfg,
		logger:   logger,
	}
}

// Publish implements core.EventPublisher — returns error if queue is full (backpressure)
func (b *Bus) Publish(ctx context.Context, evt core.Event) error {
	select {
	case b.queue <- evt:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		b.logger.Warn("event bus full, dropping event",
			"event_id", evt.ID,
			"source_id", evt.SourceID,
			"capacity", b.config.QueueSize,
		)
		return fmt.Errorf("%w: capacity %d, event %s", core.ErrBusFull, b.config.QueueSize, evt.ID)
	}
}

// Start launches workers and blocks until ctx is cancelled, then drains remaining events
func (b *Bus) Start(ctx context.Context) {
	for i := 0; i < b.config.WorkerCount; i++ {
		b.wg.Add(1)
		go b.worker(ctx, i)
	}
	<-ctx.Done()
	b.logger.Info("bus shutting down, draining queue", "remaining", len(b.queue))
	b.drain()
	b.wg.Wait()
	b.logger.Info("bus shutdown complete")
}

func (b *Bus) worker(ctx context.Context, id int) {
	defer b.wg.Done()
	for {
		select {
		case evt := <-b.queue:
			b.pipeline.Execute(ctx, evt)
		case <-ctx.Done():
			return
		}
	}
}

// drain processes all remaining events in the queue after context cancellation
func (b *Bus) drain() {
	for {
		select {
		case evt := <-b.queue:
			b.pipeline.Execute(context.Background(), evt)
		default:
			return
		}
	}
}
````

---

### Phase 2: Pipeline & Stages (Days 2–4)

**Goal**: Three-stage pipeline (lifecycle → route → delivery) plus logging middleware.

#### 2.1 Pipeline

````go
package pipeline

import (
	"context"
	"log/slog"
	"time"

	"mediation-engine/pkg/core"
)

// Pipeline executes stages in order for each event
type Pipeline struct {
	stages []core.PipelineStage
	logger *slog.Logger
}

func New(logger *slog.Logger, stages ...core.PipelineStage) *Pipeline {
	return &Pipeline{stages: stages, logger: logger}
}

func (p *Pipeline) Execute(ctx context.Context, evt core.Event) {
	ec := &core.EventContext{Event: evt}

	start := time.Now()
	for _, stage := range p.stages {
		if err := stage.Process(ctx, ec); err != nil {
			p.logger.Error("pipeline stage failed",
				"stage", stage.Name(),
				"event_id", evt.ID,
				"source_id", evt.SourceID,
				"error", err,
				"elapsed_ms", time.Since(start).Milliseconds(),
			)
			return
		}
	}
	p.logger.Debug("pipeline completed",
		"event_id", evt.ID,
		"elapsed_ms", time.Since(start).Milliseconds(),
	)
}
````

#### 2.2 Lifecycle Stage

Handles connect/disconnect events — logs them and stops pipeline propagation (lifecycle events don't get routed to brokers).

````go
package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"mediation-engine/pkg/core"
)

// LifecycleStage handles connect/disconnect events.
// Lifecycle events are logged and do NOT continue to route/delivery stages.
type LifecycleStage struct {
	logger *slog.Logger
}

func NewLifecycleStage(logger *slog.Logger) *LifecycleStage {
	return &LifecycleStage{logger: logger}
}

func (s *LifecycleStage) Name() string { return "lifecycle" }

func (s *LifecycleStage) Process(ctx context.Context, ec *core.EventContext) error {
	if !ec.Event.IsLifecycle() {
		return nil // pass through for data events
	}

	switch ec.Event.Type {
	case core.EventTypeConnect:
		s.logger.Info("client connected",
			"client_id", ec.Event.ClientID,
			"source_id", ec.Event.SourceID,
		)
	case core.EventTypeDisconnect:
		s.logger.Info("client disconnected",
			"client_id", ec.Event.ClientID,
			"source_id", ec.Event.SourceID,
		)
	}

	// Return a sentinel error to stop pipeline for lifecycle events.
	// This is not a failure — lifecycle events simply don't route to brokers.
	return fmt.Errorf("lifecycle event handled (client_id=%s)", ec.Event.ClientID)
}
````

#### 2.3 Route Resolution Stage

````go
package pipeline

import (
	"context"
	"fmt"

	"mediation-engine/internal/routing"
	"mediation-engine/pkg/core"
)

// RouteStage resolves the route for an event based on its SourceID
type RouteStage struct {
	table *routing.Table
}

func NewRouteStage(table *routing.Table) *RouteStage {
	return &RouteStage{table: table}
}

func (s *RouteStage) Name() string { return "route_resolve" }

func (s *RouteStage) Process(ctx context.Context, ec *core.EventContext) error {
	route, ok := s.table.Lookup(ec.Event.SourceID)
	if !ok {
		return fmt.Errorf("%w: source=%s", core.ErrNoRoute, ec.Event.SourceID)
	}
	ec.Route = route
	return nil
}
````

#### 2.4 Log Stage (Structured Packet Logging for Fluent Bit)

````go
package pipeline

import (
	"context"
	"log/slog"

	"mediation-engine/pkg/core"
)

// LogStage emits structured JSON logs for every data packet passing through.
// An external log shipper (e.g., Fluent Bit) extracts these for observability.
// This replaces the removed policy engine — no transformation, just observation.
type LogStage struct {
	logger *slog.Logger
}

func NewLogStage(logger *slog.Logger) *LogStage {
	return &LogStage{logger: logger}
}

func (s *LogStage) Name() string { return "packet_log" }

func (s *LogStage) Process(ctx context.Context, ec *core.EventContext) error {
	target := ""
	direction := "unknown"
	if ec.Route != nil {
		target = ec.Route.Target
		if ec.Route.Direction == core.DirectionUpstream {
			direction = "upstream"
		} else {
			direction = "downstream"
		}
	}

	s.logger.Info("packet",
		"event_id", ec.Event.ID,
		"source_id", ec.Event.SourceID,
		"client_id", ec.Event.ClientID,
		"target", target,
		"direction", direction,
		"payload_size", len(ec.Event.Payload),
		"timestamp", ec.Event.Timestamp,
		"metadata", ec.Event.Metadata,
	)
	return nil
}
````

#### 2.5 Delivery Stage

````go
package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"mediation-engine/pkg/core"
)

// DeliveryStage sends the event to its resolved target (endpoint or entrypoint)
type DeliveryStage struct {
	entrypoints map[string]core.Entrypoint
	endpoints   map[string]core.Endpoint
	logger      *slog.Logger
}

func NewDeliveryStage(
	entrypoints map[string]core.Entrypoint,
	endpoints map[string]core.Endpoint,
	logger *slog.Logger,
) *DeliveryStage {
	return &DeliveryStage{
		entrypoints: entrypoints,
		endpoints:   endpoints,
		logger:      logger,
	}
}

func (s *DeliveryStage) Name() string { return "delivery" }

func (s *DeliveryStage) Process(ctx context.Context, ec *core.EventContext) error {
	if ec.Route == nil {
		return core.ErrNoRoute
	}

	target := ec.Route.Target

	switch ec.Route.Direction {
	case core.DirectionUpstream:
		ep, ok := s.endpoints[target]
		if !ok {
			return fmt.Errorf("%w: endpoint %s", core.ErrTargetNotFound, target)
		}
		return ep.SendUpstream(ctx, ec.Event)

	case core.DirectionDownstream:
		ep, ok := s.entrypoints[target]
		if !ok {
			return fmt.Errorf("%w: entrypoint %s", core.ErrTargetNotFound, target)
		}
		return ep.SendDownstream(ctx, ec.Event)

	default:
		return fmt.Errorf("unknown direction for route source=%s target=%s", ec.Route.Source, target)
	}
}
````

---

### Phase 3: Routing Table (Day 3)

````go
package routing

import (
	"sync"

	"mediation-engine/pkg/core"
)

// Table provides lock-free route lookups optimized for read-heavy workloads
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

// ReplaceAll atomically replaces all routes (for hot-reload)
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

### Phase 4: Entrypoint Plugins (Days 4–6)

Four entrypoints — WebSocket, SSE, HTTP GET, HTTP POST.

#### 4.1 WebSocket Entrypoint

Existing implementation in pkg/plugins/ws/entrypoint.go — refactor to use new interfaces:

````go
package ws

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"mediation-engine/pkg/core"
)

type Entrypoint struct {
	name      string
	port      int
	upgrader  websocket.Upgrader
	publisher core.EventPublisher
	clients   sync.Map // clientID → *websocket.Conn
	server    *http.Server
	logger    *slog.Logger
}

func New(name string, port int, logger *slog.Logger) *Entrypoint {
	return &Entrypoint{
		name:   name,
		port:   port,
		logger: logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (e *Entrypoint) Name() string { return e.name }
func (e *Entrypoint) Type() string { return "websocket" }

func (e *Entrypoint) Start(ctx context.Context, publisher core.EventPublisher) error {
	e.publisher = publisher

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
	if e.server != nil {
		return e.server.Shutdown(ctx)
	}
	return nil
}

func (e *Entrypoint) SendDownstream(ctx context.Context, evt core.Event) error {
	val, ok := e.clients.Load(evt.ClientID)
	if !ok {
		return fmt.Errorf("client %s not connected", evt.ClientID)
	}
	conn := val.(*websocket.Conn)
	return conn.WriteMessage(websocket.TextMessage, evt.Payload)
}

func (e *Entrypoint) handleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := e.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] upgrade error: %v", err)
		return
	}

	clientID := uuid.New().String()
	e.clients.Store(clientID, conn)

	// Publish connect event
	e.publisher.Publish(r.Context(), core.Event{
		ID:        uuid.New().String(),
		SourceID:  e.name,
		ClientID:  clientID,
		Type:      core.EventTypeConnect,
		Timestamp: time.Now(),
	})

	defer func() {
		conn.Close()
		e.clients.Delete(clientID)
		e.publisher.Publish(context.Background(), core.Event{
			ID:        uuid.New().String(),
			SourceID:  e.name,
			ClientID:  clientID,
			Type:      core.EventTypeDisconnect,
			Timestamp: time.Now(),
		})
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		e.publisher.Publish(r.Context(), core.Event{
			ID:        uuid.New().String(),
			SourceID:  e.name,
			ClientID:  clientID,
			Payload:   msg,
			Type:      core.EventTypeData,
			Timestamp: time.Now(),
			Metadata:  map[string]string{"protocol": "websocket"},
		})
	}
}
````

#### 4.2 SSE Entrypoint

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
	"mediation-engine/pkg/core"
)

type sseClient struct {
	flusher http.Flusher
	writer  http.ResponseWriter
	done    chan struct{}
}

type Entrypoint struct {
	name      string
	port      int
	publisher core.EventPublisher
	clients   sync.Map // clientID → *sseClient
	server    *http.Server
	logger    *slog.Logger
}

func New(name string, port int, logger *slog.Logger) *Entrypoint {
	return &Entrypoint{name: name, port: port, logger: logger}
}

func (e *Entrypoint) Name() string { return e.name }
func (e *Entrypoint) Type() string { return "sse" }

func (e *Entrypoint) Start(ctx context.Context, publisher core.EventPublisher) error {
	e.publisher = publisher
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
	if e.server != nil {
		return e.server.Shutdown(ctx)
	}
	return nil
}

func (e *Entrypoint) SendDownstream(ctx context.Context, evt core.Event) error {
	val, ok := e.clients.Load(evt.ClientID)
	if !ok {
		return fmt.Errorf("sse client %s not connected", evt.ClientID)
	}
	client := val.(*sseClient)
	_, err := fmt.Fprintf(client.writer, "id: %s\ndata: %s\n\n", evt.ID, string(evt.Payload))
	if err != nil {
		return err
	}
	client.flusher.Flush()
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
	client := &sseClient{flusher: flusher, writer: w, done: make(chan struct{})}
	e.clients.Store(clientID, client)

	e.publisher.Publish(r.Context(), core.Event{
		ID: uuid.New().String(), SourceID: e.name, ClientID: clientID,
		Type: core.EventTypeConnect, Timestamp: time.Now(),
	})

	defer func() {
		close(client.done)
		e.clients.Delete(clientID)
		e.publisher.Publish(context.Background(), core.Event{
			ID: uuid.New().String(), SourceID: e.name, ClientID: clientID,
			Type: core.EventTypeDisconnect, Timestamp: time.Now(),
		})
	}()

	// SSE is server-push only; block until client disconnects
	<-r.Context().Done()
}
````

#### 4.3 HTTP POST Entrypoint (Webhook Receiver)

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
	"mediation-engine/pkg/core"
)

// Entrypoint receives HTTP POST requests and publishes each as an event.
// Useful for webhook ingestion — each POST body becomes an event payload.
type Entrypoint struct {
	name      string
	port      int
	publisher core.EventPublisher
	server    *http.Server
	logger    *slog.Logger
	maxBody   int64 // max request body size in bytes
}

func New(name string, port int, logger *slog.Logger) *Entrypoint {
	return &Entrypoint{
		name:    name,
		port:    port,
		logger:  logger,
		maxBody: 1 << 20, // 1MB default
	}
}

func (e *Entrypoint) Name() string { return e.name }
func (e *Entrypoint) Type() string { return "http_post" }

func (e *Entrypoint) Start(ctx context.Context, publisher core.EventPublisher) error {
	e.publisher = publisher
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

// SendDownstream is a no-op for HTTP POST — responses are synchronous in handlePost
func (e *Entrypoint) SendDownstream(ctx context.Context, evt core.Event) error {
	return fmt.Errorf("http_post entrypoint does not support downstream sends")
}

func (e *Entrypoint) handlePost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, e.maxBody))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	metadata := map[string]string{
		"protocol":     "http_post",
		"content_type": r.Header.Get("Content-Type"),
		"remote_addr":  r.RemoteAddr,
	}

	err = e.publisher.Publish(r.Context(), core.Event{
		ID:        uuid.New().String(),
		SourceID:  e.name,
		ClientID:  r.RemoteAddr, // no persistent session, use remote addr
		Payload:   body,
		Type:      core.EventTypeData,
		Timestamp: time.Now(),
		Metadata:  metadata,
	})

	if err != nil {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status":"accepted"}`))
}
````

#### 4.4 HTTP GET Entrypoint (Polling)

````go
package httpget

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"mediation-engine/pkg/core"
)

// Entrypoint serves HTTP GET requests as a polling-based data source.
// Downstream events are buffered per-client and returned on next GET.
type Entrypoint struct {
	name      string
	port      int
	publisher core.EventPublisher
	server    *http.Server
	logger    *slog.Logger
	buffers   sync.Map // clientID → chan []byte
	bufSize   int
}

func New(name string, port int, logger *slog.Logger) *Entrypoint {
	return &Entrypoint{
		name:    name,
		port:    port,
		logger:  logger,
		bufSize: 100,
	}
}

func (e *Entrypoint) Name() string { return e.name }
func (e *Entrypoint) Type() string { return "http_get" }

func (e *Entrypoint) Start(ctx context.Context, publisher core.EventPublisher) error {
	e.publisher = publisher
	mux := http.NewServeMux()
	mux.HandleFunc("/", e.handleGet)

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
	if e.server != nil {
		return e.server.Shutdown(ctx)
	}
	return nil
}

// SendDownstream buffers the event payload for the target client's next GET request
func (e *Entrypoint) SendDownstream(ctx context.Context, evt core.Event) error {
	val, ok := e.buffers.Load(evt.ClientID)
	if !ok {
		// Create buffer on first downstream send for this client
		ch := make(chan []byte, e.bufSize)
		e.buffers.Store(evt.ClientID, ch)
		val = ch
	}
	ch := val.(chan []byte)

	select {
	case ch <- evt.Payload:
		return nil
	default:
		return fmt.Errorf("http_get buffer full for client %s", evt.ClientID)
	}
}

func (e *Entrypoint) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := r.Header.Get("X-Client-ID")
	if clientID == "" {
		clientID = uuid.New().String()
	}

	// Check for buffered downstream data
	val, ok := e.buffers.Load(clientID)
	if ok {
		ch := val.(chan []byte)
		select {
		case data := <-ch:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(data)
			return
		default:
			// No data buffered, return 204
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
````

---

### Phase 5: Endpoint Plugins (Days 6–10)

Five endpoints — Kafka, RabbitMQ, MQTT5, Solace, JMS.

#### 5.1 Kafka Endpoint

Refactor from existing pkg/plugins/kafka/endpoint.go:

````go
package kafka

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/segmentio/kafka-go"
	"mediation-engine/pkg/core"
)

type Endpoint struct {
	name      string
	brokers   []string
	topicIn   string // consume from (broker → engine)
	topicOut  string // produce to (engine → broker)
	writer    *kafka.Writer
	publisher core.EventPublisher
	logger    *slog.Logger
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

func (e *Endpoint) Start(ctx context.Context, publisher core.EventPublisher) error {
	e.publisher = publisher

	// Producer
	e.writer = &kafka.Writer{
		Addr:     kafka.TCP(e.brokers...),
		Topic:    e.topicOut,
		Balancer: &kafka.LeastBytes{},
	}

	// Consumer (broker → engine, for downstream events)
	if e.topicIn != "" {
		reader := kafka.NewReader(kafka.ReaderConfig{
			Brokers: e.brokers,
			Topic:   e.topicIn,
			GroupID: fmt.Sprintf("mediation-%s", e.name),
		})
		go e.consume(ctx, reader)
	}

	e.logger.Info("kafka endpoint started", "name", e.name, "brokers", e.brokers)
	<-ctx.Done()
	return nil
}

func (e *Endpoint) Stop(ctx context.Context) error {
	if e.writer != nil {
		return e.writer.Close()
	}
	return nil
}

func (e *Endpoint) SendUpstream(ctx context.Context, evt core.Event) error {
	return e.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(evt.ID),
		Value: evt.Payload,
	})
}

func (e *Endpoint) consume(ctx context.Context, reader *kafka.Reader) {
	defer reader.Close()
	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			e.logger.Error("kafka consume error", "error", err)
			continue
		}
		e.publisher.Publish(ctx, core.Event{
			ID:       string(msg.Key),
			SourceID: e.name,
			Payload:  msg.Value,
			Type:     core.EventTypeData,
			Metadata: map[string]string{
				"protocol": "kafka",
				"topic":    msg.Topic,
			},
		})
	}
}
````

#### 5.2 RabbitMQ Endpoint

````go
package rabbitmq

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"mediation-engine/pkg/core"
)

type Endpoint struct {
	name      string
	url       string // amqp://user:pass@host:5672/vhost
	queueIn   string // consume from
	queueOut  string // publish to
	conn      *amqp.Connection
	channel   *amqp.Channel
	publisher core.EventPublisher
	logger    *slog.Logger
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

func (e *Endpoint) Start(ctx context.Context, publisher core.EventPublisher) error {
	e.publisher = publisher

	var err error
	e.conn, err = amqp.Dial(e.url)
	if err != nil {
		return fmt.Errorf("rabbitmq dial failed: %w", err)
	}

	e.channel, err = e.conn.Channel()
	if err != nil {
		return fmt.Errorf("rabbitmq channel failed: %w", err)
	}

	// Declare queues
	for _, q := range []string{e.queueIn, e.queueOut} {
		if q != "" {
			_, err = e.channel.QueueDeclare(q, true, false, false, false, nil)
			if err != nil {
				return fmt.Errorf("rabbitmq queue declare failed for %s: %w", q, err)
			}
		}
	}

	// Consumer
	if e.queueIn != "" {
		go e.consume(ctx)
	}

	e.logger.Info("rabbitmq endpoint started", "name", e.name, "url", e.url)
	<-ctx.Done()
	return nil
}

func (e *Endpoint) Stop(ctx context.Context) error {
	if e.channel != nil {
		e.channel.Close()
	}
	if e.conn != nil {
		return e.conn.Close()
	}
	return nil
}

func (e *Endpoint) SendUpstream(ctx context.Context, evt core.Event) error {
	return e.channel.PublishWithContext(ctx,
		"",         // exchange
		e.queueOut, // routing key
		false, false,
		amqp.Publishing{
			ContentType: "application/octet-stream",
			Body:        evt.Payload,
			MessageId:   evt.ID,
			Timestamp:   time.Now(),
		},
	)
}

func (e *Endpoint) consume(ctx context.Context) {
	msgs, err := e.channel.Consume(e.queueIn, "", true, false, false, false, nil)
	if err != nil {
		e.logger.Error("rabbitmq consume failed", "error", err)
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			e.publisher.Publish(ctx, core.Event{
				ID:       uuid.New().String(),
				SourceID: e.name,
				Payload:  msg.Body,
				Type:     core.EventTypeData,
				Metadata: map[string]string{
					"protocol": "rabbitmq",
					"queue":    e.queueIn,
				},
				Timestamp: time.Now(),
			})
		}
	}
}
````

#### 5.3 MQTT 5.0 Endpoint

````go
package mqtt5

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/google/uuid"
	"mediation-engine/pkg/core"
	"net/url"
)

// Endpoint implements core.Endpoint for MQTT 5.0 using eclipse/paho.golang
type Endpoint struct {
	name      string
	brokerURL string
	topicIn   string
	topicOut  string
	cm        *autopaho.ConnectionManager
	publisher core.EventPublisher
	logger    *slog.Logger
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

func (e *Endpoint) Start(ctx context.Context, publisher core.EventPublisher) error {
	e.publisher = publisher

	serverURL, err := url.Parse(e.brokerURL)
	if err != nil {
		return fmt.Errorf("mqtt5 invalid broker URL: %w", err)
	}

	cfg := autopaho.ClientConfig{
		ServerUrls: []*url.URL{serverURL},
		KeepAlive:  30,
		OnConnectionUp: func(cm *autopaho.ConnectionManager, connAck *paho.Connack) {
			if e.topicIn != "" {
				cm.Subscribe(ctx, &paho.Subscribe{
					Subscriptions: []paho.SubscribeOptions{
						{Topic: e.topicIn, QoS: 1},
					},
				})
			}
			e.logger.Info("mqtt5 connected", "name", e.name)
		},
		ClientConfig: paho.ClientConfig{
			ClientID: fmt.Sprintf("mediation-%s", e.name),
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				func(pr paho.PublishReceived) (bool, error) {
					e.publisher.Publish(ctx, core.Event{
						ID:       uuid.New().String(),
						SourceID: e.name,
						Payload:  pr.Packet.Payload,
						Type:     core.EventTypeData,
						Metadata: map[string]string{
							"protocol": "mqtt5",
							"topic":    pr.Packet.Topic,
						},
						Timestamp: time.Now(),
					})
					return true, nil
				},
			},
		},
	}

	e.cm, err = autopaho.NewConnection(ctx, cfg)
	if err != nil {
		return fmt.Errorf("mqtt5 connection failed: %w", err)
	}

	e.logger.Info("mqtt5 endpoint started", "name", e.name, "broker", e.brokerURL)
	<-ctx.Done()
	return nil
}

func (e *Endpoint) Stop(ctx context.Context) error {
	if e.cm != nil {
		return e.cm.Disconnect(ctx)
	}
	return nil
}

func (e *Endpoint) SendUpstream(ctx context.Context, evt core.Event) error {
	_, err := e.cm.Publish(ctx, &paho.Publish{
		Topic:   e.topicOut,
		QoS:     1,
		Payload: evt.Payload,
	})
	return err
}
````

#### 5.4 Solace Endpoint

Solace supports AMQP 1.0, SMF, and MQTT. Using the Solace Go API:

````go
package solace

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"mediation-engine/pkg/core"
	"solace.dev/go/messaging"
	"solace.dev/go/messaging/pkg/solace/config"
	"solace.dev/go/messaging/pkg/solace/resource"
)

type Endpoint struct {
	name       string
	host       string
	vpn        string
	username   string
	password   string
	topicIn    string
	topicOut   string
	service    messaging.MessagingService
	publisher  core.EventPublisher
	logger     *slog.Logger
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

func (e *Endpoint) Start(ctx context.Context, publisher core.EventPublisher) error {
	e.publisher = publisher

	var err error
	e.service, err = messaging.NewMessagingServiceBuilder().
		FromConfigurationProvider(config.ServicePropertyMap{
			config.TransportLayerPropertyHost:                e.host,
			config.ServicePropertyVPNName:                    e.vpn,
			config.AuthenticationPropertySchemeBasicUserName: e.username,
			config.AuthenticationPropertySchemeBasicPassword: e.password,
		}).Build()
	if err != nil {
		return fmt.Errorf("solace build failed: %w", err)
	}

	if err = e.service.Connect(); err != nil {
		return fmt.Errorf("solace connect failed: %w", err)
	}

	// Subscribe for inbound
	if e.topicIn != "" {
		receiver, err := e.service.CreateDirectMessageReceiverBuilder().
			WithSubscriptions(resource.TopicSubscriptionOf(e.topicIn)).Build()
		if err != nil {
			return fmt.Errorf("solace receiver build failed: %w", err)
		}
		if err = receiver.Start(); err != nil {
			return fmt.Errorf("solace receiver start failed: %w", err)
		}
		go func() {
			for {
				msg, err := receiver.ReceiveMessage(ctx)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					e.logger.Error("solace receive error", "error", err)
					continue
				}
				payload, _ := msg.GetPayloadAsBytes()
				e.publisher.Publish(ctx, core.Event{
					ID:       uuid.New().String(),
					SourceID: e.name,
					Payload:  payload,
					Type:     core.EventTypeData,
					Metadata: map[string]string{
						"protocol":    "solace",
						"destination": msg.GetDestinationName(),
					},
					Timestamp: time.Now(),
				})
			}
		}()
	}

	e.logger.Info("solace endpoint started", "name", e.name, "host", e.host)
	<-ctx.Done()
	return nil
}

func (e *Endpoint) Stop(ctx context.Context) error {
	if e.service != nil {
		e.service.Disconnect()
	}
	return nil
}

func (e *Endpoint) SendUpstream(ctx context.Context, evt core.Event) error {
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

#### 5.5 JMS Endpoint (via AMQP 1.0)

JMS doesn't have a native Go library. The standard approach is to use AMQP 1.0 (which JMS 2.0+ brokers like ActiveMQ Artemis, IBM MQ support):

````go
package jms

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Azure/go-amqp"
	"github.com/google/uuid"
	"mediation-engine/pkg/core"
)

// Endpoint connects to JMS-compatible brokers via AMQP 1.0.
// Supports ActiveMQ Artemis, IBM MQ, and any JMS broker with AMQP 1.0 support.
type Endpoint struct {
	name      string
	url       string // amqp://host:5672
	queueIn   string
	queueOut  string
	conn      *amqp.Conn
	session   *amqp.Session
	sender    *amqp.Sender
	publisher core.EventPublisher
	logger    *slog.Logger
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

func (e *Endpoint) Start(ctx context.Context, publisher core.EventPublisher) error {
	e.publisher = publisher

	var err error
	e.conn, err = amqp.Dial(ctx, e.url, nil)
	if err != nil {
		return fmt.Errorf("jms/amqp dial failed: %w", err)
	}

	e.session, err = e.conn.NewSession(ctx, nil)
	if err != nil {
		return fmt.Errorf("jms/amqp session failed: %w", err)
	}

	// Sender for outbound
	if e.queueOut != "" {
		e.sender, err = e.session.NewSender(ctx, e.queueOut, nil)
		if err != nil {
			return fmt.Errorf("jms/amqp sender failed: %w", err)
		}
	}

	// Receiver for inbound
	if e.queueIn != "" {
		receiver, err := e.session.NewReceiver(ctx, e.queueIn, nil)
		if err != nil {
			return fmt.Errorf("jms/amqp receiver failed: %w", err)
		}
		go e.consume(ctx, receiver)
	}

	e.logger.Info("jms endpoint started", "name", e.name, "url", e.url)
	<-ctx.Done()
	return nil
}

func (e *Endpoint) Stop(ctx context.Context) error {
	if e.sender != nil {
		e.sender.Close(ctx)
	}
	if e.session != nil {
		e.session.Close(ctx)
	}
	if e.conn != nil {
		return e.conn.Close()
	}
	return nil
}

func (e *Endpoint) SendUpstream(ctx context.Context, evt core.Event) error {
	return e.sender.Send(ctx, &amqp.Message{
		Data: [][]byte{evt.Payload},
		Properties: &amqp.MessageProperties{
			MessageID: evt.ID,
		},
	}, nil)
}

func (e *Endpoint) consume(ctx context.Context, receiver *amqp.Receiver) {
	for {
		msg, err := receiver.Receive(ctx, nil)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			e.logger.Error("jms/amqp receive error", "error", err)
			continue
		}
		receiver.AcceptMessage(ctx, msg)

		var payload []byte
		if len(msg.Data) > 0 {
			payload = msg.Data[0]
		}

		e.publisher.Publish(ctx, core.Event{
			ID:       uuid.New().String(),
			SourceID: e.name,
			Payload:  payload,
			Type:     core.EventTypeData,
			Metadata: map[string]string{
				"protocol": "jms_amqp10",
			},
			Timestamp: time.Now(),
		})
	}
}
````

---

### Phase 6: Plugin Registry & Lifecycle (Days 9–10)

````go
package plugins

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

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

// StartAll starts endpoints first (brokers must be ready), then entrypoints
func (r *Registry) StartAll(ctx context.Context, publisher core.EventPublisher) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(r.endpoints)+len(r.entrypoints))

	for name, ep := range r.endpoints {
		wg.Add(1)
		go func(n string, e core.Endpoint) {
			defer wg.Done()
			r.logger.Info("starting endpoint", "name", n, "type", e.Type())
			if err := e.Start(ctx, publisher); err != nil && ctx.Err() == nil {
				errCh <- fmt.Errorf("endpoint %s failed: %w", n, err)
			}
		}(name, ep)
	}

	time.Sleep(500 * time.Millisecond) // let brokers connect

	for name, ep := range r.entrypoints {
		wg.Add(1)
		go func(n string, e core.Entrypoint) {
			defer wg.Done()
			r.logger.Info("starting entrypoint", "name", n, "type", e.Type())
			if err := e.Start(ctx, publisher); err != nil && ctx.Err() == nil {
				errCh <- fmt.Errorf("entrypoint %s failed: %w", n, err)
			}
		}(name, ep)
	}

	go func() { wg.Wait(); close(errCh) }()
	return nil
}

// StopAll stops entrypoints first (stop accepting), then endpoints (drain brokers)
func (r *Registry) StopAll(ctx context.Context) {
	for name, ep := range r.entrypoints {
		r.logger.Info("stopping entrypoint", "name", name)
		_ = ep.Stop(ctx)
	}
	for name, ep := range r.endpoints {
		r.logger.Info("stopping endpoint", "name", name)
		_ = ep.Stop(ctx)
	}
}
````

---

### Phase 7: Configuration (Days 10–11)

#### 7.1 Config Format

````yaml
bus:
  queue_size: 4096
  worker_count: 8

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
  - source: kafka-main
    target: ws-inbound
    direction: downstream
  - source: webhook-receiver
    target: rabbitmq-orders
    direction: upstream
  - source: mqtt5-iot
    target: solace-events
    direction: upstream
````

#### 7.2 Config Loader

````go
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	"mediation-engine/internal/bus"
)

type Config struct {
	Bus         bus.Config        `yaml:"bus"`
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
	Source    string `yaml:"source"`
	Target   string `yaml:"target"`
	Direction string `yaml:"direction"` // "upstream" or "downstream"
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
````

#### 7.3 Config Watcher (Hot-Reload for Routes)

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
					dir := core.DirectionUpstream
					if r.Direction == "downstream" {
						dir = core.DirectionDownstream
					}
					routes = append(routes, &core.Route{
						Source: r.Source, Target: r.Target, Direction: dir,
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

### Phase 8: Main Wiring (Day 11)

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

	"mediation-engine/internal/bus"
	"mediation-engine/internal/pipeline"
	"mediation-engine/internal/routing"
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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// 1. Load config
	configPath := "config.yaml"
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		configPath = p
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("config load failed", "error", err)
		os.Exit(1)
	}

	// 2. Context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 3. Route table
	routeTable := routing.NewTable()
	for _, r := range cfg.Routes {
		dir := core.DirectionUpstream
		if r.Direction == "downstream" {
			dir = core.DirectionDownstream
		}
		routeTable.Add(&core.Route{Source: r.Source, Target: r.Target, Direction: dir})
	}

	// 4. Plugin registry
	pluginReg := plugins.NewRegistry(logger)
	registerPlugins(cfg, pluginReg, logger)

	// 5. Pipeline: lifecycle → route → log → delivery
	// Packet logging is the structured JSON logger that Fluent Bit extracts from
	packetLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	p := pipeline.New(logger,
		pipeline.NewLifecycleStage(logger),
		pipeline.NewRouteStage(routeTable),
		pipeline.NewLogStage(packetLogger),
		pipeline.NewDeliveryStage(pluginReg.Entrypoints(), pluginReg.Endpoints(), logger),
	)

	// 6. Event bus
	eventBus := bus.New(cfg.Bus, p, logger)

	// 7. Start plugins (they publish events to the bus)
	pluginReg.StartAll(ctx, eventBus)

	// 8. Config hot-reload watcher
	watcher := config.NewWatcher(configPath, routeTable, logger)
	go watcher.Watch(ctx)

	// 9. Health endpoint
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		})
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ready"}`))
		})
		srv := &http.Server{Addr: ":9091", Handler: mux}
		go func() {
			<-ctx.Done()
			srv.Shutdown(context.Background())
		}()
		logger.Info("health server starting", "addr", ":9091")
		srv.ListenAndServe()
	}()

	// 10. Start bus (blocks until ctx cancelled, then drains)
	logger.Info("mediation engine starting",
		"entrypoints", len(cfg.Entrypoints),
		"endpoints", len(cfg.Endpoints),
		"routes", len(cfg.Routes),
	)
	eventBus.Start(ctx)

	// 11. Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	pluginReg.StopAll(shutdownCtx)
	logger.Info("mediation engine stopped")
}

func registerPlugins(cfg *config.Config, reg *plugins.Registry, logger *slog.Logger) {
	for _, e := range cfg.Entrypoints {
		switch e.Type {
		case "websocket":
			reg.RegisterEntrypoint(ws.New(e.Name, e.Port, logger))
		case "sse":
			reg.RegisterEntrypoint(sse.New(e.Name, e.Port, logger))
		case "http_post":
			reg.RegisterEntrypoint(httppost.New(e.Name, e.Port, logger))
		case "http_get":
			reg.RegisterEntrypoint(httpget.New(e.Name, e.Port, logger))
		default:
			logger.Warn("unknown entrypoint type", "type", e.Type, "name", e.Name)
		}
	}

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

### Phase 9: Container Co-location (Day 12)

Since the mediation engine now lives in the **same container** as the policy engine and Envoy router, the Dockerfile changes:

````dockerfile
# This is NOT a standalone image — it produces the binary that gets
# COPY'd into the gateway-runtime container alongside policy-engine and envoy.

FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o mediation-engine cmd/server/main.go
````

The **gateway-runtime** Dockerfile (maintained separately) would include:

```dockerfile
# In the gateway-runtime multi-stage Dockerfile:
COPY --from=mediation-builder /app/mediation-engine /usr/local/bin/mediation-engine
# Alongside:
COPY --from=policy-engine-builder /app/policy-engine /usr/local/bin/policy-engine
COPY envoy.yaml /etc/envoy/envoy.yaml
```

A supervisor process or shell script starts all three:

```bash
#!/bin/sh
# Start all processes in the gateway-runtime container
envoy -c /etc/envoy/envoy.yaml &
policy-engine --extproc-port 9001 --xds-port 9002 &
mediation-engine &
wait
```

---

### Phase 10: Testing (Days 12–14)

| Layer | What to Test | Approach |
|---|---|---|
| **Pipeline** | Stage ordering, lifecycle stop, error propagation | Unit tests with mock stages |
| **Event bus** | Backpressure, draining, worker count | Unit + race tests |
| **Route table** | Concurrent read/write, ReplaceAll | Race detector (`-race`) |
| **Log stage** | Structured output format | Capture slog output, parse JSON |
| **Entrypoints** | WS upgrade, SSE stream, HTTP POST accept, HTTP GET polling | Integration with httptest |
| **Endpoints** | Kafka produce/consume, RabbitMQ, MQTT5 | Docker Compose + testcontainers |
| **Config watcher** | File modification detection, route reload | Temp file + timer |
| **Full flow** | WS → Bus → Kafka & Kafka → Bus → SSE | Docker Compose integration |

#### Example: Pipeline Unit Test

````go
package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"mediation-engine/pkg/core"
)

type mockStage struct {
	name   string
	called bool
	err    error
}

func (m *mockStage) Name() string { return m.name }
func (m *mockStage) Process(ctx context.Context, ec *core.EventContext) error {
	m.called = true
	return m.err
}

func TestPipeline_AllStagesExecute(t *testing.T) {
	s1 := &mockStage{name: "s1"}
	s2 := &mockStage{name: "s2"}
	s3 := &mockStage{name: "s3"}

	p := New(slog.Default(), s1, s2, s3)
	p.Execute(context.Background(), core.Event{ID: "test-1"})

	for _, s := range []*mockStage{s1, s2, s3} {
		if !s.called {
			t.Errorf("stage %s should have been called", s.name)
		}
	}
}

func TestPipeline_StopsOnError(t *testing.T) {
	failing := &mockStage{name: "fail", err: errors.New("boom")}
	after := &mockStage{name: "after"}

	p := New(slog.Default(), failing, after)
	p.Execute(context.Background(), core.Event{ID: "test-2"})

	if !failing.called {
		t.Error("failing stage should have been called")
	}
	if after.called {
		t.Error("stage after failure should NOT have been called")
	}
}
````

---

## 5. Migration Strategy

| Step | Action | Risk |
|---|---|---|
| 1 | Create new `pkg/core/` with simplified types (no policy/session/retention) | None — new files |
| 2 | Build `internal/bus/`, `internal/pipeline/`, `internal/routing/`, `internal/logging/` | None — new packages |
| 3 | Implement 4 entrypoints (WS, SSE, HTTP GET, HTTP POST) | Low — WS/SSE refactored from existing |
| 4 | Implement 5 endpoints (Kafka, RabbitMQ, MQTT5, Solace, JMS) | Medium — new broker integrations |
| 5 | Add `Stop()` to all plugins | Low — new method |
| 6 | Wire new main.go | Medium — full switchover |
| 7 | Delete old hub.go, `internal/policy/`, `pkg/session/`, `pkg/retention/` | After validation |
| 8 | Integrate binary into gateway-runtime container | Low — Dockerfile change |

---

## 6. Go Module Dependencies

```
# Existing (keep)
github.com/gorilla/websocket        # WS entrypoint
github.com/segmentio/kafka-go        # Kafka endpoint
gopkg.in/yaml.v3                     # Config loading
github.com/google/uuid               # Event IDs

# New
github.com/rabbitmq/amqp091-go       # RabbitMQ endpoint
github.com/eclipse/paho.golang       # MQTT 5.0 endpoint
solace.dev/go/messaging              # Solace endpoint
github.com/Azure/go-amqp             # JMS via AMQP 1.0

# Removed
github.com/eclipse/paho.mqtt.golang  # Old MQTT 3.1.1 (replaced by mqtt5)
```

---

## 7. What's Deferred (Future Phases)

| Feature | When | Why Deferred |
|---|---|---|
| **WebSub protocol support** | Future | Requires persistence layer (subscriptions, verification) |
| **Persistence layer** (session/retention) | With WebSub | Only needed for WebSub subscription state |
| **Packet-level policies** | Removed permanently | Not needed; logging covers observability |
| **Prometheus metrics** | Future | Structured logging + Fluent Bit covers P1 observability |
| **Per-client subscriptions** on endpoints | With WebSub | `SubscribeForClient()` only needed for WebSub fan-out |

---

## 8. Summary of Changes vs Original Plan

| # | Original Plan | Revised Plan |
|---|---|---|
| 1 | 5 pipeline stages (lifecycle, route, policy, retention, delivery) | **3+1 stages** (lifecycle, route, log, delivery) |
| 2 | Policy engine with per-event timeout | **Removed** — no packet-level policies |
| 3 | Session/retention stores (memory + Redis) | **Removed** — no persistence layer |
| 4 | Standalone container | **Co-located** in gateway-runtime container |
| 5 | 2 entrypoints (WS, SSE) | **4 entrypoints** (WS, SSE, HTTP GET, HTTP POST) |
| 6 | 3 endpoints (Kafka, MQTT, Mock) | **5 endpoints** (Kafka, RabbitMQ, MQTT5, Solace, JMS) |
| 7 | Prometheus metrics | **Structured JSON logging** for Fluent Bit extraction |
| 8 | `IngressHub` interface | **`EventPublisher`** interface (simpler) |
| 9 | `core.Policy`, `RoutePolicyEngine` | **Deleted** |
| 10 | `core.SessionStore`, `RetentionStore` | **Deleted** (deferred to WebSub phase) |

Similar code found with 3 license types