# Plan: Mediation Engine UDS + MediationAPI Kind

**TL;DR:** Replace the per-entrypoint TCP port system with a single UDS HTTP server in the mediation engine. Envoy routes all `MediationAPI` traffic to this UDS. The URL path's first segment after the API context identifies the entrypoint. The gateway controller gets a new `MediationApi` kind with its own xDS translation that creates a mediation-engine cluster (UDS pipe) and path-prefix routes. The static `config.yaml` continues to define entrypoints, endpoints, and routes — the `MediationAPI` spec only maps an API context to entrypoint names.

## A. Mediation Engine — Single UDS Server

1. **Add UDS socket path constant and env var**: In `cmd/server/main.go`, read `MEDIATION_SOCKET_PATH` env (default `/var/run/api-platform/mediation-engine.sock`). Remove per-port listener logic.

2. **Change `Entrypoint` interface** in `pkg/core/interfaces.go`: Replace `Start(ctx, manager) error` with `RegisterRoutes(mux *http.ServeMux, manager SessionManager)`. Keep `Stop(ctx) error` for cleanup, and `Name() string` / `Type() string`.

3. **Refactor each entrypoint plugin** to implement the new interface — register its HTTP handler(s) under `/{name}`:

   - **WebSocket** `ws/entrypoint.go`: Remove `http.Server` field, remove `Start()`. In `RegisterRoutes()`, register `handleConnection` at `/{name}`. The handler already does WebSocket upgrade.

   - **SSE** `sse/entrypoint.go`: Same pattern — register `handleSSE` at `/{name}`.

   - **HTTP POST** `httppost/entrypoint.go`: Register `handleWebhook` at `/{name}`.

   - **HTTP GET** `httpget/entrypoint.go`: Has 3 sub-paths (`/subscribe`, `/poll`, `/unsubscribe`). Register a sub-mux at `/{name}/` that dispatches to these.

4. **Remove `port` from config** in `pkg/config/loader.go`: Remove `Port` field from `EntrypointConfig`. Update `config.yaml` to remove all `port:` lines.

5. **Remove `port` from entrypoint constructors**: Each `New()` function (ws, sse, httppost, httpget) no longer takes a `port` parameter.

6. **Rewrite `Registry.StartEntrypoints()`** in `pkg/plugins/registry.go`: Instead of launching each entrypoint in its own goroutine, create a single `http.ServeMux`, call `RegisterRoutes(mux, manager)` on every entrypoint, then listen on the UDS path. Add a health-check endpoint at `/healthz`.

7. **Update `main.go`** in `cmd/server/main.go`: Pass the socket path to the registry. Remove the per-entrypoint goroutine launch pattern. The registry now owns the single UDS listener.

## B. Gateway Controller — MediationApi Kind

8. **Extend OpenAPI spec** in `api/openapi.yaml`: Add `MediationApi` to the `APIConfiguration.kind` enum. Add a `MediationAPIData` schema with fields: `displayName`, `version`, `context`, `entrypoints` (array of `{name: string}`), optional `policies`. Add it to the `spec.anyOf`.

9. **Regenerate API types**: Run `oapi-codegen` (per `oapi-codegen.yaml`) to generate the new `MediationApi` constant and `MediationAPIData` struct in `pkg/api/generated/`.
DO NOT modify the generated code manually.

10. **Add constants** in `pkg/constants/constants.go`:
    - `MediationEngineClusterName = "api-platform/mediation-engine"`
    - `DefaultMediationEngineSocketPath = "/var/run/api-platform/mediation-engine.sock"`

11. **Add `MediationEngineConfig`** to `pkg/config/config.go`: Mirror the `PolicyEngineConfig` pattern — fields for `Enabled bool`, `Mode string` (uds/tcp), `Host`, `Port`, `SocketPath`. Add to the `RouterConfig` struct. Default: `Enabled: false, Mode: "uds"`.

12. **Add validator** — new file `pkg/config/mediation_validator.go`: Validate `MediationAPIData` — require `displayName`, `version`, `context`, non-empty `entrypoints` list, each entrypoint has a `name`.

13. **Wire validator into `APIValidator`** in `pkg/config/api_validator.go`: Add a `case api.MediationApi:` branch in `validateAPIConfiguration()` that calls the mediation validator.

14. **Update `StoredConfig` accessors** in `pkg/models/stored_config.go`: Add `MediationApi` branches to `GetCompositeKey()`, `GetDisplayName()`, `GetVersion()`, `GetContext()`, `GetPolicies()` — extract from `MediationAPIData` via `Spec.AsMediationAPIData()`.

