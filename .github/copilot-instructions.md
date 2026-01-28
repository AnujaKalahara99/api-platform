# WSO2 API Platform - Copilot Instructions

## Architecture

Multi-component API platform with Docker-distributed, independently deployable services:

```
platform-api (9243)         → Backend for portals, org/project/gateway CRUD
gateway-controller (9090)   → xDS control plane, API config validation (gRPC:18000)
policy-engine (9002)        → Envoy ext_proc filter for policy execution
router (8080/8443)          → Envoy Proxy data plane
mediation-engine            → Async protocol bridging (WS, SSE → Kafka, MQTT)
```

**Data flow**: API YAML → gateway-controller validates → xDS push to router → router invokes policy-engine via ext_proc

## Quick Start

```bash
# Full stack (includes portals, PostgreSQL)
cd distribution/all-in-one && docker compose up --build

# Gateway-only development
cd gateway && make build-local && docker compose up -d
```

## Go Module Pattern

Components use `replace` directives for local shared modules:
```go
replace github.com/wso2/api-platform/common => ../common
replace github.com/wso2/api-platform/sdk => ../sdk
```
- `common/`: Shared authenticators, models, constants, errors
- `sdk/`: Gateway SDK utilities

## API Configuration Format

All API configs use this CRD-style YAML (examples in [gateway/examples/](gateway/examples/)):
```yaml
apiVersion: gateway.api-platform.wso2.com/v1alpha1
kind: RestApi
metadata:
  name: weather-api-v1.0
spec:
  context: /weather/$version    # $version substitution supported
  upstream:
    main:
      url: http://backend:5000/api/v2
  policies:                     # API-level or operation-level
    - name: apiKeyValidation
      version: v1.0.0
      executionCondition: "request.metadata[authenticated] != true"  # CEL
      params: { header: "X-API-Key" }
  operations:
    - method: GET
      path: /{country_code}/{city}
```

## Policy Engine

**Critical**: Policy Engine has ZERO built-in policies. All policies compiled via Gateway Builder:
```bash
docker run --rm \
    -v $(pwd)/sample-policies:/workspace/sample-policies \
    -v $(pwd)/policy-manifest.yaml:/workspace/policy-manifest.yaml \
    -v $(pwd)/output:/workspace/output \
    wso2/api-platform/gateway-builder:latest
```
See [gateway/sample-policies/](gateway/sample-policies/) for reference implementations.

## Mediation Engine (Async Protocol Bridging)

Event-driven architecture for bidirectional protocol bridging. Located in [gateway/mediation-engine/](gateway/mediation-engine/).

Mediation Engine Architecture The Mediation Engine is a modular system that bridges web protocols (WebSocket, SSE, Webhooks) and event brokers (Kafka, MQTT, Solace) by converting all traffic into a protocol-agnostic Abstract Data Type (ADT). It connects defined Entrypoints to Endpoints via internal routes. Since upstream proxies (Envoy/WSO2) only validate the initial connection handshake, the Mediation Engine must enforce its own policies on individual data packets. All implementations must preserve this abstraction and include a resiliency layer to ensure delivery guarantees and data integrity during translation.

### Core Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Entrypoints   │────▶│       Hub        │────▶│    Endpoints    │
│ (WS, SSE)       │     │  (Central Router)│     │ (Kafka, MQTT)   │
│ Accept traffic  │◀────│  Policy chains   │◀────│ Backend systems │
└─────────────────┘     └──────────────────┘     └─────────────────┘
```

**Flow**: Entrypoint receives → Hub.Publish() → Route lookup → Policy evaluation → Endpoint.SendUpstream() or Entrypoint.SendDownstream()

### Key Interfaces ([pkg/core/interfaces.go](gateway/mediation-engine/pkg/core/interfaces.go))


### Event Structure ([pkg/core/types.go](gateway/mediation-engine/pkg/core/types.go))

```go
type Event struct {
    ID       string
    SourceID string            // Matches route.Source in config
    ClientID string            // Session tracking
    Payload  []byte
    Metadata map[string]string
}
```

### Configuration Format ([config.yaml](gateway/mediation-engine/config.yaml))

### Built-in Policies ([internal/policy/policies.go](gateway/mediation-engine/internal/policy/policies.go))

- `filter`: Block events matching `block_pattern` in payload
- `transform`: Add prefix to payload or headers to metadata
- `rate-limit`: (placeholder implementation)

Register custom policies via `policy.Engine.RegisterPolicy()`.

### Running Mediation Engine

```bash
cd gateway
make build-local-mediation-engine
docker compose up mediation-engine
```

## Testing

```bash
# Gateway integration tests (BDD with Godog)
cd gateway && make test-integration-all

# Run specific scenario
cd gateway/it && go test -v ./... -godog.tags="@wip"

# Unit tests
cd gateway && make test
cd platform-api/src && go test ./...
cd cli/src && make test
```

Integration test features in [gateway/it/features/](gateway/it/features/) - see [gateway/it/CONTRIBUTING.md](gateway/it/CONTRIBUTING.md) for writing tests.

## Portals

- **Management Portal** (5173): React + TypeScript + Vite - in `portals/management-portal/`
- **Developer Portal** (3001): Node.js, requires PostgreSQL - in `portals/developer-portal/`

## Key Conventions

- **YAML-first configs**: GitOps-ready, all API/gateway configs are YAML
- **Component independence**: No hard dependencies between components
- **Version files**: Track versions in `VERSION` files (root, `gateway/VERSION`, `platform-api/VERSION`)
- **Documentation**: README has quick start only; detailed specs in `spec/` directories
