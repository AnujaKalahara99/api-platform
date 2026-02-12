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

package config

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/internal/routing"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

type Watcher struct {
	path     string
	table    *routing.Table
	interval time.Duration
	logger   *slog.Logger
	lastMod  time.Time
}

func NewWatcher(path string, table *routing.Table, logger *slog.Logger) *Watcher {
	return &Watcher{
		path:     path,
		table:    table,
		interval: 5 * time.Second,
		logger:   logger,
	}
}

func (w *Watcher) Watch(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info, err := os.Stat(w.path)
			if err != nil {
				w.logger.Warn("config stat failed", "path", w.path, "error", err)
				continue
			}

			if !info.ModTime().After(w.lastMod) {
				continue
			}

			w.lastMod = info.ModTime()

			cfg, err := Load(w.path)
			if err != nil {
				w.logger.Error("config reload failed", "path", w.path, "error", err)
				continue
			}

			routes := make([]*core.Route, 0, len(cfg.Routes))
			for _, rc := range cfg.Routes {
				routes = append(routes, rc.ToRoute())
			}
			w.table.ReplaceAll(routes)
			w.logger.Info("routes reloaded", "count", len(routes))
		}
	}
}
