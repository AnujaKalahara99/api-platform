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

package logging

import (
	"log/slog"

	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

type PacketLogger struct {
	logger *slog.Logger
}

func NewPacketLogger(logger *slog.Logger) *PacketLogger {
	return &PacketLogger{logger: logger}
}

func (p *PacketLogger) Log(evt core.Event, route *core.Route, direction string) {
	p.logger.Info("packet",
		"event_id", evt.ID,
		"source_id", evt.SourceID,
		"client_id", evt.ClientID,
		"direction", direction,
		"route_source", route.Source,
		"route_target", route.Target,
		"payload_size", len(evt.Payload),
		"timestamp", evt.Timestamp,
	)
}
