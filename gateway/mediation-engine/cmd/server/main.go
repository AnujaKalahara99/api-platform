package main

import (
	"context"
	"log"
	"mediation-engine/internal/engine"
	"mediation-engine/pkg/plugins/mqtt"
	"mediation-engine/pkg/plugins/ws"
	"os"
	"os/signal"
)

func main() {
	log.Println("Initializing Mediation Engine... hoooooo")

	// 1. Configure Plugins

	// ENTRYPOINT: WebSocket on port 8080
	// Clients connect to: ws://localhost:8080/connect
	wsEntry := ws.New("8066")

	// ENDPOINT: Public MQTT Broker (Eclipse Mosquitto)
	// - Reads from 'test/topic/sensor' (simulates backend data)
	// - Writes to 'test/topic/commands' (simulates client commands)
	mqttEnd := mqtt.New("mqtt://broker.hivemq.com:1883", "test/pub", "test/sub")

	// 2. Configure Router
	router := engine.NewRouter(wsEntry, mqttEnd)

	// 3. Start Engine
	ctx, cancel := context.WithCancel(context.Background())

	// Run the router (non-blocking here, but Router.Start blocks)
	go router.Start(ctx)

	// 4. Wait for Ctrl+C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c // Block until signal received

	log.Println("Stopping Engine...")
	cancel() // Cancel context to stop all plugins
}
