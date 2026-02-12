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

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/internal/logging"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/internal/routing"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/internal/session"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/config"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/plugins"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/plugins/httpget"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/plugins/httppost"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/plugins/kafka"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/plugins/rabbitmq"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/plugins/sse"
	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/plugins/ws"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/etc/mediation/config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("failed to load config", "path", configPath, "error", err)
		os.Exit(1)
	}

	packetLog := logging.NewPacketLogger(logger.With("component", "packet"))
	registry := plugins.NewRegistry(logger)

	registerEntrypoints(cfg, registry, logger, packetLog)
	registerEndpoints(cfg, registry, logger)

	routeTable := routing.NewTable()
	for _, rc := range cfg.Routes {
		routeTable.Add(rc.ToRoute())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := registry.ConnectEndpoints(ctx); err != nil {
		logger.Error("failed to connect endpoints", "error", err)
		os.Exit(1)
	}

	mgr := session.NewManager(routeTable, registry.Endpoints(), logger, packetLog)

	watcher := config.NewWatcher(configPath, routeTable, logger)
	go watcher.Watch(ctx)

	registry.StartEntrypoints(ctx, mgr)

	logger.Info("mediation engine started", "config", configPath)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("shutting down mediation engine")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	mgr.DestroyAll()
	registry.StopAll(shutdownCtx)

	logger.Info("mediation engine stopped")
}

func registerEntrypoints(cfg *config.Config, reg *plugins.Registry, logger *slog.Logger, packetLog *logging.PacketLogger) {
	for _, e := range cfg.Entrypoints {
		switch e.Type {
		case "websocket":
			reg.RegisterEntrypoint(ws.New(e.Name, e.Port, logger, packetLog))
		case "sse":
			reg.RegisterEntrypoint(sse.New(e.Name, e.Port, logger, packetLog))
		case "http_post":
			reg.RegisterEntrypoint(httppost.New(e.Name, e.Port, logger, packetLog))
		case "http_get":
			reg.RegisterEntrypoint(httpget.New(e.Name, e.Port, logger, packetLog))
		default:
			logger.Warn("unknown entrypoint type", "name", e.Name, "type", e.Type)
		}
	}
}

func registerEndpoints(cfg *config.Config, reg *plugins.Registry, logger *slog.Logger) {
	for _, e := range cfg.Endpoints {
		switch e.Type {
		case "kafka":
			brokers := strings.Split(e.Config["brokers"], ",")
			reg.RegisterEndpoint(kafka.New(
				e.Name, brokers,
				e.Config["topic_in"], e.Config["topic_out"],
				e.Config["group_id"],
				logger,
			))
		case "rabbitmq":
			reg.RegisterEndpoint(rabbitmq.New(
				e.Name,
				e.Config["url"],
				e.Config["queue_in"], e.Config["queue_out"],
				logger,
			))
		default:
			logger.Warn("unknown endpoint type", "name", e.Name, "type", e.Type)
		}
	}
}
