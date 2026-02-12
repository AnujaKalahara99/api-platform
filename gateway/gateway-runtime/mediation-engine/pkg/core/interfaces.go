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

import "context"

type Entrypoint interface {
	Name() string
	Type() string
	Start(ctx context.Context, manager SessionManager) error
	Stop(ctx context.Context) error
}

type Endpoint interface {
	Name() string
	Type() string
	StartConsumer(ctx context.Context, session *Session, ch chan<- BrokerMessage) error
	StopConsumer(sessionID string) error
	Send(ctx context.Context, evt Event) error
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
}

type SessionManager interface {
	CreateSession(ctx context.Context, entrypointName string, clientID string) (*Session, error)
	DestroySession(sessionID string) error
}

type Session struct {
	ID             string
	ClientID       string
	EntrypointName string
	Route          *Route
	Downstream     chan BrokerMessage
	Upstream       chan Event
	Cancel         context.CancelFunc
}
