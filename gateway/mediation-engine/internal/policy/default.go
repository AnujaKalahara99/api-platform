package policy

import (
	"context"
	"sync"

	"mediation-engine/pkg/core"
)

type Engine struct {
	policies map[string]core.Policy
	mu       sync.RWMutex
}

func NewEngine() *Engine {
	return &Engine{
		policies: make(map[string]core.Policy),
	}
}

func (e *Engine) RegisterPolicy(p core.Policy) {
	e.mu.Lock()
	e.policies[p.Type()] = p
	e.mu.Unlock()
}

func (e *Engine) Evaluate(ctx context.Context, evt core.Event) (core.PolicyAction, error) {
	return core.ActionAllow, nil
}

func (e *Engine) EvaluateWithRoute(ctx context.Context, evt core.Event, route *core.Route) (core.PolicyAction, *core.Event, error) {
	if route == nil || len(route.Policies) == 0 {
		return core.ActionAllow, nil, nil
	}

	modifiedEvt := evt
	for _, spec := range route.Policies {
		e.mu.RLock()
		p, exists := e.policies[spec.Type]
		e.mu.RUnlock()

		if !exists {
			continue
		}

		action, err := p.Execute(ctx, &modifiedEvt, spec.Config)
		if err != nil {
			return core.ActionBlock, nil, err
		}
		if action == core.ActionBlock || action == core.ActionDrop {
			return action, nil, nil
		}
	}

	if modifiedEvt.ID != evt.ID || string(modifiedEvt.Payload) != string(evt.Payload) {
		return core.ActionTransform, &modifiedEvt, nil
	}

	return core.ActionAllow, nil, nil
}
