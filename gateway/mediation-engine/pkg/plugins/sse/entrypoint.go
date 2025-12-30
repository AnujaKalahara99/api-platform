package sse

import (
	"context"
	"fmt"
	"io"
	"log"
	"mediation-engine/pkg/core"
	"net/http"
	"sync"
)

type SSEEntrypoint struct {
	name    string
	port    string
	clients map[chan []byte]bool
	lock    sync.Mutex
}

func New(name, port string) *SSEEntrypoint {
	return &SSEEntrypoint{
		name:    name,
		port:    port,
		clients: make(map[chan []byte]bool),
	}
}

func (s *SSEEntrypoint) Name() string { return s.name }
func (s *SSEEntrypoint) Type() string { return "sse" }

func (s *SSEEntrypoint) Start(ctx context.Context, hub core.IngressHub) error {
	mux := http.NewServeMux()

	// 1. Downstream (Stream to Browser)
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		clientChan := make(chan []byte, 10)
		s.lock.Lock()
		s.clients[clientChan] = true
		s.lock.Unlock()

		defer func() {
			s.lock.Lock()
			delete(s.clients, clientChan)
			s.lock.Unlock()
			close(clientChan)
		}()

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

	// 2. Upstream (Browser POST to Engine)
	mux.HandleFunc("/publish", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hub.Publish(core.Event{
			SourceID: s.name,
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

	log.Printf("[%s] SSE listening on :%s", s.name, s.port)
	return server.ListenAndServe()
}

func (s *SSEEntrypoint) SendDownstream(ctx context.Context, evt core.Event) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	for ch := range s.clients {
		select {
		case ch <- evt.Payload:
		default:
		}
	}
	return nil
}
