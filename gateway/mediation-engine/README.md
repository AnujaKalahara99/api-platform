### Folder Structure

```text
mediation-engine/
├── config.yaml                  # Configuration
├── cmd/
│   └── server/
│       └── main.go              # Main wiring
├── internal/
│   ├── engine/
│   │   └── hub.go               # Central Switchboard
│   └── policy/
│       └── default.go           # Default Policy Engine
├── pkg/
│   ├── config/
│   │   └── loader.go            # YAML Parser
│   ├── core/
│   │   ├── interfaces.go        # Abstract Interfaces
│   │   └── types.go             # Abstract Event Data
│   └── plugins/
│       ├── kafka/               # Kafka Endpoint
│       ├── mqtt/                # MQTT Endpoint
│       ├── sse/                 # SSE Entrypoint
│       └── ws/                  # WS Entrypoint
└── go.mod