15. **Add xDS translation** in `pkg/xds/translator.go`:

    - In `TranslateConfigs()` (~L199), add branch: `else if cfg.Configuration.Kind == api.MediationApi { translateMediationAPIConfig(cfg) }`.

    - New method `translateMediationAPIConfig(cfg)`: Extract `MediationAPIData`, for each entrypoint name create a route with:
      - **Match**: regex `/{context}/{version}/{entrypoint-name}(/.*)?`
      - **Route action**: cluster `MediationEngineClusterName`
      - **Regex rewrite**: `/{entrypoint-name}\1` (strip API context, keep entrypoint name + rest)
      - **Dynamic metadata**: set `api_kind: "MediationApi"`, `api_name`, etc. (for policy engine / analytics)

    - New method `createMediationEngineCluster()`: Mirror `createPolicyEngineCluster()` — UDS pipe to `DefaultMediationEngineSocketPath` for UDS mode, `SocketAddress` for TCP mode. Cluster type `STATIC` for UDS. **Enable WebSocket upgrade** on the cluster by adding `upgrade_configs: [{upgrade_type: "websocket"}]` to the HTTP connection manager.

    - In `TranslateConfigs()` (~L311), add mediation engine cluster to the snapshot if `MediationEngine.Enabled` is true (parallel to the policy engine cluster block).

16. **Enable WebSocket upgrade in listener** in `createListener()` (~L620): Add `upgrade_configs: [{upgrade_type: "websocket"}]` to the HTTP connection manager when mediation engine is enabled. This lets Envoy tunnel WebSocket connections to the mediation engine.

17. **Update deployment flow** in `pkg/api/handlers/handlers.go` and the deployment service: Ensure `MediationApi` kind is accepted in `CreateAPI` / `ListAPIs` / `DeleteAPI`. Since we're extending the `APIConfiguration.kind` enum (not a separate config type like MCP), the existing `deploymentService.DeployAPIConfiguration()` flow should work with minimal changes — just add the kind to allowed kinds.

## C. Runtime — Docker & Entrypoint Script

18. **Update `docker-entrypoint.sh`**: When `MEDIATION_ENABLED=true`: start mediation engine before Envoy (like policy engine), wait for `/var/run/api-platform/mediation-engine.sock` to appear before starting Envoy. Pass `MEDIATION_SOCKET_PATH` env to mediation engine process.

19. **Update Dockerfile**: Remove `EXPOSE 8066 8067 8068 8069` from the exposed ports list.

20. **Update `docker-compose.yaml`**: Remove port mappings for 8066-8069 from the gateway-runtime service.

## D. Gateway Controller Config Integration

21. **Pass mediation engine config to translator** in `pkg/config/config.go`: Add `MediationEngine MediationEngineConfig` to `RouterConfig`. Parse from config file / env — pattern identical to `PolicyEngine`.

22. **Update test config** in `gateway/it/test-config.yaml`: Add `mediation_engine: { enabled: true, mode: uds }` under `gateway_controller.router` for integration tests.

## E. Tests

23. **Unit tests — mediation engine**: Update existing tests in `routing/table_test.go`, `session/manager_test.go`, `config/loader_test.go` to remove port references. Add test for UDS server startup and path-based routing.

24. **Unit tests — gateway controller**: Add `mediation_validator_test.go`. Add translator test for `translateMediationAPIConfig()` verifying correct routes, cluster, and path rewrite. Update `config_test.go` for new `MediationEngineConfig`.

25. **Integration test**: Add a feature file in `gateway/it/features/` for MediationAPI — deploy a MediationAPI, send a WebSocket/HTTP request to the API context path, verify it reaches the mediation engine entrypoint via UDS.

## Verification

- `cd gateway/gateway-runtime/mediation-engine && go test ./...` — all mediation engine unit tests pass
- `cd gateway/gateway-controller && go test ./...` — controller unit tests pass (including new validator, translator tests)
- `cd gateway && make build-local` — Docker images build successfully
- `cd gateway/it && make test` — integration tests pass (after docker-compose image swap per copilot-instructions)
- Manual: deploy a MediationAPI via REST API, verify Envoy routes WebSocket/HTTP traffic through UDS to the correct entrypoint

## Key Design Decisions

- **Entrypoint interface change**: `Start() → RegisterRoutes(mux, manager)` — entrypoints become HTTP handlers rather than standalone servers. This is a breaking interface change but eliminates all per-port code
- **MediationApi as APIConfiguration kind variant** (not a separate config type like MCP) — simpler, uses existing deployment pipeline, no transformer needed
- **Config.yaml retained for infrastructure** — entrypoints/endpoints/routes are infrastructure config; MediationAPI only defines the external API surface (context, version, entrypoint exposure)
- **WebSocket upgrade in Envoy** — required since Envoy doesn't upgrade WebSocket by default; must be added to HCM `upgrade_configs`
