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

package jms

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Azure/go-amqp"
	"github.com/google/uuid"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

type Endpoint struct {
	name      string
	url       string
	queueIn   string
	queueOut  string
	conn      *amqp.Conn
	sendSess  *amqp.Session
	sender    *amqp.Sender
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
func (e *Endpoint) Type() string { return "jms" }

func (e *Endpoint) Connect(ctx context.Context) error {
	var err error
	e.conn, err = amqp.Dial(ctx, e.url, nil)
	if err != nil {
		return fmt.Errorf("jms dial: %w", err)
	}

	if e.queueOut != "" {
		e.sendSess, err = e.conn.NewSession(ctx, nil)
		if err != nil {
			return fmt.Errorf("jms send session: %w", err)
		}
		e.sender, err = e.sendSess.NewSender(ctx, e.queueOut, nil)
		if err != nil {
			return fmt.Errorf("jms sender: %w", err)
		}
	}

	e.logger.Info("jms endpoint connected", "name", e.name, "url", e.url)
	return nil
}

func (e *Endpoint) Disconnect(ctx context.Context) error {
	e.consumers.Range(func(key, val any) bool {
		receiver := val.(*amqp.Receiver)
		receiver.Close(ctx)
		return true
	})
	if e.sender != nil {
		e.sender.Close(ctx)
	}
	if e.sendSess != nil {
		e.sendSess.Close(ctx)
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

	recvSess, err := e.conn.NewSession(ctx, nil)
	if err != nil {
		return fmt.Errorf("jms consumer session: %w", err)
	}

	receiver, err := recvSess.NewReceiver(ctx, e.queueIn, &amqp.ReceiverOptions{
		Credit: 1,
	})
	if err != nil {
		recvSess.Close(ctx)
		return fmt.Errorf("jms receiver: %w", err)
	}

	e.consumers.Store(session.ID, receiver)
	defer func() {
		e.consumers.Delete(session.ID)
		receiver.Close(ctx)
		recvSess.Close(ctx)
	}()

	for {
		msg, err := receiver.Receive(ctx, nil)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("jms receive: %w", err)
		}

		var payload []byte
		if len(msg.GetData()) > 0 {
			payload = msg.GetData()
		}

		amqpMsg := msg
		brokerMsg := core.BrokerMessage{
			Event: core.Event{
				ID:        uuid.New().String(),
				SourceID:  e.name,
				ClientID:  session.ClientID,
				Payload:   payload,
				Metadata:  map[string]string{"jms_queue": e.queueIn},
				Timestamp: time.Now().UTC(),
				Type:      core.EventTypeData,
			},
			Ack:  func() error { return receiver.AcceptMessage(ctx, amqpMsg) },
			Nack: func() error { return receiver.RejectMessage(ctx, amqpMsg, nil) },
		}

		select {
		case ch <- brokerMsg:
		case <-ctx.Done():
			return nil
		}
	}
}

func (e *Endpoint) StopConsumer(sessionID string) error {
	val, ok := e.consumers.LoadAndDelete(sessionID)
	if !ok {
		return nil
	}
	return val.(*amqp.Receiver).Close(context.Background())
}

func (e *Endpoint) Send(ctx context.Context, evt core.Event) error {
	if e.sender == nil {
		return nil
	}
	return e.sender.Send(ctx, &amqp.Message{
		Data: [][]byte{evt.Payload},
		Properties: &amqp.MessageProperties{
			MessageID: evt.ID,
		},
	}, nil)
}
