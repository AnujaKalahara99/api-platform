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

package core

import "time"

type DeliveryGuarantee int

const (
	DeliveryAuto DeliveryGuarantee = iota
	DeliveryNone
	DeliveryAtMostOnce
	DeliveryAtLeastOnce
)

type EventType int

const (
	EventTypeData EventType = iota
	EventTypeConnect
	EventTypeDisconnect
)

type Direction int

const (
	DirectionUpstream Direction = iota
	DirectionDownstream
)

type Event struct {
	ID        string            `json:"id"`
	SourceID  string            `json:"source_id"`
	ClientID  string            `json:"client_id"`
	Payload   []byte            `json:"payload"`
	Metadata  map[string]string `json:"metadata"`
	Timestamp time.Time         `json:"timestamp"`
	Type      EventType         `json:"type"`
}

func (e Event) IsLifecycle() bool {
	return e.Type == EventTypeConnect || e.Type == EventTypeDisconnect
}

type BrokerMessage struct {
	Event Event
	Ack   func() error
	Nack  func() error
}

type Route struct {
	Source            string            `yaml:"source"`
	Target            string            `yaml:"target"`
	Direction         Direction         `yaml:"direction"`
	DeliveryGuarantee DeliveryGuarantee `yaml:"delivery_guarantee"`
	ChannelSize       int               `yaml:"channel_size"`
}
