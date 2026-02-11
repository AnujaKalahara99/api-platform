User: @workspace Can you thouroughly look at the the mediation engine architecture and create a detailed plan on how to implement this again, efficiently and optimally.

Workspace: Collecting workspace informationFiltering to most relevant information

# Mediation Engine — Reimplementation Plan

After a thorough analysis of the existing Mediation Engine codebase in mediation-engine, here is a comprehensive plan to reimplement it efficiently and optimally.

---

## 1. Current Architecture Analysis

### What Exists Today

The Mediation Engine bridges web protocols (WebSocket, SSE) and event brokers (Kafka, MQTT) through a protocol-agnostic event abstraction. The core components are:

| Component | File | Role |
|---|---|---|
| **Hub** (Central Router) | internal/engine/hub.go | Event routing, lifecycle management, retention |
| **Policy Engine** | internal/policy/policyEngine.go | Per-route policy evaluation (filter, transform, rate-limit) |
| **Built-in Policies** | internal/policy/policies.go | `filter`, `transform`, `rate-limit` implementations |
| **Core Abstractions** | pkg/core/interfaces.go | `Entrypoint`, `Endpoint`, `Policy`, `SessionStore`, `RetentionStore` interfaces |
| **Core Types** | pkg/core/types.go | `Event`, `Route`, `Session`, `PolicyAction` |
| **Plugins** | pkg/plugins/ | ws, sse (entrypoints), kafka, mqtt, mock (endpoints) |
| **Session Stores** | pkg/session/ | Memory and Redis implementations |
| **Retention Stores** | pkg/retention/ | Memory and Redis implementations |
| **Config Loader** | pkg/config/loader.go | YAML configuration parsing |
| **Main Entry** | cmd/server/main.go | Wiring and startup |

### Current Data Flow

```
Client → Entrypoint (WS/SSE)
           │
           ▼
      Hub.Publish(event)
           │
           ▼
      Hub.ingressChan (buffered 1000)
           │
           ▼
      go Hub.processEvent()
           │
           ├── IsLifecycle? → handleLifecycleEvent() → session/cache update
           │
           └── routeMessage()
                  │
                  ├── Route lookup by evt.SourceID
                  ├── Policy evaluation (EvaluateWithRoute)
                  │      └── Sequential: filter → transform → rate-limit
                  ├── Retention store (if enabled)
                  └── Deliver to target (Endpoint.SendUpstream / Entrypoint.SendDownstream)
```

### Identified Weaknesses

1. **Unbounded goroutine spawning**: Every event in `Hub.Start()` spawns `go h.processEvent()` — no concurrency control.
2. **Single ingress channel**: One `chan core.Event` with hardcoded buffer of 1000 — no backpressure, no priority.
3. **Lock contention on routes**: `sync.RWMutex` on route lookups in the hot path.
4. **No graceful shutdown**: `ctx.Done()` exits the loop but doesn't drain in-flight events.
5. **No observability**: No metrics, no tracing, no structured logging.
6. **Session store in Hub**: The Hub directly manages client cache (`map[string]bool`) — mixed concerns.
7. **Policy engine has no per-event timeout**: A slow policy blocks the event pipeline.
8. **Retention and session are optional but checked every event**: Nil-check overhead on every message.
9. **Plugin lifecycle**: Plugins start with `go plugin.Start()` but there's no health check or restart.
10. **Config is static**: No hot-reload capability.

---

## 2. Reimplementation Goals

| Goal | Metric |
|---|---|
| **Bounded concurrency** | Worker pool with configurable size |
| **Backpressure** | Slow consumers don't crash the system |
| **Graceful shutdown** | Drain in-flight events before exit |
| **Observability** | Prometheus metrics + structured logging |
| **Clean separation** | Hub only routes; session/retention are middleware |
| **Policy timeout** | Per-policy context deadline |
| **Type safety** | Stronger typing on route targets |
| **Testability** | Every component unit-testable with interfaces |
| **Hot-reload** | Config watcher for route/policy changes |

---

