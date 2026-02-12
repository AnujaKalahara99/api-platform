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

package rabbitmq

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

type Endpoint struct {
	name      string
	url       string
	queueIn   string
	queueOut  string
	conn      *amqp.Connection
	pubCh     *amqp.Channel
	logger    *slog.Logger
	consumers sync.Map
}

func New(name, url, queueIn, queueOut string, logger *slog.Logger) *Endpoint {
	return &Endpoint{
		name:     name,
		url:      url,
		queueIn:  queueIn,
		queueOut: queueOut,
		logger:   logger,
	}
}

func (e *Endpoint) Name() string { return e.name }
func (e *Endpoint) Type() string { return "rabbitmq" }

func (e *Endpoint) Connect(ctx context.Context) error {
	var err error
	e.conn, err = amqp.Dial(e.url)
	if err != nil {
		return fmt.Errorf("rabbitmq dial: %w", err)
	}

	e.pubCh, err = e.conn.Channel()
	if err != nil {
		return fmt.Errorf("rabbitmq publish channel: %w", err)
	}

	for _, q := range []string{e.queueIn, e.queueOut} {
		if q != "" {
			_, err := e.pubCh.QueueDeclare(q, true, false, false, false, nil)
			if err != nil {
				return fmt.Errorf("rabbitmq queue declare %s: %w", q, err)
			}
		}
	}

	e.logger.Info("rabbitmq endpoint connected", "name", e.name, "url", e.url)
	return nil
}

func (e *Endpoint) Disconnect(ctx context.Context) error {
	e.consumers.Range(func(key, val any) bool {
		ch := val.(*amqp.Channel)
		ch.Close()
		return true
	})
	if e.pubCh != nil {
		e.pubCh.Close()
	}
	if e.conn != nil {
		return e.conn.Close()
	}
	return nil
}

func (e *Endpoint) StartConsumer(
	ctx context.Context,
	session *core.Session,
	ch chan<- core.BrokerMessage,
) error {
	if e.queueIn == "" {
		<-ctx.Done()
		return nil
	}

	consumerCh, err := e.conn.Channel()
	if err != nil {
		return fmt.Errorf("rabbitmq consumer channel: %w", err)
	}

	if err := consumerCh.Qos(1, 0, false); err != nil {
		consumerCh.Close()
		return fmt.Errorf("rabbitmq qos: %w", err)
	}

	consumerTag := fmt.Sprintf("mediation-%s-%s", e.name, session.ID)
	deliveries, err := consumerCh.Consume(
		e.queueIn,
		consumerTag,
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		consumerCh.Close()
		return fmt.Errorf("rabbitmq consume: %w", err)
	}

	e.consumers.Store(session.ID, consumerCh)
	defer func() {
		e.consumers.Delete(session.ID)
		consumerCh.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-deliveries:
			if !ok {
				return nil
			}
			delivery := d
			brokerMsg := core.BrokerMessage{
				Event: core.Event{
					ID:        uuid.New().String(),
					SourceID:  e.name,
					ClientID:  session.ClientID,
					Payload:   delivery.Body,
					Metadata:  map[string]string{"rabbitmq_routing_key": delivery.RoutingKey},
					Timestamp: time.Now().UTC(),
					Type:      core.EventTypeData,
				},
				Ack:  func() error { return delivery.Ack(false) },
				Nack: func() error { return delivery.Nack(false, true) },
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
	val, ok := e.consumers.LoadAndDelete(sessionID)
	if !ok {
		return nil
	}
	return val.(*amqp.Channel).Close()
}

func (e *Endpoint) Send(ctx context.Context, evt core.Event) error {
	if e.queueOut == "" || e.pubCh == nil {
		return nil
	}
	return e.pubCh.PublishWithContext(ctx,
		"",
		e.queueOut,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/octet-stream",
			Body:        evt.Payload,
			MessageId:   evt.ID,
			Timestamp:   evt.Timestamp,
		},
	)
}
