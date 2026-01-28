package engine

import (
	"context"
	"log"
	"sync"
	"time"

	"mediation-engine/pkg/core"
	"mediation-engine/pkg/retention"
)

type Hub struct {
	entrypoints map[string]core.Entrypoint
	endpoints   map[string]core.Endpoint
	routes      map[string]*core.Route

	ingressChan chan core.Event
	policy      core.RoutePolicyEngine

	// Retention support
	retention       core.RetentionStore
	retentionConfig core.RetentionConfig
	clientOnline    map[string]bool      // clientID -> connected?
	clientLastSeen  map[string]time.Time // clientID -> last seen time

	mu sync.RWMutex
}

func NewHub(policy core.RoutePolicyEngine) *Hub {
	return &Hub{
		entrypoints:    make(map[string]core.Entrypoint),
		endpoints:      make(map[string]core.Endpoint),
		routes:         make(map[string]*core.Route),
		ingressChan:    make(chan core.Event, 1000),
		policy:         policy,
		clientOnline:   make(map[string]bool),
		clientLastSeen: make(map[string]time.Time),
	}
}

// WithRetention configures the hub with a retention store and global config
func (h *Hub) WithRetention(store core.RetentionStore, config core.RetentionConfig) *Hub {
	h.retention = store
	h.retentionConfig = config
	if store != nil {
		log.Printf("[Engine] Retention enabled: mode=%s, ttl=%v, max=%d",
			config.Mode, config.TTL, config.MaxPerClient)
	}
	return h
}

func (h *Hub) RegisterEntrypoint(e core.Entrypoint) { h.entrypoints[e.Name()] = e }
func (h *Hub) RegisterEndpoint(e core.Endpoint)     { h.endpoints[e.Name()] = e }

func (h *Hub) AddRoute(route *core.Route) {
	h.mu.Lock()
	h.routes[route.Source] = route
	h.mu.Unlock()
}

func (h *Hub) Publish(evt core.Event) {
	h.ingressChan <- evt
}

func (h *Hub) Start(ctx context.Context) {
	log.Println("[Engine] Mediation Hub Started. Waiting for events...")

	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-h.ingressChan:
			go h.processEvent(ctx, evt)
		}
	}
}

func (h *Hub) processEvent(ctx context.Context, evt core.Event) {
	// Handle lifecycle events
	if evt.IsLifecycle() {
		h.handleLifecycleEvent(ctx, evt)
		return
	}

	h.routeMessage(ctx, evt)
}

// handleLifecycleEvent manages client connect/disconnect at endpoints
func (h *Hub) handleLifecycleEvent(ctx context.Context, evt core.Event) {
	// Update client online status for retention
	h.mu.Lock()
	switch evt.Type {
	case core.EventTypeConnect:
		h.clientOnline[evt.ClientID] = true
		h.clientLastSeen[evt.ClientID] = time.Now()
	case core.EventTypeDisconnect:
		h.clientOnline[evt.ClientID] = false
		h.clientLastSeen[evt.ClientID] = time.Now()
	}
	h.mu.Unlock()

	// Replay retained events on client reconnect (from entrypoint)
	if evt.Type == core.EventTypeConnect && h.retention != nil {
		// Check if this connect is from an entrypoint (not endpoint)
		if _, isEntrypoint := h.entrypoints[evt.SourceID]; isEntrypoint {
			go h.replayRetainedEvents(ctx, evt.ClientID, evt.SourceID)
		}
	}

	h.mu.RLock()
	route, exists := h.routes[evt.SourceID]
	h.mu.RUnlock()

	if !exists {
		log.Printf("[Router] No route for lifecycle event from: %s", evt.SourceID)
		return
	}

	destName := route.Destination

	// Only handle lifecycle for endpoint destinations
	endpoint, ok := h.endpoints[destName]
	if !ok {
		return
	}

	switch evt.Type {
	case core.EventTypeConnect:
		opts := core.SubscribeOptions{
			QoS:      2, // Default QoS for delivery guarantees
			Metadata: evt.Metadata,
		}
		if err := endpoint.SubscribeForClient(ctx, evt.ClientID, opts); err != nil {
			log.Printf("[Router] Failed to subscribe client %s at %s: %v",
				evt.ClientID, destName, err)
		}

	case core.EventTypeDisconnect:
		if err := endpoint.UnsubscribeClient(ctx, evt.ClientID); err != nil {
			log.Printf("[Router] Failed to unsubscribe client %s at %s: %v",
				evt.ClientID, destName, err)
		}
	}
}

