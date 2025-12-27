package engine

import (
	"context"
	"log"
	v1 "mediation-engine/pkg/mediation/v1"
)

type Router struct {
	entrypoint v1.Entrypoint
	endpoint   v1.Endpoint

	// Channels act as the buffer/pipeline
	upstream   chan v1.Packet // Web -> Broker
	downstream chan v1.Packet // Broker -> Web
}

func NewRouter(entry v1.Entrypoint, end v1.Endpoint) *Router {
	return &Router{
		entrypoint: entry,
		endpoint:   end,
		upstream:   make(chan v1.Packet, 100),
		downstream: make(chan v1.Packet, 100),
	}
}

func (r *Router) Start(ctx context.Context) {
	// 1. Start the Interface Listeners in background goroutines
	go func() {
		log.Printf("[Router] Starting Entrypoint: %s", r.entrypoint.Name())
		if err := r.entrypoint.Start(ctx, r.upstream); err != nil {
			log.Printf("[Router] Entrypoint Error: %v", err)
		}
	}()

	go func() {
		log.Printf("[Router] Starting Endpoint: %s", r.endpoint.Name())
		if err := r.endpoint.Start(ctx, r.downstream); err != nil {
			log.Printf("[Router] Endpoint Error: %v", err)
		}
	}()

	log.Println("[Router] Pipeline Active. Waiting for data...")

	// 2. The Main Loop
	for {
		select {
		case <-ctx.Done():
			log.Println("[Router] Shutting down...")
			return

		// FLOW A: Web Client -> Messaging Broker (Upstream)
		case pkt := <-r.upstream:
			log.Printf("[UPSTREAM] %s -> %s | Payload: %s", pkt.SourceID, r.endpoint.Name(), string(pkt.Payload))
			// (Optional) Add logic here: Transformation, Validation, etc.
			go r.endpoint.Publish(ctx, pkt)

		// FLOW B: Messaging Broker -> Web Client (Downstream)
		case pkt := <-r.downstream:
			log.Printf("[DOWNSTREAM] %s -> %s | Payload: %s", pkt.SourceID, r.entrypoint.Name(), string(pkt.Payload))
			// (Optional) Add logic here: Transformation
			go r.entrypoint.SendToClient(ctx, pkt)
		}
	}
}
