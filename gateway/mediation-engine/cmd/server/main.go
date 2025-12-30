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
	"mediation-engine/pkg/plugins/kafka"
	"mediation-engine/pkg/plugins/mqtt"
	"mediation-engine/pkg/plugins/sse"
	"mediation-engine/pkg/plugins/ws"
)

func main() {
	fmt.Println("Starting gateway-mediation-engine:v2")
	// 1. Load Config
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	// 2. Init Hub
	hub := engine.NewHub(&policy.DefaultEngine{})

	ctx, cancel := context.WithCancel(context.Background())

	// 3. Register Entrypoints
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

	// 4. Register Endpoints
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

	// 5. Load Routes
	for _, r := range cfg.Routes {
		hub.AddRoute(r.Source, r.Destination)
		log.Printf("[Route] %s -> %s", r.Source, r.Destination)
	}

	// 6. Start Hub
	go hub.Start(ctx)

	// Wait for Shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
	cancel()
}
