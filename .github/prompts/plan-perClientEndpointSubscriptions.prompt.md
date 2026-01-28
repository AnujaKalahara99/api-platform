# Plan: Per-Client Endpoint Subscription Management

Extend the mediation engine to create dedicated broker subscribers (Kafka/MQTT) for each client connected to an entrypoint. When a client connects, the system resolves routes to determine target endpoints and creates per-client subscriptions. On disconnect, only that client's subscriptions are removed, preserving other clients' delivery guarantees.

## Steps

1. **Add lifecycle event types** in `pkg/core/types.go` — introduce `EventType` field and constants (`EventTypeConnect`, `EventTypeDisconnect`, `EventTypeMessage`) to distinguish client lifecycle from data events.

2. **Extend `Endpoint` interface** in `pkg/core/interfaces.go` — add `SubscribeForClient(ctx, clientID, options)`, `UnsubscribeClient(ctx, clientID)`, and `DeliverToClient(clientID, Event)` methods for per-client subscription management.

3. **Implement per-client subscription in MQTT endpoint** in `pkg/plugins/mqtt/endpoint.go` — maintain `clientSubscriptions map[string]mqtt.Token`, create QoS-2 subscription per client with unique callback routing to Hub, cleanup on unsubscribe.

4. **Implement per-client consumer in Kafka endpoint** in `pkg/plugins/kafka/endpoint.go` — maintain `clientReaders map[string]*kafka.Reader` with per-client consumer groups, spawn read goroutine per client, close reader on unsubscribe.

5. **Emit lifecycle events from entrypoints** — modify `pkg/plugins/ws/entrypoint.go` and `pkg/plugins/sse/entrypoint.go` to publish `EventTypeConnect` after session creation and `EventTypeDisconnect` on client close.

6. **Handle lifecycle events in Hub** — update `internal/engine/hub.go` `processEvent()` to detect lifecycle events, resolve routes for the source entrypoint, call `SubscribeForClient`/`UnsubscribeClient` on destination endpoints.

## Further Considerations

1. **Per-client topic patterns?** Should clients subscribe to personalized topics (e.g., `events/{clientID}`) or all share the same topic with message filtering? Recommend: same topic, client-specific callback routing via `ClientID` in metadata.

2. **Session store integration in endpoints?** Endpoints could optionally use `SessionStore` to persist subscription state for recovery — defer this for now and keep subscriptions in-memory only? - do the efficient thing.

3. **SSE entrypoint client tracking** — SSE currently uses anonymous channels without client IDs. Should we add `ClientIdentity` tracking to SSE similar to WebSocket, or leave SSE as broadcast-only? - add that feature
