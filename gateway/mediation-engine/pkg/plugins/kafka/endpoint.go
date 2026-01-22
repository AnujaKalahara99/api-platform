package kafka

import (
	"context"
	"fmt"
	"log"
	"sync"

	"mediation-engine/pkg/core"

	"github.com/segmentio/kafka-go"
)

// clientReader tracks a per-client consumer
type clientReader struct {
	reader *kafka.Reader
	cancel context.CancelFunc
}

type KafkaEndpoint struct {
	name     string
	brokers  []string
	topicIn  string
	topicOut string
	writer   *kafka.Writer
	hub      core.IngressHub

	clientReaders map[string]*clientReader // clientID -> reader
	mu            sync.RWMutex
}

func New(name string, brokers []string, topicIn, topicOut string) *KafkaEndpoint {
	return &KafkaEndpoint{
		name:          name,
		brokers:       brokers,
		topicIn:       topicIn,
		topicOut:      topicOut,
		clientReaders: make(map[string]*clientReader),
	}
}

func (k *KafkaEndpoint) Name() string { return k.name }
func (k *KafkaEndpoint) Type() string { return "kafka" }

func (k *KafkaEndpoint) Start(ctx context.Context, hub core.IngressHub) error {
	k.hub = hub

	// Setup Writer
	k.writer = &kafka.Writer{
		Addr:     kafka.TCP(k.brokers...),
		Topic:    k.topicIn,
		Balancer: &kafka.LeastBytes{},
	}

	log.Printf("[%s] Kafka connected to %v", k.name, k.brokers)

	<-ctx.Done()
	k.cleanupAllReaders()
	return k.writer.Close()
}

// SubscribeForClient creates a dedicated consumer for a specific client
func (k *KafkaEndpoint) SubscribeForClient(ctx context.Context, clientID string, opts core.SubscribeOptions) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	// Already subscribed
	if _, exists := k.clientReaders[clientID]; exists {
		log.Printf("[%s] Client %s already has consumer", k.name, clientID)
		return nil
	}

	topic := k.topicOut
	if opts.Topic != "" {
		topic = opts.Topic
	}

	// Create reader with unique consumer group per client
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  k.brokers,
		Topic:    topic,
		GroupID:  fmt.Sprintf("client-%s-%s", k.name, clientID),
		MinBytes: 10e3,
		MaxBytes: 10e6,
	})

	readerCtx, cancel := context.WithCancel(ctx)

	// Start read goroutine for this client
	go k.readLoop(readerCtx, reader, clientID)

	k.clientReaders[clientID] = &clientReader{reader: reader, cancel: cancel}
	log.Printf("[%s] Client %s consumer started on topic %s", k.name, clientID, topic)
	return nil
}

// UnsubscribeClient removes the consumer for a specific client
func (k *KafkaEndpoint) UnsubscribeClient(ctx context.Context, clientID string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	cr, exists := k.clientReaders[clientID]
	if !exists {
		return nil
	}

	cr.cancel()
	cr.reader.Close()
	delete(k.clientReaders, clientID)

	log.Printf("[%s] Client %s consumer stopped", k.name, clientID)
	return nil
}

func (k *KafkaEndpoint) readLoop(ctx context.Context, reader *kafka.Reader, clientID string) {
	defer reader.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			m, err := reader.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return // Context cancelled, normal shutdown
				}
				log.Printf("[%s] Kafka reader error for client %s: %v", k.name, clientID, err)
				return
			}

			log.Printf("[%s] Message for client %s from topic %s, size: %d bytes",
				k.name, clientID, k.topicOut, len(m.Value))

			k.hub.Publish(core.Event{
				Type:     core.EventTypeMessage,
				SourceID: k.name,
				ClientID: clientID, // Route back to specific client
				Payload:  m.Value,
				Metadata: map[string]string{"key": string(m.Key)},
			})
		}
	}
}

func (k *KafkaEndpoint) cleanupAllReaders() {
	k.mu.Lock()
	defer k.mu.Unlock()
	for clientID, cr := range k.clientReaders {
		cr.cancel()
		cr.reader.Close()
		delete(k.clientReaders, clientID)
	}
}

func (k *KafkaEndpoint) SendUpstream(ctx context.Context, evt core.Event) error {
	log.Printf("[%s] Sending to topic %s, key: %s, size: %d bytes",
		k.name, k.topicIn, evt.ID, len(evt.Payload))
	return k.writer.WriteMessages(ctx,
		kafka.Message{
			Key:   []byte(evt.ID),
			Value: evt.Payload,
		},
	)
}
