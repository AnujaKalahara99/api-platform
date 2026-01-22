package sse

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"

	"mediation-engine/pkg/core"
)

// clientConn tracks an SSE client connection with its identity
type clientConn struct {
	ch       chan []byte
	identity *core.ClientIdentity
}

type SSEEntrypoint struct {
	name     string
	port     string
	clients  map[string]*clientConn // clientID -> connection
	sessions core.SessionStore
	hub      core.IngressHub
	lock     sync.Mutex
}

func New(name, port string) *SSEEntrypoint {
	return &SSEEntrypoint{
		name:    name,
		port:    port,
		clients: make(map[string]*clientConn),
	}
}

// WithSessionStore sets the session store for client tracking
func (s *SSEEntrypoint) WithSessionStore(store core.SessionStore) *SSEEntrypoint {
	s.sessions = store
	return s
}

func (s *SSEEntrypoint) Name() string { return s.name }
func (s *SSEEntrypoint) Type() string { return "sse" }

func (s *SSEEntrypoint) Start(ctx context.Context, hub core.IngressHub) error {
	s.hub = hub
	mux := http.NewServeMux()

	// Downstream (Stream to Browser)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Extract client identity
		identity := core.ExtractClientIdentity(r, "sse", s.name)

		log.Printf("[%s] Client connecting: id=%s remote=%s",
			s.name, identity.ID, identity.RemoteAddr)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set(core.ClientIDHeader, identity.ID)

		// Create session if store is configured
		if s.sessions != nil {
			session := core.NewSession(identity)
			if err := s.sessions.Create(r.Context(), session); err != nil {
				log.Printf("[%s] Session create error: %v", s.name, err)
			}
		}

		clientChan := make(chan []byte, 10)
		client := &clientConn{ch: clientChan, identity: identity}

		s.lock.Lock()
		s.clients[identity.ID] = client
		s.lock.Unlock()

		log.Printf("[%s] Client connected: id=%s total=%d", s.name, identity.ID, len(s.clients))

		// Emit connect lifecycle event
		hub.Publish(core.Event{
			Type:     core.EventTypeConnect,
			SourceID: s.name,
			ClientID: identity.ID,
			Metadata: map[string]string{
				"protocol":    identity.Protocol,
				"remote_addr": identity.RemoteAddr,
			},
		})

		defer s.handleDisconnect(identity.ID)

		fmt.Fprintf(w, "data: connected\n\n")
		w.(http.Flusher).Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case msg := <-clientChan:
				fmt.Fprintf(w, "data: %s\n\n", msg)
				w.(http.Flusher).Flush()
			}
		}
	})

	// Upstream (Browser POST to Engine)
	mux.HandleFunc("/publish", func(w http.ResponseWriter, r *http.Request) {
		identity := core.ExtractClientIdentity(r, "sse", s.name)
		body, _ := io.ReadAll(r.Body)
		hub.Publish(core.Event{
			Type:     core.EventTypeMessage,
			SourceID: s.name,
			ClientID: identity.ID,
			Payload:  body,
			Metadata: map[string]string{"method": "POST"},
		})
		w.WriteHeader(202)
	})

	server := &http.Server{Addr: s.port, Handler: mux}

	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	log.Printf("[%s] SSE listening on %s", s.name, s.port)
	return server.ListenAndServe()
}

func (s *SSEEntrypoint) handleDisconnect(clientID string) {
	s.lock.Lock()
	client, exists := s.clients[clientID]
	if exists {
		delete(s.clients, clientID)
		close(client.ch)
	}
	s.lock.Unlock()

	// Update session state
	if s.sessions != nil {
		s.sessions.UpdateState(context.Background(), clientID, core.SessionStateDisconnected)
	}

	// Emit disconnect lifecycle event
	s.hub.Publish(core.Event{
		Type:     core.EventTypeDisconnect,
		SourceID: s.name,
		ClientID: clientID,
	})

	log.Printf("[%s] Client disconnected: id=%s total=%d", s.name, clientID, len(s.clients))
}

func (s *SSEEntrypoint) SendDownstream(ctx context.Context, evt core.Event) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	// If ClientID specified, send to specific client
	if evt.ClientID != "" {
		if client, exists := s.clients[evt.ClientID]; exists {
			select {
			case client.ch <- evt.Payload:
			default:
				log.Printf("[%s] Client %s buffer full, dropping message", s.name, evt.ClientID)
			}
		}
		return nil
	}

	// Broadcast to all clients
	for _, client := range s.clients {
		select {
		case client.ch <- evt.Payload:
		default:
		}
	}
	return nil
}

// GetConnectedClients returns IDs of all connected clients
func (s *SSEEntrypoint) GetConnectedClients() []string {
	s.lock.Lock()
	defer s.lock.Unlock()
	ids := make([]string, 0, len(s.clients))
	for id := range s.clients {
		ids = append(ids, id)
	}
	return ids
}
