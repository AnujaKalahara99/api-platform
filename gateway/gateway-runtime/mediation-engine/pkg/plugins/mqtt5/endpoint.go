// Copyright (c) 2025, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied. See the License for the
// specific language governing permissions and limitations
// under the License.

package mqtt5

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/google/uuid"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

type Endpoint struct {
	name      string
	brokerURL string
	topicIn   string
	topicOut  string
	cm        *autopaho.ConnectionManager
	logger    *slog.Logger
	consumers sync.Map
	router    paho.Router
}

func New(name, brokerURL, topicIn, topicOut string, logger *slog.Logger) *Endpoint {
	return &Endpoint{
		name:      name,
		brokerURL: brokerURL,
		topicIn:   topicIn,
		topicOut:  topicOut,
		logger:    logger,
		router:    paho.NewStandardRouter(),
	}
}

func (e *Endpoint) Name() string { return e.name }
func (e *Endpoint) Type() string { return "mqtt5" }

func (e *Endpoint) Connect(ctx context.Context) error {
	serverURL, err := url.Parse(e.brokerURL)
	if err != nil {
		return fmt.Errorf("mqtt5 invalid URL: %w", err)
	}

	cfg := autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{serverURL},
		KeepAlive:                     30,
		CleanStartOnInitialConnection: true,
		SessionExpiryInterval:         60,
		OnConnectionUp: func(cm *autopaho.ConnectionManager, connAck *paho.Connack) {
			e.logger.Info("mqtt5 connection up", "name", e.name)
		},
		ClientConfig: paho.ClientConfig{
			ClientID: "mediation-" + e.name + "-" + uuid.New().String()[:8],
			Router:   e.router,
		},
	}

	e.cm, err = autopaho.NewConnection(ctx, cfg)
	if err != nil {
		return fmt.Errorf("mqtt5 connection: %w", err)
	}

	if err := e.cm.AwaitConnection(ctx); err != nil {
		return fmt.Errorf("mqtt5 await connection: %w", err)
	}

	e.logger.Info("mqtt5 endpoint connected", "name", e.name, "broker", e.brokerURL)
	return nil
}

func (e *Endpoint) Disconnect(ctx context.Context) error {
	if e.cm != nil {
		return e.cm.Disconnect(ctx)
	}
	return nil
}

func (e *Endpoint) StartConsumer(
	ctx context.Context,
	session *core.Session,
	ch chan<- core.BrokerMessage,
) error {
	if e.topicIn == "" {
		<-ctx.Done()
		return nil
	}

	mqttCh := make(chan *paho.Publish, 1)
	e.consumers.Store(session.ID, mqttCh)

	e.router.RegisterHandler(e.topicIn, func(p *paho.Publish) {
		select {
		case mqttCh <- p:
		default:
		}
	})

	_, err := e.cm.Subscribe(ctx, &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{Topic: e.topicIn, QoS: 1},
		},
	})
	if err != nil {
		e.consumers.Delete(session.ID)
		close(mqttCh)
		return fmt.Errorf("mqtt5 subscribe: %w", err)
	}

	defer func() {
		e.consumers.Delete(session.ID)
		close(mqttCh)
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case pub, ok := <-mqttCh:
			if !ok {
				return nil
			}
			brokerMsg := core.BrokerMessage{
				Event: core.Event{
					ID:        uuid.New().String(),
					SourceID:  e.name,
					ClientID:  session.ClientID,
					Payload:   pub.Payload,
					Metadata:  map[string]string{"mqtt_topic": pub.Topic},
					Timestamp: time.Now().UTC(),
					Type:      core.EventTypeData,
				},
				Ack:  func() error { return nil },
				Nack: func() error { return nil },
			}

			select {
			case ch <- brokerMsg:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func (e *Endpoint) StopConsumer(sessionID string) error {
	e.consumers.Delete(sessionID)
	return nil
}

func (e *Endpoint) Send(ctx context.Context, evt core.Event) error {
	if e.topicOut == "" || e.cm == nil {
		return nil
	}
	_, err := e.cm.Publish(ctx, &paho.Publish{
		Topic:   e.topicOut,
		QoS:     1,
		Payload: evt.Payload,
	})
	return err
}
