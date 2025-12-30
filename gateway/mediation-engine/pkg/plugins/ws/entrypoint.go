package ws

import (
	"context"
	"log"
	"mediation-engine/pkg/core"
	"net/http"

	"github.com/gorilla/websocket"
)

type WSEntrypoint struct {
	name     string
	port     string
	clients  map[*websocket.Conn]bool
	upgrader websocket.Upgrader
	hub      core.IngressHub // Reference to the engine
}

func New(name, port string) *WSEntrypoint {
	return &WSEntrypoint{
		name:     name,
		port:     port,
		clients:  make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
	}
}

func (w *WSEntrypoint) Name() string { return w.name }
func (w *WSEntrypoint) Type() string { return "websocket" }

// Start listens for traffic and Pushes to Hub
func (w *WSEntrypoint) Start(ctx context.Context, hub core.IngressHub) error {
	w.hub = hub // Store reference to hub

	http.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		conn, _ := w.upgrader.Upgrade(rw, r, nil)
		w.clients[conn] = true

		defer conn.Close()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("Read error: %v", err)
				break
			}

			// ---------------------------------------------------------
			// LOGGING ADDED HERE
			log.Printf("[%s] Received message: %s", w.name, string(msg))
			// ------

			// CONVERT TO ABSTRACT EVENT
			event := core.Event{
				SourceID: w.name, // "ws-users"
				Payload:  msg,
				Metadata: map[string]string{"client_ip": r.RemoteAddr},
			}

			// PUSH TO ENGINE
			w.hub.Publish(event)
		}
	})

	log.Printf("[%s] WS listening on %s", w.name, w.port)
	return http.ListenAndServe(w.port, nil)
}

// SendDownstream accepts Abstract Event -> Writes to WS
func (w *WSEntrypoint) SendDownstream(ctx context.Context, evt core.Event) error {
	for conn := range w.clients {
		conn.WriteMessage(websocket.TextMessage, evt.Payload)
	}
	return nil
}
