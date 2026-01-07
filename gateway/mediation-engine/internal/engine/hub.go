package engine

import (
	"context"
	"log"
	"sync"

	"mediation-engine/pkg/core"
)

type Hub struct {
	entrypoints map[string]core.Entrypoint
	endpoints   map[string]core.Endpoint
	routes      map[string]*core.Route

	ingressChan chan core.Event
	policy      core.RoutePolicyEngine

	mu sync.RWMutex
}

func NewHub(policy core.RoutePolicyEngine) *Hub {
	return &Hub{
		entrypoints: make(map[string]core.Entrypoint),
		endpoints:   make(map[string]core.Endpoint),
		routes:      make(map[string]*core.Route),
		ingressChan: make(chan core.Event, 1000),
		policy:      policy,
	}
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
		entrypoint.SendDownstream(ctx, evt)
		return
	}

	log.Printf("[Router] Destination %s not found", destName)
}
