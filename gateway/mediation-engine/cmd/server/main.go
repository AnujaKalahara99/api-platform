package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"mediation-engine/internal/engine"
	"mediation-engine/internal/policy"
	"mediation-engine/pkg/config"
	"mediation-engine/pkg/core"
	"mediation-engine/pkg/plugins/kafka"
	"mediation-engine/pkg/plugins/mqtt"
	"mediation-engine/pkg/plugins/sse"
	"mediation-engine/pkg/plugins/ws"
	"mediation-engine/pkg/session"
)

func main() {
	fmt.Println("Starting gateway-mediation-engine:SnapShot")

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	// Initialize session store
	// sessionStore, err := session.NewStore(session.Config{
	// 	Type: session.StoreType(cfg.Session.Type),
	// 	Redis: session.RedisConfig{
	// 		Addr:      cfg.Session.Redis.Addr,
	// 		Password:  cfg.Session.Redis.Password,
	// 		DB:        cfg.Session.Redis.DB,
	// 		KeyPrefix: cfg.Session.Redis.KeyPrefix,
	// 	},
	// })

	// if err != nil {
	// 	log.Printf("Warning: Session store init failed, using memory: %v", err)
	// 	sessionStore = session.NewMemoryStore()
	// }

	sessionStore := session.NewMemoryStore()

	defer sessionStore.Close()

	// Parse reconnection window (default 5 minutes)
	reconnectionWindow := 5 * time.Minute
	if cfg.Session.ReconnectionWindow != "" {
		if duration, err := time.ParseDuration(cfg.Session.ReconnectionWindow); err == nil {
			reconnectionWindow = duration
		} else {
			log.Printf("Warning: Invalid reconnection_window '%s', using default 5m", cfg.Session.ReconnectionWindow)
		}
	}

	// Parse cleanup interval (default 30 seconds)
	cleanupInterval := 30 * time.Second
	if cfg.Session.CleanupInterval != "" {
		if duration, err := time.ParseDuration(cfg.Session.CleanupInterval); err == nil {
			cleanupInterval = duration
		} else {
			log.Printf("Warning: Invalid cleanup_interval '%s', using default 30s", cfg.Session.CleanupInterval)
		}
	}

	policyEngine := policy.NewEngine()
	policy.RegisterBuiltinPolicies(policyEngine)
	hub := engine.NewHub(policyEngine).WithSessionStore(sessionStore)

	ctx, cancel := context.WithCancel(context.Background())

	// Track endpoints that support lifecycle binding
	lifecycleBinders := make(map[string]core.LifecycleBinder)

	// Track WebSocket entrypoints for later wiring
	wsEntrypoints := make([]*ws.WSEntrypoint, 0)

	for _, e := range cfg.Entrypoints {
		switch e.Type {
		case "websocket":
			plugin := ws.New(e.Name, e.Port).
				WithSessionStore(sessionStore).
				WithReconnectionWindow(reconnectionWindow)
			hub.RegisterEntrypoint(plugin)
			wsEntrypoints = append(wsEntrypoints, plugin)
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
			lifecycleBinders[e.Name] = plugin // MQTT supports lifecycle binding
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

	// Wire endpoints to WebSocket entrypoints for lifecycle binding
	for _, wsEntry := range wsEntrypoints {
		wsEntry.WithEndpoints(lifecycleBinders)
		log.Printf("[Binding] Wired %d endpoints to WebSocket entrypoint %s", len(lifecycleBinders), wsEntry.Name())
	}

	// Start cleanup worker
	cleanupWorker := session.NewCleanupWorker(sessionStore, lifecycleBinders, cleanupInterval)
	go cleanupWorker.Start(ctx)

	go hub.Start(ctx)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
	cancel()
}