## 3. Proposed Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                        Mediation Engine                          │
│                                                                  │
│  ┌─────────────┐    ┌──────────────────┐    ┌────────────────┐  │
│  │ Entrypoints │───▶│    Event Bus     │───▶│   Endpoints    │  │
│  │ (WS, SSE,   │    │  (bounded queue  │    │ (Kafka, MQTT,  │  │
│  │  Webhook)   │◀───│   + worker pool) │◀───│  Mock)         │  │
│  └─────────────┘    └────────┬─────────┘    └────────────────┘  │
│                              │                                   │
│                    ┌─────────▼──────────┐                       │
│                    │   Pipeline Stages  │                       │
│                    │  ┌──────────────┐  │                       │
│                    │  │ Route Resolve│  │                       │
│                    │  ├──────────────┤  │                       │
│                    │  │ Session Mgmt │  │                       │
│                    │  ├──────────────┤  │                       │
│                    │  │ Policy Chain │  │                       │
│                    │  ├──────────────┤  │                       │
│                    │  │  Retention   │  │                       │
│                    │  ├──────────────┤  │                       │
│                    │  │  Delivery    │  │                       │
│                    │  └──────────────┘  │                       │
│                    └───────────────────┘                        │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                    Observability                          │   │
│  │  Prometheus Metrics │ Structured Logging │ Health Checks  │   │
│  └──────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────┘
```

---

## 4. Directory Structure

```
mediation-engine/
├── cmd/
│   └── server/
│       └── main.go                    # Minimal wiring, DI
├── internal/
│   ├── bus/
│   │   ├── bus.go                     # Event bus with worker pool
│   │   ├── bus_test.go
│   │   ├── worker.go                  # Worker implementation
│   │   └── worker_test.go
│   ├── pipeline/
│   │   ├── pipeline.go               # Ordered stage execution
│   │   ├── pipeline_test.go
│   │   ├── stage_route.go            # Route resolution stage
│   │   ├── stage_session.go          # Session management stage
│   │   ├── stage_policy.go           # Policy evaluation stage
│   │   ├── stage_retention.go        # Retention stage
│   │   ├── stage_delivery.go         # Final delivery stage
│   │   └── stage_lifecycle.go        # Connect/disconnect handling
│   ├── policy/
│   │   ├── engine.go                 # Policy executor with timeouts
│   │   ├── engine_test.go
│   │   ├── filter.go                 # Filter policy
│   │   ├── transform.go             # Transform policy
│   │   ├── ratelimit.go             # Rate limit policy (real impl)
│   │   └── registry.go              # Policy type registry
│   ├── routing/
│   │   ├── table.go                  # Lock-free route table (sync.Map)
│   │   ├── table_test.go
│   │   └── resolver.go              # Route matching logic
│   ├── metrics/
│   │   ├── metrics.go               # Prometheus collectors
│   │   └── middleware.go            # Metrics pipeline stage
│   └── health/
│       └── health.go                 # Health check endpoint
├── pkg/
│   ├── core/
│   │   ├── interfaces.go            # All interfaces
│   │   ├── types.go                 # Event, Route, etc.
│   │   ├── errors.go                # Domain errors
│   │   ├── session.go               # Session types
│   │   └── retention.go             # Retention types
│   ├── plugins/
│   │   ├── registry.go              # Plugin registry
│   │   ├── kafka/
│   │   │   └── endpoint.go
│   │   ├── mqtt/
│   │   │   └── endpoint.go
│   │   ├── sse/
│   │   │   └── entrypoint.go
│   │   ├── ws/
│   │   │   └── entrypoint.go
│   │   └── mock/
│   │       └── endpoint.go
│   ├── session/
│   │   ├── memory.go
│   │   ├── redis.go
│   │   └── factory.go
│   ├── retention/
│   │   ├── memory.go
│   │   ├── redis.go
│   │   └── factory.go
│   └── config/
│       ├── loader.go
│       ├── loader_test.go
│       └── watcher.go               # File watcher for hot-reload
├── config.yaml
├── Dockerfile
├── Makefile
├── go.mod
└── README.md
```

---

## 5. Phased Implementation Plan

### Phase 1: Core Abstractions & Types (Days 1–2)

**Goal**: Establish the foundational types and interfaces that everything else depends on.

#### 1.1 Refined Core Interfaces

````go
package core

import "context"

// Entrypoint accepts inbound traffic from clients
type Entrypoint interface {
    Name() string
    Type() string
    Start(ctx context.Context, publisher EventPublisher) error  // blocking, returns on ctx cancel
    Stop(ctx context.Context) error                              // graceful stop
    SendDownstream(ctx context.Context, evt Event) error
}

// Endpoint connects to backend systems
type Endpoint interface {
    Name() string
    Type() string
    Start(ctx context.Context, publisher EventPublisher) error
    Stop(ctx context.Context) error
    SendUpstream(ctx context.Context, evt Event) error
}

// EventPublisher is the interface plugins use to submit events
type EventPublisher interface {
    Publish(ctx context.Context, evt Event) error  // returns error on backpressure
}

// Policy evaluates a single event and returns an action
type Policy interface {
    Type() string
    Execute(ctx context.Context, evt *Event, config map[string]string) (PolicyAction, error)
}

// RoutePolicyEngine evaluates the full policy chain for a route
type RoutePolicyEngine interface {
    EvaluateWithRoute(ctx context.Context, evt Event, route *Route) (PolicyAction, *Event, error)
}

// SessionStore manages client session lifecycle
type SessionStore interface {
    Create(ctx context.Context, session *Session) error
    Get(ctx context.Context, clientID string) (*Session, error)
    Update(ctx context.Context, session *Session) error
    Delete(ctx context.Context, clientID string) error
    UpdateState(ctx context.Context, clientID string, state string) error
    Close() error
}

// RetentionStore persists events for replay/recovery
type RetentionStore interface {
    Store(ctx context.Context, clientID string, evt Event) error
    Retrieve(ctx context.Context, clientID string, since int64) ([]Event, error)
    Cleanup(ctx context.Context) error
    Close() error
}

// PipelineStage is a single processing step in the event pipeline
type PipelineStage interface {
    Name() string
    Process(ctx context.Context, ec *EventContext) error
}
````

#### 1.2 Refined Core Types

````go
package core

import "time"

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
    EventTypeData EventType = iota
    EventTypeConnect
    EventTypeDisconnect
)

func (e Event) IsLifecycle() bool {
    return e.Type == EventTypeConnect || e.Type == EventTypeDisconnect
}

// EventContext carries the event through the pipeline with accumulated state
type EventContext struct {
    Event         Event
    Route         *Route           // resolved in route stage
    ModifiedEvent *Event           // set by policy stage if transformed
    PolicyAction  PolicyAction     // set by policy stage
    Targets       []DeliveryTarget // resolved from route
    Err           error            // first error encountered
}

type DeliveryTarget struct {
    Name      string
    Direction Direction // upstream or downstream
}

type Direction int

const (
    DirectionUpstream Direction = iota
    DirectionDownstream
)

type PolicyAction int

const (
    ActionAllow PolicyAction = iota
    ActionBlock
    ActionTransform
)

type Route struct {
    Source   string       `yaml:"source"`
    Target  string       `yaml:"target"`
    Policies []PolicySpec `yaml:"policies"`
}

type PolicySpec struct {
    Type   string            `yaml:"type"`
    Config map[string]string `yaml:"config"`
}

type Session struct {
    ClientID    string
    State       string
    EntryPoint  string
    Metadata    map[string]string
    ConnectedAt time.Time
    UpdatedAt   time.Time
}
````

**Key changes from current**:
- `EventPublisher.Publish()` now returns `error` for backpressure signaling
- `Entrypoint/Endpoint.Start()` returns `error` and blocks (current design uses fire-and-forget goroutines)
- `Entrypoint/Endpoint.Stop()` added for graceful shutdown
- New `PipelineStage` interface replaces monolithic Hub processing
- `EventContext` carries per-event state through the pipeline (replaces scattered state in Hub)
- `Event.Timestamp` added for latency tracking
- `Event.Type` replaces string-based lifecycle detection

---

### Phase 2: Event Bus with Worker Pool (Days 2–3)

**Goal**: Replace the unbounded `go h.processEvent()` with a bounded worker pool.

````go
package bus

import (
    "context"
    "fmt"
    "log/slog"
    "sync"

    "mediation-engine/pkg/core"
)

type Config struct {
    QueueSize   int `yaml:"queue_size"`    // default: 4096
    WorkerCount int `yaml:"worker_count"`  // default: runtime.NumCPU() * 2
}

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
        return fmt.Errorf("event bus full (capacity: %d), dropping event %s", b.config.QueueSize, evt.ID)
    }
}

// Start launches workers and blocks until ctx is cancelled, then drains
func (b *Bus) Start(ctx context.Context) {
    for i := 0; i < b.config.WorkerCount; i++ {
        b.wg.Add(1)
        go b.worker(ctx, i)
    }
    <-ctx.Done()
    b.drain()
    b.wg.Wait()
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

**Why this matters**:
- Current code: `go h.processEvent(ctx, evt)` in hub.go — unbounded goroutines under load
- New: Fixed worker pool prevents goroutine explosion; backpressure returns error to publisher
- Graceful shutdown: `drain()` processes remaining events before workers exit

---

### Phase 3: Pipeline Architecture (Days 3–5)

**Goal**: Replace the monolithic `Hub.processEvent()` / `routeMessage()` with composable pipeline stages.

````go
package pipeline

import (
    "context"
    "log/slog"
    "time"

    "mediation-engine/pkg/core"
)

type Pipeline struct {
    stages []core.PipelineStage
    logger *slog.Logger
}

func New(logger *slog.Logger, stages ...core.PipelineStage) *Pipeline {
    return &Pipeline{stages: stages, logger: logger}
}

func (p *Pipeline) Execute(ctx context.Context, evt core.Event) {
    ec := &core.EventContext{
        Event:        evt,
        PolicyAction: core.ActionAllow,
    }

    start := time.Now()
    for _, stage := range p.stages {
        if err := stage.Process(ctx, ec); err != nil {
            p.logger.Error("pipeline stage failed",
                "stage", stage.Name(),
                "event_id", evt.ID,
                "error", err,
                "elapsed", time.Since(start),
            )
            return // stop pipeline on error
        }
        // Check if policy blocked the event
        if ec.PolicyAction == core.ActionBlock {
            p.logger.Debug("event blocked by policy",
                "stage", stage.Name(),
                "event_id", evt.ID,
            )
            return
        }
    }
    p.logger.Debug("pipeline completed",
        "event_id", evt.ID,
        "elapsed", time.Since(start),
    )
}
````

#### 3.1 Route Resolution Stage

````go
package pipeline

import (
    "context"
    "fmt"

    "mediation-engine/internal/routing"
    "mediation-engine/pkg/core"
)

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
        return fmt.Errorf("no route for source: %s", ec.Event.SourceID)
    }
    ec.Route = route
    return nil
}
````

#### 3.2 Lifecycle Stage (handles connect/disconnect before other stages)

````go
package pipeline

import (
    "context"

    "mediation-engine/pkg/core"
)

type LifecycleStage struct {
    sessions core.SessionStore
}

func NewLifecycleStage(sessions core.SessionStore) *LifecycleStage {
    return &LifecycleStage{sessions: sessions}
}

func (s *LifecycleStage) Name() string { return "lifecycle" }

func (s *LifecycleStage) Process(ctx context.Context, ec *core.EventContext) error {
    if !ec.Event.IsLifecycle() {
        return nil // pass through for data events
    }

    switch ec.Event.Type {
    case core.EventTypeConnect:
        return s.sessions.Create(ctx, &core.Session{
            ClientID:   ec.Event.ClientID,
            State:      "connected",
            EntryPoint: ec.Event.SourceID,
        })
    case core.EventTypeDisconnect:
        return s.sessions.Delete(ctx, ec.Event.ClientID)
    }

    // Lifecycle events don't continue through remaining stages
    ec.PolicyAction = core.ActionBlock
    return nil
}
````

#### 3.3 Policy Stage

````go
package pipeline

import (
    "context"
    "time"

    "mediation-engine/pkg/core"
)

type PolicyStage struct {
    engine  core.RoutePolicyEngine
    timeout time.Duration // per-route policy timeout
}

func NewPolicyStage(engine core.RoutePolicyEngine, timeout time.Duration) *PolicyStage {
    if timeout <= 0 {
        timeout = 5 * time.Second
    }
    return &PolicyStage{engine: engine, timeout: timeout}
}

func (s *PolicyStage) Name() string { return "policy" }

func (s *PolicyStage) Process(ctx context.Context, ec *core.EventContext) error {
    if ec.Route == nil || len(ec.Route.Policies) == 0 {
        return nil
    }

    pCtx, cancel := context.WithTimeout(ctx, s.timeout)
    defer cancel()

    action, modified, err := s.engine.EvaluateWithRoute(pCtx, ec.Event, ec.Route)
    if err != nil {
        return err
    }

    ec.PolicyAction = action
    if modified != nil {
        ec.ModifiedEvent = modified
    }
    return nil
}
````

#### 3.4 Retention Stage

````go
package pipeline

import (
    "context"

    "mediation-engine/pkg/core"
)

type RetentionStage struct {
    store  core.RetentionStore
    config core.RetentionConfig
}

func NewRetentionStage(store core.RetentionStore, config core.RetentionConfig) *RetentionStage {
    return &RetentionStage{store: store, config: config}
}

func (s *RetentionStage) Name() string { return "retention" }

func (s *RetentionStage) Process(ctx context.Context, ec *core.EventContext) error {
    if s.store == nil {
        return nil
    }

    evt := ec.Event
    if ec.ModifiedEvent != nil {
        evt = *ec.ModifiedEvent
    }

    // Fire-and-forget retention storage — don't block delivery
    go func() {
        _ = s.store.Store(context.Background(), evt.ClientID, evt)
    }()

    return nil
}
````

#### 3.5 Delivery Stage

````go
package pipeline

import (
    "context"
    "fmt"
    "log/slog"

    "mediation-engine/pkg/core"
)

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
        return fmt.Errorf("no route resolved")
    }

    evt := ec.Event
    if ec.ModifiedEvent != nil {
        evt = *ec.ModifiedEvent
    }

    target := ec.Route.Target

    // Try endpoint first (upstream delivery)
    if ep, ok := s.endpoints[target]; ok {
        return ep.SendUpstream(ctx, evt)
    }

    // Try entrypoint (downstream delivery)
    if ep, ok := s.entrypoints[target]; ok {
        return ep.SendDownstream(ctx, evt)
    }

    return fmt.Errorf("unknown delivery target: %s", target)
}
````

---

### Phase 4: Routing Table (Day 4)

**Goal**: Replace `map[string]*Route` with lock-free `sync.Map` for the hot path.

````go
package routing

import (
    "sync"

    "mediation-engine/pkg/core"
)

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
    // Delete all existing
    t.routes.Range(func(key, _ any) bool {
        t.routes.Delete(key)
        return true
    })
    // Insert new
    for _, r := range routes {
        t.routes.Store(r.Source, r)
    }
}
````

**Why**: Current `Hub.AddRoute()` uses `sync.RWMutex` on a regular map. `sync.Map` is optimized for read-heavy workloads (which route lookup is), and eliminates lock contention on the hot path.

---

### Phase 5: Policy Engine with Timeouts (Days 5–6)

**Goal**: Improve the existing `Engine` with per-policy timeouts and better error handling.

````go
package policy

import (
    "context"
    "fmt"
    "log/slog"
    "sync"
    "time"

    "mediation-engine/pkg/core"
)

type Engine struct {
    policies map[string]core.Policy
    mu       sync.RWMutex
    timeout  time.Duration
    logger   *slog.Logger
}

func NewEngine(logger *slog.Logger, policyTimeout time.Duration) *Engine {
    if policyTimeout <= 0 {
        policyTimeout = 1 * time.Second
    }
    return &Engine{
        policies: make(map[string]core.Policy),
        timeout:  policyTimeout,
        logger:   logger,
    }
}

func (e *Engine) RegisterPolicy(p core.Policy) {
    e.mu.Lock()
    e.policies[p.Type()] = p
    e.mu.Unlock()
}

func (e *Engine) EvaluateWithRoute(ctx context.Context, evt core.Event, route *core.Route) (core.PolicyAction, *core.Event, error) {
    if route == nil || len(route.Policies) == 0 {
        return core.ActionAllow, nil, nil
    }

    modified := evt
    for i, spec := range route.Policies {
        e.mu.RLock()
        p, exists := e.policies[spec.Type]
        e.mu.RUnlock()

        if !exists {
            e.logger.Warn("policy not found, skipping",
                "policy_type", spec.Type,
                "route_source", route.Source,
                "index", i,
            )
            continue
        }

        // Per-policy timeout
        pCtx, cancel := context.WithTimeout(ctx, e.timeout)
        action, err := p.Execute(pCtx, &modified, spec.Config)
        cancel()

        if err != nil {
            if pCtx.Err() == context.DeadlineExceeded {
                return core.ActionBlock, nil, fmt.Errorf("policy %s timed out after %v", spec.Type, e.timeout)
            }
            return core.ActionBlock, nil, fmt.Errorf("policy %s failed: %w", spec.Type, err)
        }

        if action == core.ActionBlock {
            return core.ActionBlock, nil, nil
        }
    }

    // Check if event was modified
    if modified.ID != evt.ID || string(modified.Payload) != string(evt.Payload) {
        return core.ActionTransform, &modified, nil
    }

    return core.ActionAllow, nil, nil
}
````

---

### Phase 6: Observability (Days 6–7)

**Goal**: Add Prometheus metrics and structured logging.

````go
package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
    EventsReceived = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "mediation_events_received_total",
            Help: "Total events received by source",
        },
        []string{"source_id", "event_type"},
    )

    EventsDelivered = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "mediation_events_delivered_total",
            Help: "Total events successfully delivered",
        },
        []string{"target", "direction"},
    )

    EventsDropped = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "mediation_events_dropped_total",
            Help: "Total events dropped (backpressure or policy block)",
        },
        []string{"reason"},
    )

    PipelineLatency = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "mediation_pipeline_duration_seconds",
            Help:    "Pipeline processing duration",
            Buckets: prometheus.DefBuckets,
        },
        []string{"stage"},
    )

    PolicyLatency = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "mediation_policy_duration_seconds",
            Help:    "Individual policy execution duration",
            Buckets: prometheus.DefBuckets,
        },
        []string{"policy_type"},
    )

    QueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "mediation_queue_depth",
        Help: "Current number of events in the bus queue",
    })

    ActiveConnections = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "mediation_active_connections",
            Help: "Active client connections by entrypoint",
        },
        []string{"entrypoint"},
    )
)

func Register(reg prometheus.Registerer) {
    reg.MustRegister(
        EventsReceived, EventsDelivered, EventsDropped,
        PipelineLatency, PolicyLatency, QueueDepth, ActiveConnections,
    )
}
````

#### Metrics Pipeline Stage (wraps any stage with timing)

````go
package metrics

import (
    "context"
    "time"

    "mediation-engine/pkg/core"
)

type InstrumentedStage struct {
    inner core.PipelineStage
}

func Instrument(stage core.PipelineStage) *InstrumentedStage {
    return &InstrumentedStage{inner: stage}
}

func (s *InstrumentedStage) Name() string { return s.inner.Name() }

func (s *InstrumentedStage) Process(ctx context.Context, ec *core.EventContext) error {
    start := time.Now()
    err := s.inner.Process(ctx, ec)
    PipelineLatency.WithLabelValues(s.inner.Name()).Observe(time.Since(start).Seconds())
    return err
}
````

---

### Phase 7: Plugin Lifecycle Management (Days 7–8)

**Goal**: Proper startup ordering, health checks, and restart on failure.

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

// StartAll starts endpoints first (backends), then entrypoints (clients).
// Endpoints start first so they're ready to receive when entrypoints accept connections.
func (r *Registry) StartAll(ctx context.Context, publisher core.EventPublisher) error {
    var wg sync.WaitGroup
    errCh := make(chan error, len(r.endpoints)+len(r.entrypoints))

    // Start endpoints first
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

    // Brief delay to let endpoints initialize
    time.Sleep(500 * time.Millisecond)

    // Start entrypoints
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

    go func() {
        wg.Wait()
        close(errCh)
    }()

    return nil
}

// StopAll gracefully stops entrypoints first (stop accepting), then endpoints
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

### Phase 8: Rewired Main (Day 8)

**Goal**: Clean dependency injection with explicit wiring.

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

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"

    "mediation-engine/internal/bus"
    "mediation-engine/internal/metrics"
    "mediation-engine/internal/pipeline"
    "mediation-engine/internal/policy"
    "mediation-engine/internal/routing"
    "mediation-engine/pkg/config"
    "mediation-engine/pkg/core"
    "mediation-engine/pkg/plugins"
    "mediation-engine/pkg/plugins/kafka"
    "mediation-engine/pkg/plugins/mock"
    "mediation-engine/pkg/plugins/mqtt"
    "mediation-engine/pkg/plugins/sse"
    "mediation-engine/pkg/plugins/ws"
    "mediation-engine/pkg/retention"
    "mediation-engine/pkg/session"
)

func main() {
    logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
    slog.SetDefault(logger)

    // 1. Load config
    cfg, err := config.Load("config.yaml")
    if err != nil {
        logger.Error("config load failed", "error", err)
        os.Exit(1)
    }

    // 2. Context with signal handling
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    // 3. Metrics
    reg := prometheus.DefaultRegisterer
    metrics.Register(reg)

    // 4. Session store
    sessionStore := session.NewMemoryStore()
    defer sessionStore.Close()

    // 5. Retention store
    var retentionStore core.RetentionStore
    retentionConfig := core.RetentionConfig{
        Enabled: cfg.Retention.Enabled,
        Mode:    core.RetentionMode(cfg.Retention.Mode),
    }
    if cfg.Retention.Enabled {
        retentionStore, err = retention.NewStore(retentionConfig)
        if err != nil {
            logger.Warn("retention store init failed", "error", err)
        } else {
            defer retentionStore.Close()
        }
    }

    // 6. Policy engine
    policyEngine := policy.NewEngine(logger, 1*time.Second)
    policy.RegisterBuiltinPolicies(policyEngine)

    // 7. Route table
    routeTable := routing.NewTable()
    for _, r := range cfg.Routes {
        route := &core.Route{Source: r.Source, Target: r.Target}
        for _, p := range r.Policies {
            route.Policies = append(route.Policies, core.PolicySpec{Type: p.Type, Config: p.Config})
        }
        routeTable.Add(route)
    }

    // 8. Plugin registry
    pluginReg := plugins.NewRegistry(logger)
    registerPlugins(cfg, pluginReg, sessionStore)

    // 9. Pipeline
    p := pipeline.New(logger,
        metrics.Instrument(pipeline.NewLifecycleStage(sessionStore)),
        metrics.Instrument(pipeline.NewRouteStage(routeTable)),
        metrics.Instrument(pipeline.NewPolicyStage(policyEngine, 5*time.Second)),
        metrics.Instrument(pipeline.NewRetentionStage(retentionStore, retentionConfig)),
        metrics.Instrument(pipeline.NewDeliveryStage(pluginReg.Entrypoints(), pluginReg.Endpoints(), logger)),
    )

    // 10. Event bus
    eventBus := bus.New(bus.Config{
        QueueSize:   4096,
        WorkerCount: 8,
    }, p, logger)

    // 11. Start plugins (they publish to bus)
    pluginReg.StartAll(ctx, eventBus)

    // 12. Metrics/health HTTP server
    go func() {
        mux := http.NewServeMux()
        mux.Handle("/metrics", promhttp.Handler())
        mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
            w.WriteHeader(http.StatusOK)
            w.Write([]byte("ok"))
        })
        srv := &http.Server{Addr: ":9090", Handler: mux}
        go func() {
            <-ctx.Done()
            srv.Shutdown(context.Background())
        }()
        logger.Info("metrics server starting", "addr", ":9090")
        srv.ListenAndServe()
    }()

    // 13. Start bus (blocks until ctx cancelled, then drains)
    logger.Info("mediation engine starting")
    eventBus.Start(ctx)

    // 14. Graceful shutdown
    shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer shutdownCancel()
    pluginReg.StopAll(shutdownCtx)
    logger.Info("mediation engine stopped")
}

func registerPlugins(cfg *config.Config, reg *plugins.Registry, sessionStore core.SessionStore) {
    for _, e := range cfg.Entrypoints {
        switch e.Type {
        case "websocket":
            plugin := ws.New(e.Name, e.Port).WithSessionStore(sessionStore)
            reg.RegisterEntrypoint(plugin)
        case "sse":
            plugin := sse.New(e.Name, e.Port)
            reg.RegisterEntrypoint(plugin)
        }
    }
    for _, e := range cfg.Endpoints {
        switch e.Type {
        case "kafka":
            brokers := strings.Split(e.Config["brokers"], ",")
            plugin := kafka.New(e.Name, brokers, e.Config["topic_in"], e.Config["topic_out"])
            reg.RegisterEndpoint(plugin)
        case "mqtt":
            plugin := mqtt.New(e.Name, e.Config["broker"], e.Config["topic_in"], e.Config["topic_out"])
            reg.RegisterEndpoint(plugin)
        case "mock":
            plugin := mock.New(e.Name)
            reg.RegisterEndpoint(plugin)
        }
    }
}
````

---

### Phase 9: Config Hot-Reload (Day 9)

````go
package config

import (
    "context"
    "log/slog"
    "os"
    "time"

    "mediation-engine/pkg/core"
    "mediation-engine/internal/routing"
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
                    route := &core.Route{Source: r.Source, Target: r.Target}
                    for _, p := range r.Policies {
                        route.Policies = append(route.Policies, core.PolicySpec{Type: p.Type, Config: p.Config})
                    }
                    routes = append(routes, route)
                }
                w.table.ReplaceAll(routes)
                w.logger.Info("routes reloaded", "count", len(routes))
            }
        }
    }
}
````

---

### Phase 10: Testing Strategy (Days 9–11)

| Layer | What to Test | Approach |
|---|---|---|
| **Pipeline stages** | Each stage in isolation | Unit tests with mock interfaces |
| **Event bus** | Backpressure, draining, concurrency | Unit tests with synthetic load |
| **Policy engine** | Timeout, chain evaluation, transform detection | Unit tests |
| **Route table** | Concurrent read/write, hot-reload | Race detector tests (`-race`) |
| **Integration** | Full flow: WS → Hub → Kafka | Docker Compose test (as in it) |
| **Load** | Throughput under sustained load | Benchmarks with `testing.B` |

Example unit test for the pipeline:

````go
package pipeline

import (
    "context"
    "log/slog"
    "testing"

    "mediation-engine/pkg/core"
)

type mockStage struct {
    name    string
    called  bool
    action  core.PolicyAction
    err     error
}

func (m *mockStage) Name() string { return m.name }
func (m *mockStage) Process(ctx context.Context, ec *core.EventContext) error {
    m.called = true
    ec.PolicyAction = m.action
    return m.err
}

func TestPipeline_StopsOnBlock(t *testing.T) {
    blocker := &mockStage{name: "blocker", action: core.ActionBlock}
    after := &mockStage{name: "after", action: core.ActionAllow}

    p := New(slog.Default(), blocker, after)
    p.Execute(context.Background(), core.Event{ID: "test-1"})

    if !blocker.called {
        t.Error("blocker should have been called")
    }
    if after.called {
        t.Error("after should NOT have been called after block")
    }
}

func TestPipeline_AllStagesRun(t *testing.T) {
    s1 := &mockStage{name: "s1", action: core.ActionAllow}
    s2 := &mockStage{name: "s2", action: core.ActionAllow}
    s3 := &mockStage{name: "s3", action: core.ActionAllow}

    p := New(slog.Default(), s1, s2, s3)
    p.Execute(context.Background(), core.Event{ID: "test-2"})

    for _, s := range []*mockStage{s1, s2, s3} {
        if !s.called {
            t.Errorf("stage %s should have been called", s.name)
        }
    }
}
````

---

## 6. Migration Strategy

| Step | Action | Risk |
|---|---|---|
| 1 | Implement new `pkg/core/` types alongside existing | None — additive |
| 2 | Build `internal/bus/`, `internal/pipeline/`, `internal/routing/` | None — new packages |
| 3 | Refactor `internal/policy/` to new Engine API | Low — interface compatible |
| 4 | Add `Stop()` to existing plugins (ws, sse, kafka, mqtt) | Low — add method |
| 5 | Rewrite main.go with new wiring | Medium — full switchover |
| 6 | Remove old hub.go | After validation |
| 7 | Add metrics, health endpoint | None — additive |

---

## 7. Performance Targets

| Metric | Current (estimated) | Target |
|---|---|---|
| Event throughput | ~5K/s (unbounded goroutines) | 50K/s (worker pool) |
| P99 pipeline latency | Unknown | < 5ms (no-policy) |
| P99 pipeline latency (with policies) | Unknown | < 20ms |
| Memory per connection | ~8KB goroutine stack | Fixed pool (configurable) |
| Graceful shutdown | Not implemented | < 30s drain |
| Backpressure | Silent drop | Error returned to publisher |

---

## 8. Summary of Key Improvements Over Current Implementation

1. **Bounded concurrency** — Worker pool in `Bus` replaces unbounded goroutines in `Hub.Start()`
2. **Pipeline pattern** — Composable stages replace monolithic `processEvent()`/`routeMessage()` in `Hub`
3. **Backpressure** — `Publish()` returns error when queue is full instead of silent buffering
4. **Graceful shutdown** — Drain queue → stop entrypoints → stop endpoints
5. **Per-policy timeouts** — Prevents slow policies from blocking the pipeline
6. **Observability** — Prometheus metrics for every stage, structured JSON logging
7. **Lock-free routing** — `sync.Map` replaces `sync.RWMutex` + `map` for route lookups
8. **Config hot-reload** — Route changes without restart
9. **Plugin lifecycle** — Ordered startup (endpoints before entrypoints), graceful stop
10. **Testability** — Every component is interface-driven and independently testable