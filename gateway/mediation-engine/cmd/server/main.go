package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"mediation-engine/internal/engine"
	"mediation-engine/internal/policy"
	"mediation-engine/pkg/config"
	"mediation-engine/pkg/core"
	"mediation-engine/pkg/plugins/kafka"
	"mediation-engine/pkg/plugins/mqtt"
	"mediation-engine/pkg/plugins/sse"
	"mediation-engine/pkg/plugins/ws"
)

func main() {
	fmt.Println("Starting gateway-mediation-engine:v2")

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	policyEngine := policy.NewEngine()
	policy.RegisterBuiltinPolicies(policyEngine)
	hub := engine.NewHub(policyEngine)

	ctx, cancel := context.WithCancel(context.Background())

	for _, e := range cfg.Entrypoints {
		switch e.Type {
		case "websocket":
			plugin := ws.New(e.Name, e.Port)
			hub.RegisterEntrypoint(plugin)
			go plugin.Start(ctx, hub)
		case "sse":
			plugin := sse.New(e.Name, e.Port)
			hub.RegisterEntrypoint(plugin)
			go plugin.Start(ctx, hub)
		}
	}

	for _, e := range cfg.Endpoints {
		switch e.Type {
		case "kafka":
			brokers := strings.Split(e.Config["brokers"], ",")
			plugin := kafka.New(e.Name, brokers, e.Config["topic_in"], e.Config["topic_out"])
			hub.RegisterEndpoint(plugin)
			go plugin.Start(ctx, hub)
		case "mqtt":
			plugin := mqtt.New(e.Name, e.Config["broker"], e.Config["topic_in"], e.Config["topic_out"])
			hub.RegisterEndpoint(plugin)
			go plugin.Start(ctx, hub)
		}
	}

	for _, r := range cfg.Routes {
		route := &core.Route{
			Source:      r.Source,
			Destination: r.Destination,
			Policies:    make([]core.PolicySpec, len(r.Policies)),
		}
		for i, p := range r.Policies {
			route.Policies[i] = core.PolicySpec{
				Name:   p.Name,
				Type:   p.Type,
				Config: p.Config,
			}
		}
		hub.AddRoute(route)
		log.Printf("[Route] %s -> %s (policies: %d)", r.Source, r.Destination, len(r.Policies))
	}

	go hub.Start(ctx)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
	cancel()
}
