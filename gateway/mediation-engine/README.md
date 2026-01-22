### Folder Structure

```text
mediation-engine/
├── config.yaml                  # Configuration
├── cmd/
│   └── server/
│       └── main.go              # Main wiring
├── internal/
│   ├── engine/
│   │   └── hub.go               # Central Switchboard with client binding
│   └── policy/
│       └── default.go           # Default Policy Engine
├── pkg/
│   ├── config/
│   │   └── loader.go            # YAML Parser
│   ├── core/
│   │   ├── interfaces.go        # Abstract Interfaces (includes LifecycleBinder)
│   │   ├── types.go             # Abstract Event Data & Subscription
│   │   ├── client.go            # Client identity extraction
│   │   └── session.go           # Session management with reconnection
│   ├── session/
│   │   ├── memory.go            # In-memory session store
│   │   ├── redis.go             # Redis session store
│   │   ├── factory.go           # Session store factory
│   │   └── cleanup.go           # Background cleanup worker
│   └── plugins/
│       ├── kafka/               # Kafka Endpoint
│       ├── mqtt/                # MQTT Endpoint with hybrid 1:1/1:N
│       ├── sse/                 # SSE Entrypoint
│       └── ws/                  # WS Entrypoint with reconnection logic└── go.mod
```

---

## Client-Bound Endpoint Lifecycle Management

The mediation engine supports **client-bound endpoint lifecycle management** with reconnection windows. This enables stateful, resilient connections where endpoint resources (like MQTT subscriptions) are tied to individual client sessions.

### Features

#### 1. **Hybrid Architecture (MQTT)**
- **Publish Path (1:N)**: Stateless connection pool for sending messages upstream
- **Consume Path (1:1)**: Dedicated persistent MQTT session per WebSocket client for receiving messages
- Broker-side message queuing during disconnections

#### 2. **Reconnection Window**
- Default 5-minute window for clients to reconnect
- Session state preserved during window
- Endpoint subscriptions remain active
- Automatic cleanup after expiration

#### 3. **Automatic Binding**
- First event from client to endpoint triggers binding
- Subscription state tracked in session
- Lazy binding on-demand

#### 4. **Modular Design**
- `LifecycleBinder` interface for endpoint plugins
- Works across different entrypoints (WebSocket, SSE)
- Pluggable session stores (memory, Redis)

### Configuration

Add session management settings to `config.yaml`:

```yaml
session:
  type: "memory"  # or "redis"
  reconnection_window: "5m"  # Client reconnection grace period
  cleanup_interval: "30s"    # Background cleanup scan interval
  redis:
    addr: "localhost:6379"
    password: ""
    db: 0
    keyPrefix: "mediation:session:"
```

### Lifecycle Flow

#### Connect
1. WebSocket client connects with optional `X-WSO2-Client-ID` header
2. Check for existing session:
   - **Within window**: Resume endpoint subscriptions
   - **Expired**: Cleanup old bindings, create new session
   - **New**: Create fresh session

#### First Event
1. Client sends message to endpoint route
2. Hub detects new client-endpoint pairing
3. Endpoint `BindClient()` creates dedicated resources
4. Subscription tracked in session

#### Disconnect
1. Client connection closes
2. Session state → `disconnected`
3. Reconnection deadline set (now + 5 minutes)
4. Endpoint subscriptions **remain active**

#### Reconnect (Within Window)
1. Same client reconnects (identified by Client ID)
2. Endpoint `ResumeClient()` reactivates subscriptions
3. Session state → `connected`
4. No data loss (broker queued messages)

#### Expiration (After Window)
1. Background cleanup worker scans expired sessions
2. Endpoint `UnbindClient()` tears down resources
3. Session deleted from store

### Implementation Details

#### Core Interfaces

**LifecycleBinder** ([pkg/core/interfaces.go](pkg/core/interfaces.go)):
```go
type LifecycleBinder interface {
    BindClient(ctx context.Context, clientID string, params map[string]string) error
    UnbindClient(ctx context.Context, clientID string) error
    ResumeClient(ctx context.Context, clientID string) error
}
```

**Session** ([pkg/core/session.go](pkg/core/session.go)):
```go
type Session struct {
    ClientID             string
    State                string  // "connected" | "disconnected"
    DisconnectedAt       *time.Time
    ReconnectionDeadline *time.Time
    Subscriptions        []Subscription
}
```

#### MQTT Hybrid Implementation

[pkg/plugins/mqtt/endpoint.go](pkg/plugins/mqtt/endpoint.go):
- **Producer pool**: 5 shared connections for `SendUpstream()`
- **Per-client consumers**: Dedicated MQTT connection with `CleanSession=false`
- Client connection ID: `engine-{endpoint}-client-{clientID}`

#### Cleanup Worker

[pkg/session/cleanup.go](pkg/session/cleanup.go):
- Background goroutine with configurable interval
- Scans `ListExpired()` from session store
- Unbinds from all subscribed endpoints
- Deletes expired sessions

### Extending to Other Endpoints

To add lifecycle binding to a new endpoint:

1. **Implement `LifecycleBinder`**:
```go
func (e *MyEndpoint) BindClient(ctx context.Context, clientID string, params map[string]string) error {
    // Create client-specific resources (consumer, subscription, etc.)
}

func (e *MyEndpoint) UnbindClient(ctx context.Context, clientID string) error {
    // Cleanup resources
}

func (e *MyEndpoint) ResumeClient(ctx context.Context, clientID string) error {
    // Reactivate existing resources
}
```

2. **Register in main.go**:
```go
lifecycleBinders[e.Name] = plugin
```

### Monitoring

Logs include:
- `[Binding] Creating endpoint binding: client={id} endpoint={name}`
- `[MQTT] Client {id} bound to endpoint {name} (subscribed to {topic})`
- `[CleanupWorker] Found {n} expired sessions`
- `[CleanupWorker] Unbound client {id} from endpoint {name}`

### Benefits

1. **Resilience**: Survives temporary disconnections
2. **No message loss**: Broker queuing during reconnection window
3. **Scalability**: Shared producers, per-client consumers
4. **Clean separation**: Stateless publish, stateful consume
5. **Automatic cleanup**: No resource leaks└── go.mod