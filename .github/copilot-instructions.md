# WSO2 API Platform - Copilot Instructions

## Architecture Overview

This is a **multi-component API platform** with independent, Docker-distributed components. Key architectural decisions:

- **GitOps-ready**: All configs (API specs, gateway configs) are YAML-first
- **Envoy-based Gateway**: Router uses Envoy Proxy; Policy Engine extends via `ext_proc` filter
- **xDS Protocol**: Gateway-Controller pushes configs to Router via gRPC (port 18000)
- **MCP-enabled**: Components expose Model Context Protocol for AI agent integration

### Component Boundaries

```
platform-api (9243)     → Backend for Management/Developer portals, org/project/gateway CRUD
gateway-controller (9090/18000) → xDS control plane, API config validation & persistence
policy-engine (9002)    → ext_proc service for request/response policy chains
router (8080/8443)      → Envoy Proxy data plane
mediation-engine        → Event-driven async protocol bridging (WS, SSE, Kafka, MQTT)
```

### Data Flow

1. User submits API YAML → Gateway-Controller validates & persists
2. Gateway-Controller translates to xDS resources → pushes to Router via gRPC
3. Router routes traffic; invokes Policy Engine via ext_proc for policy execution

## Build & Development

### Quick Start (Full Stack)

```bash
cd distribution/all-in-one && docker compose up --build
```

### Gateway-Only Development

```bash
cd gateway
make build-local           # Build all gateway images locally (fastest)
docker compose up -d       # Start gateway stack
make test-integration-all  # Run BDD integration tests with coverage
```

### Component-Specific Builds

```bash
# Gateway components (from gateway/)
make build-local-controller
make build-local-policy-engine
make build-local-router
make build-local-mediation-engine

# CLI (from cli/src/)
make build              # Single OS
make build-all          # Cross-platform
```

## Go Module Structure

Components use `replace` directives for local development:

```go
// In component go.mod:
replace github.com/wso2/api-platform/common => ../common
replace github.com/wso2/api-platform/sdk => ../sdk
```

- **common/**: Shared authenticators, models, constants, errors
- **sdk/**: Gateway SDK utilities

## API Configuration Format

All API configs follow this structure (see [gateway/examples/](gateway/examples/)):

```yaml
apiVersion: gateway.api-platform.wso2.com/v1alpha1
kind: RestApi
metadata:
  name: api-name-version
spec:
  displayName: Human Name
  version: v1.0
  context: /base/$version     # $version is variable substitution
  upstream:
    main:
      url: https://backend:port/path
  operations:
    - method: GET
      path: /resource/{id}
      policies:               # Operation-level policies
        - name: policy-name
          version: v1.0.0
          params: {}
```

## Policy Engine Pattern

**Critical**: Policy Engine ships with NO built-in policies. All policies compiled via Gateway Builder:

```bash
# Build custom policies
docker run --rm \
    -v $(pwd)/sample-policies:/workspace/sample-policies \
    -v $(pwd)/policy-manifest.yaml:/workspace/policy-manifest.yaml \
    -v $(pwd)/output:/workspace/output \
    wso2/api-platform/gateway-builder:latest
```

Policy interface ([gateway/policy-engine/](gateway/policy-engine/)):
```go
type Policy interface {
    Name() string
    Type() string
    Execute(ctx context.Context, evt *Event, config map[string]string) (PolicyAction, error)
}
```

## Mediation Engine (Async Protocols)

Event-driven architecture for protocol bridging ([gateway/mediation-engine/](gateway/mediation-engine/)):

- **Entrypoints** (sources): WebSocket, SSE - accept external traffic
- **Endpoints** (sinks): Kafka, MQTT - connect to backends
- **Hub**: Routes events through policy chains

```go
// Core interfaces in pkg/core/interfaces.go
type Entrypoint interface {
    Start(ctx context.Context, hub IngressHub) error
    SendDownstream(ctx context.Context, evt Event) error
}
```

## Testing

### Integration Tests (BDD with Godog)

```bash
cd gateway/it
make test-all     # Build coverage images + run tests

# Run specific scenario
go test -v ./... -godog.tags="@wip"
```

Feature files in `gateway/it/features/` - see [gateway/it/CONTRIBUTING.md](gateway/it/CONTRIBUTING.md) for writing tests.

### Unit Tests

```bash
make test                    # Gateway tests (from gateway/)
cd platform-api/src && go test ./...
cd cli/src && make test
```

## Portals

- **Management Portal** (5173): React + TypeScript + Vite
- **Developer Portal** (3001): Node.js, requires PostgreSQL

Both require Platform API running. Quick setup:
```bash
cd distribution/all-in-one && docker compose up
```

## Version Management

```bash
make version                           # Show all versions
make version-set COMPONENT=gateway VERSION_ARG=1.2.0
make version-bump-patch COMPONENT=gateway
```

Versions tracked in `VERSION` files: root, `gateway/VERSION`, `platform-api/VERSION`

## Documentation Standards

Follow [guidelines/DOCUMENTATION.md](guidelines/DOCUMENTATION.md):
- Component README: Quick start only, link to `spec/` for details
- `spec/constitution.md`: Core principles for major components
- Specs link requirements to implementation features
