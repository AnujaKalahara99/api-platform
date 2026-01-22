package ws

import (
	"context"
	"log"
	"net/http"
	"sync"

	"mediation-engine/pkg/core"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// clientConn tracks a WebSocket connection with its identity
type clientConn struct {
	conn     *websocket.Conn
	identity *core.ClientIdentity
}

type WSEntrypoint struct {
	name     string
	port     string
	clients  map[string]*clientConn // clientID -> connection
	sessions core.SessionStore
	hub      core.IngressHub
	lock     sync.Mutex
}

func New(name, port string) *WSEntrypoint {
	return &WSEntrypoint{
		name:    name,
		port:    port,
		clients: make(map[string]*clientConn),
	}
}

// WithSessionStore sets the session store for client tracking
func (w *WSEntrypoint) WithSessionStore(store core.SessionStore) *WSEntrypoint {
	w.sessions = store
	return w
}

func (w *WSEntrypoint) Name() string { return w.name }
func (w *WSEntrypoint) Type() string { return "websocket" }

func (w *WSEntrypoint) Start(ctx context.Context, hub core.IngressHub) error {
	w.hub = hub
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		// Extract client identity
		identity := core.ExtractClientIdentity(r, "websocket", w.name)

		log.Printf("[%s] Client connecting: id=%s remote=%s provided=%s",
			w.name, identity.ID, identity.RemoteAddr, identity.ProvidedID)

		// Upgrade with client ID in response header
		responseHeader := http.Header{
			core.ClientIDHeader: []string{identity.ID},
		}
		conn, err := upgrader.Upgrade(rw, r, responseHeader)
		if err != nil {
			log.Printf("[%s] WebSocket upgrade error: %v", w.name, err)
			return
		}

		// Create session if store is configured
		if w.sessions != nil {
			session := core.NewSession(identity)
			if err := w.sessions.Create(r.Context(), session); err != nil {
				log.Printf("[%s] Session create error: %v", w.name, err)
			}
		}

		// Track client
		client := &clientConn{conn: conn, identity: identity}
		w.lock.Lock()
		w.clients[identity.ID] = client
		w.lock.Unlock()

		log.Printf("[%s] Client connected: id=%s total=%d", w.name, identity.ID, len(w.clients))

		// Emit connect lifecycle event
		hub.Publish(core.Event{
			Type:     core.EventTypeConnect,
			SourceID: w.name,
			ClientID: identity.ID,
			Metadata: map[string]string{
				"protocol":    identity.Protocol,
				"remote_addr": identity.RemoteAddr,
			},
		})

		defer w.handleDisconnect(identity.ID)

		// Read loop
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("[%s] WebSocket read error: %v", w.name, err)
				break
			}

			// Update activity
			if w.sessions != nil {
				w.sessions.UpdateActivity(context.Background(), identity.ID)
			}

			hub.Publish(core.Event{
				Type:     core.EventTypeMessage,
				SourceID: w.name,
				ClientID: identity.ID,
				Payload:  msg,
				Metadata: map[string]string{"type": "websocket"},
			})
		}
	})

	server := &http.Server{Addr: w.port, Handler: mux}

	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	log.Printf("[%s] WebSocket listening on %s", w.name, w.port)
	return server.ListenAndServe()
}

func (w *WSEntrypoint) handleDisconnect(clientID string) {
	w.lock.Lock()
	client, exists := w.clients[clientID]
	if exists {
		delete(w.clients, clientID)
		client.conn.Close()
	}
	w.lock.Unlock()

	// Update session state
	if w.sessions != nil {
		w.sessions.UpdateState(context.Background(), clientID, core.SessionStateDisconnected)
	}

	// Emit disconnect lifecycle event
	w.hub.Publish(core.Event{
		Type:     core.EventTypeDisconnect,
		SourceID: w.name,
		ClientID: clientID,
	})

	log.Printf("[%s] Client disconnected: id=%s total=%d", w.name, clientID, len(w.clients))
}

func (w *WSEntrypoint) SendDownstream(ctx context.Context, evt core.Event) error {
	w.lock.Lock()
	defer w.lock.Unlock()

	// If ClientID specified, send to specific client
	if evt.ClientID != "" {
		if client, exists := w.clients[evt.ClientID]; exists {
			return client.conn.WriteMessage(websocket.TextMessage, evt.Payload)
		}
		log.Printf("[%s] Client not found: %s", w.name, evt.ClientID)
		return nil
	}

	// Broadcast to all clients
	for id, client := range w.clients {
		if err := client.conn.WriteMessage(websocket.TextMessage, evt.Payload); err != nil {
			log.Printf("[%s] Write error for client %s: %v", w.name, id, err)
			delete(w.clients, id)
			client.conn.Close()
		}
	}
	return nil
}

// GetConnectedClients returns IDs of all connected clients
func (w *WSEntrypoint) GetConnectedClients() []string {
	w.lock.Lock()
	defer w.lock.Unlock()
	ids := make([]string, 0, len(w.clients))
	for id := range w.clients {
		ids = append(ids, id)
	}
	return ids
}
