package mqtt

import (
	"context"
	"fmt"
	"time"

	v1 "mediation-engine/pkg/mediation/v1"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MQTTEndpoint struct {
	BrokerURL string
	TopicPub  string // Topic to publish data FROM Web
	TopicSub  string // Topic to listen for data TO Web
	client    mqtt.Client
}

func New(broker, pubTopic, subTopic string) *MQTTEndpoint {
	opts := mqtt.NewClientOptions().AddBroker(broker).SetClientID("mediation-engine-" + fmt.Sprint(time.Now().Unix()))
	opts.SetAutoReconnect(true)

	return &MQTTEndpoint{
		BrokerURL: broker,
		TopicPub:  pubTopic,
		TopicSub:  subTopic,
		client:    mqtt.NewClient(opts),
	}
}

func (m *MQTTEndpoint) Name() string { return "MQTT5-Adapter" }

// Start connects to MQTT and pipes subscribed messages to the Downstream channel
func (m *MQTTEndpoint) Start(ctx context.Context, fromBackend chan<- v1.Packet) error {
	if token := m.client.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("MQTT Connection Failed: %v", token.Error())
	}

	// Subscribe (Pull Logic)
	token := m.client.Subscribe(m.TopicSub, 0, func(client mqtt.Client, msg mqtt.Message) {
		// Convert MQTT Msg -> Internal Packet
		select {
		case fromBackend <- v1.Packet{
			SourceID: "mqtt-broker",
			Payload:  msg.Payload(),
			Metadata: map[string]string{"topic": msg.Topic()},
		}:
		case <-ctx.Done():
		}
	})

	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("MQTT Subscribe Failed: %v", token.Error())
	}

	// Block here until context dies to keep connection alive in this goroutine
	<-ctx.Done()
	m.client.Disconnect(250)
	return nil
}

// Publish takes data from Upstream channel and pushes to MQTT
func (m *MQTTEndpoint) Publish(ctx context.Context, pkt v1.Packet) error {
	token := m.client.Publish(m.TopicPub, 0, false, pkt.Payload)
	token.Wait()
	return token.Error()
}