// replayRetainedEvents sends stored events to a reconnected client
func (h *Hub) replayRetainedEvents(ctx context.Context, clientID, entrypointName string) {
	entrypoint, ok := h.entrypoints[entrypointName]
	if !ok {
		return
	}

	entries, err := h.retention.Retrieve(ctx, clientID)
	if err != nil {
		log.Printf("[Retention] Failed to retrieve events for client %s: %v", clientID, err)
		return
	}

	if len(entries) == 0 {
		return
	}

	log.Printf("[Retention] Replaying %d events for client %s via %s",
		len(entries), clientID, entrypointName)

	successCount := 0
	for _, entry := range entries {
		// Mark as replay in metadata
		evt := entry.Event
		if evt.Metadata == nil {
			evt.Metadata = make(map[string]string)
		}
		evt.Metadata["replayed"] = "true"
		evt.Metadata["original_timestamp"] = entry.Timestamp.Format(time.RFC3339)

		if err := entrypoint.SendDownstream(ctx, evt); err != nil {
			log.Printf("[Retention] Failed to replay event %s: %v", evt.ID, err)
			continue
		}

		// Acknowledge successful delivery
		if err := h.retention.Acknowledge(ctx, clientID, evt.ID); err != nil {
			log.Printf("[Retention] Failed to acknowledge event %s: %v", evt.ID, err)
		}
		successCount++
	}

	log.Printf("[Retention] Replay complete for client %s: %d/%d events delivered",
		clientID, successCount, len(entries))
}

// routeMessage handles regular message routing
func (h *Hub) routeMessage(ctx context.Context, evt core.Event) {
	h.mu.RLock()
	route, exists := h.routes[evt.SourceID]
	h.mu.RUnlock()

	if !exists {
		log.Printf("[Router] No route defined for source: %s", evt.SourceID)
		return
	}

	action, modifiedEvt, err := h.policy.EvaluateWithRoute(ctx, evt, route)
	if err != nil {
		log.Printf("[Policy] Error evaluating policies for %s: %v", evt.SourceID, err)
		return
	}
	if action != core.ActionAllow && action != core.ActionTransform {
		log.Printf("[Policy] Blocked event from %s", evt.SourceID)
		return
	}

	if modifiedEvt != nil {
		evt = *modifiedEvt
	}

	destName := route.Destination

	if endpoint, ok := h.endpoints[destName]; ok {
		endpoint.SendUpstream(ctx, evt)
		return
	}

	if entrypoint, ok := h.entrypoints[destName]; ok {
		// Handle retention for entrypoint destinations
		h.deliverToEntrypoint(ctx, entrypoint, evt, route)
		return
	}

	log.Printf("[Router] Destination %s not found", destName)
}

// deliverToEntrypoint handles delivery with retention support
func (h *Hub) deliverToEntrypoint(ctx context.Context, entrypoint core.Entrypoint, evt core.Event, route *core.Route) {
	// Check if retention is configured for this route
	retentionConfig := h.getRouteRetentionConfig(route)

	// No retention configured - deliver directly
	if h.retention == nil || retentionConfig == nil {
		if err := entrypoint.SendDownstream(ctx, evt); err != nil {
			log.Printf("[Router] Delivery failed to %s: %v", entrypoint.Name(), err)
		}
		return
	}

	mode := retentionConfig.Mode
	if mode == "" {
		mode = h.retentionConfig.Mode
	}

	// Check client online status
	h.mu.RLock()
	isOnline := h.clientOnline[evt.ClientID]
	h.mu.RUnlock()

	switch mode {
	case core.ModeDisconnected:
		// Store only when client is offline
		if !isOnline {
			h.storeForRetention(ctx, evt, route.Source)
			log.Printf("[Retention] Client %s offline, stored event %s for later delivery",
				evt.ClientID, evt.ID)
			return
		}
		// Client online - deliver directly
		if err := entrypoint.SendDownstream(ctx, evt); err != nil {
			log.Printf("[Router] Delivery failed to %s (client %s): %v",
				entrypoint.Name(), evt.ClientID, err)
			// On delivery failure, store for retry
			h.storeForRetention(ctx, evt, route.Source)
		}

	case core.ModeAlways:
		// Store before every delivery attempt
		h.storeForRetention(ctx, evt, route.Source)

		if err := entrypoint.SendDownstream(ctx, evt); err != nil {
			log.Printf("[Router] Delivery failed to %s (client %s): %v",
				entrypoint.Name(), evt.ClientID, err)
			// Event remains in retention for retry
			return
		}

		// Acknowledge on successful delivery
		if err := h.retention.Acknowledge(ctx, evt.ClientID, evt.ID); err != nil {
			log.Printf("[Retention] Failed to acknowledge event %s: %v", evt.ID, err)
		}
	}
}

// storeForRetention stores an event in the retention layer
func (h *Hub) storeForRetention(ctx context.Context, evt core.Event, routeKey string) {
	if h.retention == nil || evt.ClientID == "" {
		return
	}

	entry := core.RetentionEntry{
		Event:     evt,
		Timestamp: time.Now(),
		RouteKey:  routeKey,
		Attempts:  0,
	}

	if err := h.retention.Store(ctx, evt.ClientID, entry); err != nil {
		log.Printf("[Retention] Failed to store event %s: %v", evt.ID, err)
	}
}

// getRouteRetentionConfig extracts retention config from route policies
func (h *Hub) getRouteRetentionConfig(route *core.Route) *core.RetentionConfig {
	for _, p := range route.Policies {
		if p.Type == "retention" {
			cfg := retention.ParseRetentionConfigFromPolicy(p.Config, h.retentionConfig)
			return &cfg
		}
	}
	return nil
}

// IsClientOnline returns whether a client is currently connected
func (h *Hub) IsClientOnline(clientID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.clientOnline[clientID]
}
