package mqtt

import (
	"context"
	"log"
	"mediation-engine/pkg/core"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MqttEndpoint struct {
	name     string
	client   mqtt.Client
	topicIn  string
	topicOut string
}

func New(name, broker, topicIn, topicOut string) *MqttEndpoint {
	opts := mqtt.NewClientOptions().AddBroker(broker).SetClientID("engine-" + name)
	opts.SetAutoReconnect(true)
	return &MqttEndpoint{
		name:     name,
		topicIn:  topicIn,
		topicOut: topicOut,
		client:   mqtt.NewClient(opts),
	}
}

func (m *MqttEndpoint) Name() string { return m.name }
func (m *MqttEndpoint) Type() string { return "mqtt" }

func (m *MqttEndpoint) Start(ctx context.Context, hub core.IngressHub) error {
	if token := m.client.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}

	// Subscribe (Downstream -> Engine)
	m.client.Subscribe(m.topicOut, 0, func(client mqtt.Client, msg mqtt.Message) {
		hub.Publish(core.Event{
			SourceID: m.name,
			Payload:  msg.Payload(),
			Metadata: map[string]string{"topic": msg.Topic()},
		})
	})

	// Keep connection alive
	<-ctx.Done()
	m.client.Disconnect(250)
	return nil
}

// func (m *MqttEndpoint) SendUpstream(ctx context.Context, evt core.Event) error {
// 	token := m.client.Publish(m.topicIn, 0, false, evt.Payload)
// 	token.Wait()
// 	return token.Error()
// }

func (m *MqttEndpoint) SendUpstream(ctx context.Context, evt core.Event) error {
	// 1. Log the attempt before sending
	log.Printf("[MQTT] Publishing to topic '%s'. Payload: %s", m.topicIn, string(evt.Payload))

	token := m.client.Publish(m.topicIn, 0, false, evt.Payload)
	token.Wait()

	// 2. Check for errors and log accordingly
	if err := token.Error(); err != nil {
		log.Printf("[MQTT] Failed to publish to '%s': %v", m.topicIn, err)
		return err
	}

	// 3. Log success
	log.Printf("[MQTT] Successfully published to '%s'", m.topicIn)
	return nil
}
