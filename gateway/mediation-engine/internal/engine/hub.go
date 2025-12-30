package engine

import (
	"context"
	"log"
	"sync"
	"mediation-engine/pkg/core"
)

// Hub is the central switching unit.
type Hub struct {
	entrypoints map[string]core.Entrypoint
	endpoints   map[string]core.Endpoint
	routes      map[string]string // Map[SourceID] -> DestinationID
	
	ingressChan chan core.Event // The central event bus
	policy      core.PolicyEngine
	
	mu sync.RWMutex
}

func NewHub(policy core.PolicyEngine) *Hub {
	return &Hub{
		entrypoints: make(map[string]core.Entrypoint),
		endpoints:   make(map[string]core.Endpoint),
		routes:      make(map[string]string),
		ingressChan: make(chan core.Event, 1000), // Buffered Event Bus
		policy:      policy,
	}
}

// Register Components
func (h *Hub) RegisterEntrypoint(e core.Entrypoint) { h.entrypoints[e.Name()] = e }
func (h *Hub) RegisterEndpoint(e core.Endpoint)     { h.endpoints[e.Name()] = e }
func (h *Hub) AddRoute(source, dest string)         { h.routes[source] = dest }

// Publish is called by Entrypoints/Endpoints to push data into the Hub
func (h *Hub) Publish(evt core.Event) {
	h.ingressChan <- evt
}

// Start runs the main mediation loop
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

// processEvent handles Policy + Routing
func (h *Hub) processEvent(ctx context.Context, evt core.Event) {
	// 1. Policy Enforcement (The Hook)
	action, err := h.policy.Evaluate(ctx, evt)
	if err != nil || action != core.ActionAllow {
		log.Printf("[Policy] Blocked event from %s", evt.SourceID)
		return
	}

	// 2. Routing Lookup
	h.mu.RLock()
	destName, exists := h.routes[evt.SourceID]
	h.mu.RUnlock()

	if !exists {
		log.Printf("[Router] No route defined for source: %s", evt.SourceID)
		return
	}

	// 3. Dispatch
	// Check if destination is an Endpoint (Backend)
	if endpoint, ok := h.endpoints[destName]; ok {
		// log.Printf("[Flow] %s -> %s (Endpoint)", evt.SourceID, destName)
		endpoint.SendUpstream(ctx, evt)
		return
	}

	// Check if destination is an Entrypoint (Client)
	if entrypoint, ok := h.entrypoints[destName]; ok {
		// log.Printf("[Flow] %s -> %s (Entrypoint)", evt.SourceID, destName)
		entrypoint.SendDownstream(ctx, evt)
		return
	}
	
	log.Printf("[Router] Destination %s not found (Config error?)", destName)
}