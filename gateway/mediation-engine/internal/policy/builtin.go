package policy

import (
	"context"
	"strings"

	"mediation-engine/pkg/core"
)

type RateLimitPolicy struct{}

func (p *RateLimitPolicy) Name() string { return "rate-limit" }
func (p *RateLimitPolicy) Type() string { return "rate-limit" }

func (p *RateLimitPolicy) Execute(ctx context.Context, evt *core.Event, config map[string]string) (core.PolicyAction, error) {
	return core.ActionAllow, nil
}

type FilterPolicy struct{}

func (p *FilterPolicy) Name() string { return "filter" }
func (p *FilterPolicy) Type() string { return "filter" }

func (p *FilterPolicy) Execute(ctx context.Context, evt *core.Event, config map[string]string) (core.PolicyAction, error) {
	if pattern, ok := config["block_pattern"]; ok {
		if strings.Contains(string(evt.Payload), pattern) {
			return core.ActionBlock, nil
		}
	}
	return core.ActionAllow, nil
}

type TransformPolicy struct{}

func (p *TransformPolicy) Name() string { return "transform" }
func (p *TransformPolicy) Type() string { return "transform" }

func (p *TransformPolicy) Execute(ctx context.Context, evt *core.Event, config map[string]string) (core.PolicyAction, error) {
	if prefix, ok := config["prefix"]; ok {
		evt.Payload = append([]byte(prefix), evt.Payload...)
	}
	if key, ok := config["add_header"]; ok {
		if evt.Metadata == nil {
			evt.Metadata = make(map[string]string)
		}
		evt.Metadata[key] = config["header_value"]
	}
	return core.ActionAllow, nil
}

func RegisterBuiltinPolicies(e *Engine) {
	e.RegisterPolicy(&RateLimitPolicy{})
	e.RegisterPolicy(&FilterPolicy{})
	e.RegisterPolicy(&TransformPolicy{})
}
