package kafka

import (
	"context"
	"log"
	"mediation-engine/pkg/core"

	"github.com/segmentio/kafka-go"
)

type KafkaEndpoint struct {
	name     string
	brokers  []string
	topicIn  string
	topicOut string
	writer   *kafka.Writer
}

func New(name string, brokers []string, topicIn, topicOut string) *KafkaEndpoint {
	return &KafkaEndpoint{
		name:     name,
		brokers:  brokers,
		topicIn:  topicIn,  // Web -> Kafka (Write)
		topicOut: topicOut, // Kafka -> Web (Read)
	}
}

func (k *KafkaEndpoint) Name() string { return k.name }
func (k *KafkaEndpoint) Type() string { return "kafka" }

func (k *KafkaEndpoint) Start(ctx context.Context, hub core.IngressHub) error {
	// 1. Setup Writer
	k.writer = &kafka.Writer{
		Addr:     kafka.TCP(k.brokers...),
		Topic:    k.topicIn,
		Balancer: &kafka.LeastBytes{},
	}

	// 2. Setup Reader
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  k.brokers,
		Topic:    k.topicOut,
		GroupID:  "mediation-group-" + k.name,
		MinBytes: 10e3,
		MaxBytes: 10e6,
	})

	log.Printf("[%s] Kafka connected to %v", k.name, k.brokers)

	// 3. Read Loop
	go func() {
		defer reader.Close()
		for {
			m, err := reader.ReadMessage(ctx)
			if err != nil {
				break
			}

			// Push to Engine
			hub.Publish(core.Event{
				SourceID: k.name,
				Payload:  m.Value,
				Metadata: map[string]string{"key": string(m.Key)},
			})
		}
	}()

	<-ctx.Done()
	return k.writer.Close()
}

func (k *KafkaEndpoint) SendUpstream(ctx context.Context, evt core.Event) error {
	// Write to Kafka Topic
	return k.writer.WriteMessages(ctx,
		kafka.Message{
			Key:   []byte(evt.ID),
			Value: evt.Payload,
		},
	)
}
