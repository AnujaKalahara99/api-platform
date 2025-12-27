package ws

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"

	v1 "mediation-engine/pkg/mediation/v1"

	"github.com/gorilla/websocket"
)

type WSEntrypoint struct {
	Port     string
	clients  map[*websocket.Conn]bool // Set of active connections
	lock     sync.Mutex
	upgrader websocket.Upgrader
}

func New(port string) *WSEntrypoint {
	return &WSEntrypoint{
		Port:    port,
		clients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true }, // Allow all for demo
		},
	}
}

func (w *WSEntrypoint) Name() string { return "WebSocket-Server" }

// Start acts as the Source: Listens for WS messages and puts them on the Upstream channel
func (w *WSEntrypoint) Start(ctx context.Context, toBackend chan<- v1.Packet) error {
	server := &http.Server{Addr: ":" + w.Port}

	http.HandleFunc("/connect", func(rw http.ResponseWriter, r *http.Request) {
		conn, err := w.upgrader.Upgrade(rw, r, nil)
		if err != nil {
			log.Printf("WS Upgrade Failed: %v", err)
			return
		}

		w.register(conn)
		defer w.unregister(conn)

		// Read Loop (Ingesting data)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}

			// Wrap data in Packet and push to Engine
			select {
			case toBackend <- v1.Packet{
				SourceID: fmt.Sprintf("ws-%s", r.RemoteAddr),
				Payload:  msg,
			}:
			case <-ctx.Done():
				return
			}
		}
	})

	// Handle graceful shutdown of the HTTP server
	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	log.Printf("WebSocket listening on ws://localhost:%s/connect", w.Port)
	return server.ListenAndServe()
}

// SendToClient acts as the Sink: Receives data from Downstream channel and broadcasts to users
func (w *WSEntrypoint) SendToClient(ctx context.Context, pkt v1.Packet) error {
	w.lock.Lock()
	defer w.lock.Unlock()

	// Broadcast to ALL connected clients
	for conn := range w.clients {
		err := conn.WriteMessage(websocket.TextMessage, pkt.Payload)
		if err != nil {
			log.Printf("Write error: %v", err)
			conn.Close()
			delete(w.clients, conn)
		}
	}
	return nil
}

func (w *WSEntrypoint) register(conn *websocket.Conn) {
	w.lock.Lock()
	w.clients[conn] = true
	w.lock.Unlock()
}

func (w *WSEntrypoint) unregister(conn *websocket.Conn) {
	w.lock.Lock()
	delete(w.clients, conn)
	w.lock.Unlock()
	conn.Close()
}
