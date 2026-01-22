package mqtt

import (
	"context"
	"fmt"
	"log"
	"sync"

	"mediation-engine/pkg/core"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// clientSession tracks a per-client MQTT connection
type clientSession struct {
	clientID string
	client   mqtt.Client
	topic    string
	qos      byte
	cancel   context.CancelFunc
	ctx      context.Context // Store the parent context for resubscription
}

type MqttEndpoint struct {
	name     string
	broker   string
	topicIn  string
	topicOut string
	hub      core.IngressHub

	clientSessions map[string]*clientSession // clientID -> dedicated MQTT client
	mu             sync.RWMutex
}

func New(name, broker, topicIn, topicOut string) *MqttEndpoint {
	return &MqttEndpoint{
		name:           name,
		broker:         broker,
		topicIn:        topicIn,
		topicOut:       topicOut,
		clientSessions: make(map[string]*clientSession),
	}
}

func (m *MqttEndpoint) Name() string { return m.name }
func (m *MqttEndpoint) Type() string { return "mqtt" }

func (m *MqttEndpoint) Start(ctx context.Context, hub core.IngressHub) error {
	m.hub = hub
	log.Printf("[%s] MQTT endpoint initialized for broker %s", m.name, m.broker)

	<-ctx.Done()
	m.cleanupAllSessions()
	return nil
}

// SubscribeForClient creates a dedicated MQTT client with persistent session for each user
func (m *MqttEndpoint) SubscribeForClient(ctx context.Context, clientID string, opts core.SubscribeOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	topic := m.topicOut
	if opts.Topic != "" {
		topic = opts.Topic
	}

	qos := opts.QoS
	if qos == 0 {
		qos = 1 // Default to QoS 1 for delivery guarantees
	}

	// Check if session exists
	if session, exists := m.clientSessions[clientID]; exists {
		if session.client.IsConnected() {
			log.Printf("[%s] Client %s already connected", m.name, clientID)
			return nil
		}
		// Cancel old context and create new one for reconnection
		if session.cancel != nil {
			session.cancel()
		}
		return m.reconnectAndResubscribe(ctx, session, topic, byte(qos))
	}

	return m.createNewSession(ctx, clientID, topic, byte(qos))
}

// createNewSession creates a brand new MQTT client and subscription
func (m *MqttEndpoint) createNewSession(ctx context.Context, clientID, topic string, qos byte) error {
	subCtx, cancel := context.WithCancel(ctx)

	// Create dedicated MQTT client for this user
	mqttOpts := mqtt.NewClientOptions().
		AddBroker(m.broker).
		SetClientID(clientID).  // Unique client ID = user identity for broker
		SetCleanSession(false). // CRITICAL: Persist session across disconnects
		SetAutoReconnect(true). // Enable auto-reconnect
		SetOnConnectHandler(func(client mqtt.Client) {
			log.Printf("[%s] Client %s connected/reconnected to broker", m.name, clientID)
			// Resubscribe on reconnect - MQTT library will call this on auto-reconnect
			m.resubscribeOnConnect(clientID, topic, qos)
		}).
		SetConnectionLostHandler(func(client mqtt.Client, err error) {
			log.Printf("[%s] Client %s connection lost: %v", m.name, clientID, err)
		})

	mqttClient := mqtt.NewClient(mqttOpts)

	// Connect to broker
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		cancel()
		return fmt.Errorf("connect failed for %s: %w", clientID, token.Error())
	}

	// Store session first (needed for resubscribeOnConnect)
	m.clientSessions[clientID] = &clientSession{
		clientID: clientID,
		client:   mqttClient,
		topic:    topic,
		qos:      qos,
		cancel:   cancel,
		ctx:      ctx,
	}

	// Subscribe with QoS
	if err := m.subscribeWithHandler(subCtx, mqttClient, clientID, topic, qos); err != nil {
		delete(m.clientSessions, clientID)
		cancel()
		mqttClient.Disconnect(250)
		return err
	}

	log.Printf("[%s] Client %s connected with persistent session, subscribed to %s (QoS %d)",
		m.name, clientID, topic, qos)
	return nil
}

