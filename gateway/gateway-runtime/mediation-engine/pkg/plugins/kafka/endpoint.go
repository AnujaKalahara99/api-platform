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

package kafka

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

type Endpoint struct {
	name      string
	brokers   []string
	topicIn   string
	topicOut  string
	groupID   string
	writer    *kafka.Writer
	logger    *slog.Logger
	consumers sync.Map
}

func New(name string, brokers []string, topicIn, topicOut, groupID string, logger *slog.Logger) *Endpoint {
	return &Endpoint{
		name:     name,
		brokers:  brokers,
		topicIn:  topicIn,
		topicOut: topicOut,
		groupID:  groupID,
		logger:   logger,
	}
}

func (e *Endpoint) Name() string { return e.name }
func (e *Endpoint) Type() string { return "kafka" }

func (e *Endpoint) Connect(ctx context.Context) error {
	if e.topicOut != "" {
		e.writer = &kafka.Writer{
			Addr:     kafka.TCP(e.brokers...),
			Topic:    e.topicOut,
			Balancer: &kafka.LeastBytes{},
		}
	}
	e.logger.Info("kafka endpoint connected",
		"name", e.name,
		"brokers", strings.Join(e.brokers, ","),
		"topic_in", e.topicIn,
		"topic_out", e.topicOut,
	)
	return nil
}

func (e *Endpoint) Disconnect(ctx context.Context) error {
	e.consumers.Range(func(key, val any) bool {
		reader := val.(*kafka.Reader)
		reader.Close()
		return true
	})
	if e.writer != nil {
		return e.writer.Close()
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

	groupID := e.groupID
	if groupID == "" {
		groupID = "mediation-" + e.name
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  e.brokers,
		Topic:    e.topicIn,
		GroupID:  groupID + "-" + session.ID,
		MaxWait:  500 * time.Millisecond,
		MinBytes: 1,
		MaxBytes: 10e6,
	})

	e.consumers.Store(session.ID, reader)
	defer func() {
		e.consumers.Delete(session.ID)
		reader.Close()
	}()

	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			e.logger.Error("kafka fetch error", "session_id", session.ID, "error", err)
			return err
		}

		brokerMsg := core.BrokerMessage{
			Event: core.Event{
				ID:        uuid.New().String(),
				SourceID:  e.name,
				ClientID:  session.ClientID,
				Payload:   msg.Value,
				Metadata:  map[string]string{"kafka_key": string(msg.Key), "kafka_topic": msg.Topic},
				Timestamp: msg.Time,
				Type:      core.EventTypeData,
			},
			Ack: func() error {
				return reader.CommitMessages(ctx, msg)
			},
			Nack: func() error {
				return nil
			},
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
	return val.(*kafka.Reader).Close()
}

func (e *Endpoint) Send(ctx context.Context, evt core.Event) error {
	if e.writer == nil {
		return nil
	}
	return e.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(evt.ID),
		Value: evt.Payload,
	})
}
