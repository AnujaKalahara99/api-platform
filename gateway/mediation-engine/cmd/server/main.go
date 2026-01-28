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
	"mediation-engine/pkg/retention"
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

	// Initialize retention store
	var retentionStore core.RetentionStore
	retentionConfig := core.RetentionConfig{
		Enabled:      cfg.Retention.Enabled,
		Mode:         core.RetentionMode(cfg.Retention.Mode),
		Store:        cfg.Retention.Store,
		RedisAddr:    cfg.Retention.RedisAddr,
		TTL:          cfg.Retention.ParseTTL(),
		MaxPerClient: cfg.Retention.MaxPerClient,
		ClientExpiry: cfg.Retention.ParseClientExpiry(),
	}

	if cfg.Retention.Enabled {
		retentionStore, err = retention.NewStore(retentionConfig)
		if err != nil {
			log.Printf("Warning: Retention store init failed: %v", err)
		} else {
			log.Printf("[Retention] Store initialized: type=%s, mode=%s, ttl=%v",
				cfg.Retention.Store, cfg.Retention.Mode, retentionConfig.TTL)
			defer retentionStore.Close()
		}
	}

	policyEngine := policy.NewEngine()
	policy.RegisterBuiltinPolicies(policyEngine)
	hub := engine.NewHub(policyEngine).WithRetention(retentionStore, retentionConfig)

	ctx, cancel := context.WithCancel(context.Background())

	for _, e := range cfg.Entrypoints {
		switch e.Type {
		case "websocket":
			plugin := ws.New(e.Name, e.Port).WithSessionStore(sessionStore)
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
