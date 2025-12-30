package policy

import (
	"context"
	"mediation-engine/pkg/core"
)

type DefaultEngine struct{}

func (d *DefaultEngine) Evaluate(ctx context.Context, evt core.Event) (core.PolicyAction, error) {
	// Future Logic:
	// if evt.Metadata["auth"] == "" { return core.ActionBlock, nil }
	
	// For now, allow everything
	return core.ActionAllow, nil
}