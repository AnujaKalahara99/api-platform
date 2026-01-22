package mqtt

import (
	"context"
	"fmt"
	"log"
	"mediation-engine/pkg/core"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MqttEndpoint struct {
	name     string
	broker   string
	topicIn  string
	topicOut string

	// Hybrid approach: 1:N publish, 1:1 consume
	producerPool []mqtt.Client
	poolIndex    int
	poolMutex    sync.Mutex

	// Per-client persistent sessions for consume
	clientSubs map[string]mqtt.Client
	subMutex   sync.RWMutex

	hub core.IngressHub
}

func New(name, broker, topicIn, topicOut string) *MqttEndpoint {
	return &MqttEndpoint{
		name:       name,
		broker:     broker,
		topicIn:    topicIn,
		topicOut:   topicOut,
		clientSubs: make(map[string]mqtt.Client),
	}
}

func (m *MqttEndpoint) Name() string { return m.name }
func (m *MqttEndpoint) Type() string { return "mqtt" }

func (m *MqttEndpoint) Start(ctx context.Context, hub core.IngressHub) error {
	m.hub = hub

	// Initialize shared producer pool (1:N publish path)
	poolSize := 5
	m.producerPool = make([]mqtt.Client, poolSize)
	for i := 0; i < poolSize; i++ {
		opts := mqtt.NewClientOptions().
			AddBroker(m.broker).
			SetClientID(fmt.Sprintf("engine-%s-producer-%d", m.name, i)).
			SetAutoReconnect(true).
			SetCleanSession(true)

		client := mqtt.NewClient(opts)
		if token := client.Connect(); token.Wait() && token.Error() != nil {
			return fmt.Errorf("producer pool connection %d failed: %w", i, token.Error())
		}
		m.producerPool[i] = client
		log.Printf("[MQTT] Producer %d connected to %s", i, m.broker)
	}

	<-ctx.Done()

	// Cleanup
	for i, client := range m.producerPool {
		client.Disconnect(250)
		log.Printf("[MQTT] Producer %d disconnected", i)
	}

	m.subMutex.Lock()
	for clientID, client := range m.clientSubs {
		client.Disconnect(250)
		log.Printf("[MQTT] Client subscription %s disconnected", clientID)
	}
	m.subMutex.Unlock()

	return nil
}

func (m *MqttEndpoint) SendUpstream(ctx context.Context, evt core.Event) error {
	m.poolMutex.Lock()
	producer := m.producerPool[m.poolIndex]
	m.poolIndex = (m.poolIndex + 1) % len(m.producerPool)
	m.poolMutex.Unlock()

	log.Printf("[MQTT] Publishing to topic '%s'. Payload: %s", m.topicIn, string(evt.Payload))

	token := producer.Publish(m.topicIn, 1, false, evt.Payload)
	token.Wait()

	if err := token.Error(); err != nil {
		log.Printf("[MQTT] Failed to publish to '%s': %v", m.topicIn, err)
		return err
	}

	log.Printf("[MQTT] Successfully published to '%s'", m.topicIn)
	return nil
}

// BindClient creates a persistent MQTT session for this client (1:1 - stateful & resilient)
func (m *MqttEndpoint) BindClient(ctx context.Context, clientID string, params map[string]string) error {
	log.Printf("[MQTT] Binding client %s to endpoint %s", clientID, m.name)
	m.subMutex.Lock()
	defer m.subMutex.Unlock()

	if _, exists := m.clientSubs[clientID]; exists {
		log.Printf("[MQTT] Client %s already bound", clientID)
		return nil
	}

	opts := mqtt.NewClientOptions().
		AddBroker(m.broker).
		SetClientID(fmt.Sprintf("engine-%s-client-%s", m.name, clientID)).
		SetAutoReconnect(true).
		SetCleanSession(false) // Persistent session - broker queues messages while disconnected

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("client %s connection failed: %w", clientID, token.Error())
	}

	token := client.Subscribe(m.topicOut, 1, func(c mqtt.Client, msg mqtt.Message) {
		log.Printf("[MQTT] Received message for client %s from topic %s", clientID, msg.Topic())
		m.hub.Publish(core.Event{
			SourceID: m.name,
			ClientID: clientID,
			Payload:  msg.Payload(),
			Metadata: map[string]string{"topic": msg.Topic()},
		})
	})

	if token.Wait() && token.Error() != nil {
		client.Disconnect(250)
		return fmt.Errorf("subscription failed for client %s: %w", clientID, token.Error())
	}

	m.clientSubs[clientID] = client
	log.Printf("[MQTT] Client %s subscribed to %s", clientID, m.topicOut)
	return nil
}

// UnbindClient destroys MQTT client connection (called on disconnect or expiry)
func (m *MqttEndpoint) UnbindClient(ctx context.Context, clientID string) error {
	m.subMutex.Lock()
	defer m.subMutex.Unlock()

	client, exists := m.clientSubs[clientID]
	if !exists {
		return nil
	}

	client.Disconnect(250) // Closes connection, broker deletes subscription
	delete(m.clientSubs, clientID)
	log.Printf("[MQTT] Client %s unbound (connection terminated)", clientID)
	return nil
}

// ResumeClient recreates MQTT connection within reconnection window
func (m *MqttEndpoint) ResumeClient(ctx context.Context, clientID string) error {
	log.Printf("[MQTT] Resuming client %s", clientID)

	// Rebind creates fresh connection + subscription
	return m.BindClient(ctx, clientID, nil)
}