// reconnectAndResubscribe handles reconnection of an existing session
func (m *MqttEndpoint) reconnectAndResubscribe(ctx context.Context, session *clientSession, topic string, qos byte) error {
	// Create new context for the reconnected session
	subCtx, cancel := context.WithCancel(ctx)
	session.cancel = cancel
	session.ctx = ctx
	session.topic = topic
	session.qos = qos

	// Reconnect
	if token := session.client.Connect(); token.Wait() && token.Error() != nil {
		cancel()
		return fmt.Errorf("reconnect failed for %s: %w", session.clientID, token.Error())
	}

	// Resubscribe with fresh context
	if err := m.subscribeWithHandler(subCtx, session.client, session.clientID, topic, qos); err != nil {
		cancel()
		return err
	}

	log.Printf("[%s] Client %s reconnected and resubscribed to %s (QoS %d)",
		m.name, session.clientID, topic, qos)
	return nil
}

// subscribeWithHandler creates subscription with proper message handler
func (m *MqttEndpoint) subscribeWithHandler(ctx context.Context, client mqtt.Client, clientID, topic string, qos byte) error {
	handler := func(c mqtt.Client, msg mqtt.Message) {
		select {
		case <-ctx.Done():
			return
		default:
			log.Printf("[%s] Received message for client %s on topic %s: %s",
				m.name, clientID, msg.Topic(), string(msg.Payload()))
			m.hub.Publish(core.Event{
				Type:     core.EventTypeMessage,
				SourceID: m.name,
				ClientID: clientID,
				Payload:  msg.Payload(),
				Metadata: map[string]string{"topic": msg.Topic()},
			})
		}
	}

	token := client.Subscribe(topic, qos, handler)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("subscribe failed for %s: %w", clientID, token.Error())
	}

	return nil
}

// resubscribeOnConnect is called by the MQTT library's OnConnect handler during auto-reconnect
func (m *MqttEndpoint) resubscribeOnConnect(clientID, topic string, qos byte) {
	m.mu.RLock()
	session, exists := m.clientSessions[clientID]
	m.mu.RUnlock()

	if !exists {
		log.Printf("[%s] Session not found for client %s during resubscribe", m.name, clientID)
		return
	}

	// Create new context for the handler
	m.mu.Lock()
	if session.cancel != nil {
		session.cancel()
	}
	subCtx, cancel := context.WithCancel(session.ctx)
	session.cancel = cancel
	m.mu.Unlock()

	if err := m.subscribeWithHandler(subCtx, session.client, clientID, topic, qos); err != nil {
		log.Printf("[%s] Failed to resubscribe client %s: %v", m.name, clientID, err)
	} else {
		log.Printf("[%s] Client %s resubscribed to %s after reconnect", m.name, clientID, topic)
	}
}

// UnsubscribeClient disconnects without unsubscribing - broker will queue messages
func (m *MqttEndpoint) UnsubscribeClient(ctx context.Context, clientID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.clientSessions[clientID]
	if !exists {
		return nil
	}

	session.cancel()

	// CRITICAL: Only disconnect, DO NOT unsubscribe
	// This keeps the subscription active at the broker for message queuing
	session.client.Disconnect(250)

	// Keep session in map for potential reconnection
	// Remove only on explicit cleanup or after timeout
	log.Printf("[%s] Client %s disconnected (broker will queue messages for reconnection)",
		m.name, clientID)
	return nil
}

func (m *MqttEndpoint) cleanupAllSessions() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for clientID, session := range m.clientSessions {
		session.cancel()
		session.client.Disconnect(250)
		delete(m.clientSessions, clientID)
		log.Printf("[%s] Cleaned up session for %s", m.name, clientID)
	}
}

func (m *MqttEndpoint) SendUpstream(ctx context.Context, evt core.Event) error {
	m.mu.RLock()
	session, exists := m.clientSessions[evt.ClientID]
	m.mu.RUnlock()

	if !exists || !session.client.IsConnected() {
		return fmt.Errorf("client %s not connected", evt.ClientID)
	}

	log.Printf("[MQTT] Client %s publishing to topic '%s'. Payload: %s",
		evt.ClientID, m.topicIn, string(evt.Payload))

	token := session.client.Publish(m.topicIn, 2, false, evt.Payload) // QoS 2
	token.Wait()

	if err := token.Error(); err != nil {
		log.Printf("[MQTT] Failed to publish from client %s to '%s': %v",
			evt.ClientID, m.topicIn, err)
		return err
	}

	log.Printf("[MQTT] Client %s successfully published to '%s'", evt.ClientID, m.topicIn)
	return nil
}
