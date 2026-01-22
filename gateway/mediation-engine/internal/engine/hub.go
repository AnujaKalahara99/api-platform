package engine

import (
	"context"
	"log"
	"sync"
	"time"

	"mediation-engine/pkg/core"
)

type Hub struct {
	entrypoints    map[string]core.Entrypoint
	endpoints      map[string]core.Endpoint
	routes         map[string]*core.Route
	clientBindings map[string]map[string]bool // clientID -> endpointName -> bound
	sessionStore   core.SessionStore          // For subscription tracking
	ingressChan    chan core.Event
	policy         core.RoutePolicyEngine

	mu sync.RWMutex
}

func NewHub(policy core.RoutePolicyEngine) *Hub {
	return &Hub{
		entrypoints:    make(map[string]core.Entrypoint),
		endpoints:      make(map[string]core.Endpoint),
		routes:         make(map[string]*core.Route),
		clientBindings: make(map[string]map[string]bool),
		ingressChan:    make(chan core.Event, 1000),
		policy:         policy,
	}
}

// WithSessionStore sets session store for binding tracking
func (h *Hub) WithSessionStore(store core.SessionStore) *Hub {
	h.sessionStore = store
	return h
}

func (h *Hub) RegisterEntrypoint(e core.Entrypoint) { h.entrypoints[e.Name()] = e }
func (h *Hub) RegisterEndpoint(e core.Endpoint)     { h.endpoints[e.Name()] = e }

// GetEndpoint returns an endpoint by name, useful for lifecycle binding
func (h *Hub) GetEndpoint(name string) (core.Endpoint, bool) {
	endpoint, exists := h.endpoints[name]
	return endpoint, exists
}

// GetAllEndpoints returns all registered endpoints for binding
func (h *Hub) GetAllEndpoints() map[string]core.Endpoint {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make(map[string]core.Endpoint, len(h.endpoints))
	for name, endpoint := range h.endpoints {
		result[name] = endpoint
	}
	return result
}

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
		// Check if this endpoint supports lifecycle binding and client needs binding

		log.Printf("[Router] Routing event from %s to endpoint %s client %s", evt.SourceID, destName, evt.ClientID)
		if evt.ClientID != "" {
			if binder, ok := endpoint.(core.LifecycleBinder); ok {
				h.ensureClientBinding(ctx, evt.ClientID, destName, binder, route)
			}
		}
		endpoint.SendUpstream(ctx, evt)
		return
	}

	if entrypoint, ok := h.entrypoints[destName]; ok {
		entrypoint.SendDownstream(ctx, evt)
		return
	}

	log.Printf("[Router] Destination %s not found", destName)
}

// ensureClientBinding creates endpoint binding on first event from client to that endpoint
func (h *Hub) ensureClientBinding(ctx context.Context, clientID, endpointName string, binder core.LifecycleBinder, route *core.Route) {
	h.mu.Lock()
	defer h.mu.Unlock()
	log.Printf("[Binding] Ensuring client binding: client=%s endpoint=%s", clientID, endpointName)
	// Check if already bound
	if bindings, exists := h.clientBindings[clientID]; exists {
		if bindings[endpointName] {
			return // Already bound
		}
	}

	// Create binding
	log.Printf("[Binding] Creating endpoint binding: client=%s endpoint=%s", clientID, endpointName)
	if err := binder.BindClient(ctx, clientID, nil); err != nil {
		log.Printf("[Binding] Failed to bind client %s to endpoint %s: %v", clientID, endpointName, err)
		return
	}

	// Track binding
	if h.clientBindings[clientID] == nil {
		h.clientBindings[clientID] = make(map[string]bool)
	}
	h.clientBindings[clientID][endpointName] = true

	// Update session subscriptions if session store is available
	if h.sessionStore != nil {
		session, err := h.sessionStore.Get(ctx, clientID)
		if err == nil {
			subscription := core.Subscription{
				EndpointName: endpointName,
				RouteName:    route.Source + "->" + route.Destination,
				CreatedAt:    time.Now().UTC(),
			}
			session.Subscriptions = append(session.Subscriptions, subscription)
			h.sessionStore.Update(ctx, session)
			log.Printf("[Binding] Updated session subscriptions for client %s", clientID)
		}
	}
}
