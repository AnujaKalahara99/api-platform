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

package solace

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
	"solace.dev/go/messaging"
	"solace.dev/go/messaging/pkg/solace"
	"solace.dev/go/messaging/pkg/solace/config"
	"solace.dev/go/messaging/pkg/solace/message"
	"solace.dev/go/messaging/pkg/solace/resource"
)

type Endpoint struct {
	name      string
	host      string
	vpn       string
	username  string
	password  string
	topicIn   string
	topicOut  string
	service   solace.MessagingService
	logger    *slog.Logger
	consumers sync.Map
}

func New(name, host, vpn, username, password, topicIn, topicOut string, logger *slog.Logger) *Endpoint {
	return &Endpoint{
		name:     name,
		host:     host,
		vpn:      vpn,
		username: username,
		password: password,
		topicIn:  topicIn,
		topicOut: topicOut,
		logger:   logger,
	}
}

func (e *Endpoint) Name() string { return e.name }
func (e *Endpoint) Type() string { return "solace" }

func (e *Endpoint) Connect(ctx context.Context) error {
	var err error
	e.service, err = messaging.NewMessagingServiceBuilder().
		FromConfigurationProvider(config.ServicePropertyMap{
			config.TransportLayerPropertyHost:                e.host,
			config.ServicePropertyVPNName:                    e.vpn,
			config.AuthenticationPropertySchemeBasicUserName: e.username,
			config.AuthenticationPropertySchemeBasicPassword: e.password,
		}).Build()
	if err != nil {
		return fmt.Errorf("solace build: %w", err)
	}
	if err = e.service.Connect(); err != nil {
		return fmt.Errorf("solace connect: %w", err)
	}
	e.logger.Info("solace endpoint connected", "name", e.name, "host", e.host)
	return nil
}

func (e *Endpoint) Disconnect(ctx context.Context) error {
	if e.service != nil {
		e.service.Disconnect()
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

	receiver, err := e.service.CreateDirectMessageReceiverBuilder().
		WithSubscriptions(resource.TopicSubscriptionOf(e.topicIn)).
		Build()
	if err != nil {
		return fmt.Errorf("solace receiver build: %w", err)
	}
	if err = receiver.Start(); err != nil {
		return fmt.Errorf("solace receiver start: %w", err)
	}

	e.consumers.Store(session.ID, struct{}{})
	defer func() {
		e.consumers.Delete(session.ID)
		receiver.Terminate(5 * time.Second)
	}()

	msgCh := receiver.ReceiveAsync(func(inMsg message.InboundMessage) {
		payload, _ := inMsg.GetPayloadAsBytes()
		brokerMsg := core.BrokerMessage{
			Event: core.Event{
				ID:        uuid.New().String(),
				SourceID:  e.name,
				ClientID:  session.ClientID,
				Payload:   payload,
				Metadata:  map[string]string{"solace_topic": e.topicIn},
				Timestamp: time.Now().UTC(),
				Type:      core.EventTypeData,
			},
			Ack:  func() error { return nil },
			Nack: func() error { return nil },
		}

		select {
		case ch <- brokerMsg:
		case <-ctx.Done():
		}
	})
	_ = msgCh

	<-ctx.Done()
	return nil
}

func (e *Endpoint) StopConsumer(sessionID string) error {
	e.consumers.Delete(sessionID)
	return nil
}

func (e *Endpoint) Send(ctx context.Context, evt core.Event) error {
	if e.topicOut == "" || e.service == nil {
		return nil
	}
	publisher, err := e.service.CreateDirectMessagePublisherBuilder().Build()
	if err != nil {
		return err
	}
	if err = publisher.Start(); err != nil {
		return err
	}
	defer publisher.Terminate(5 * time.Second)

	msg, err := e.service.MessageBuilder().BuildWithByteArrayPayload(evt.Payload)
	if err != nil {
		return err
	}
	return publisher.Publish(msg, resource.TopicOf(e.topicOut))
}
