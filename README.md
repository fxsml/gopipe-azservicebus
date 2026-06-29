# gopipe-azservicebus

[![Go Reference](https://pkg.go.dev/badge/github.com/fxsml/gopipe-azservicebus.svg)](https://pkg.go.dev/github.com/fxsml/gopipe-azservicebus)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**Azure Service Bus adapter for [gopipe](https://github.com/fxsml/gopipe)** — publish and subscribe with batching, backpressure, lock renewal, and OpenTelemetry metrics.

## Quick Start

```go
import (
    "github.com/fxsml/gopipe-azservicebus"
    "github.com/fxsml/gopipe/message"
)

client, _ := azservicebus.NewClient("Endpoint=sb://...")
```

### Publish

```go
pub := azservicebus.NewPublisher(client, "my-topic", azservicebus.PublisherConfig{})

// Stream from a channel — batches automatically
pub.Publish(ctx, messages)

// Or send a single batch
pub.PublishBatch(ctx, batch)
```

### Subscribe

```go
sub := azservicebus.NewSubscriber(client, "my-topic", "my-subscription", azservicebus.SubscriberConfig{
    MaxInFlight: 50,
})

ch, _ := sub.Subscribe(ctx)
for msg := range ch {
    // process msg
    msg.Ack()
}
```

## Features

| Feature | Details |
|---------|---------|
| Batching | Configurable batch size and flush timeout |
| Backpressure | Semaphore-based in-flight limiting |
| Lock renewal | Auto-detected from broker; configurable override |
| Reconnection | Automatic receiver/sender recreation on failure |
| Graceful shutdown | Drains in-flight messages before closing |
| Telemetry | OpenTelemetry counters and histograms |
| CloudEvents | Preserves CloudEvents AMQP properties |
| Auth | Connection string or `DefaultAzureCredential` |

## Installation

```bash
go get github.com/fxsml/gopipe-azservicebus
```

## CLI

A command-line tool for publishing and subscribing to Azure Service Bus using JSONL (JSON Lines).

### Install

```bash
go install github.com/fxsml/gopipe-azservicebus/cmd/gopipe-azservicebus@latest
```

### Connection

Pass the Service Bus connection string via flag or environment variable:

```bash
export SERVICEBUS_CONNECTION="Endpoint=sb://..."
# or per-command: --connection "Endpoint=sb://..."
# or via env file: --env-file .env.local
```

### Publish

Read JSONL from stdin or a file and publish to a topic:

```bash
# from stdin
echo '{"specversion":"1.0","type":"com.example.event","source":"/test","id":"1"}' \
  | gopipe-azservicebus pub --topic my-topic

# from file
gopipe-azservicebus pub --topic my-topic --input-file events.jsonl
```

### Subscribe

Receive messages and write them as JSONL to stdout or a file:

```bash
# to stdout (Ctrl+C to stop)
gopipe-azservicebus sub --subscription my-topic/my-subscription

# to file, stop after 100 messages or 30 seconds
gopipe-azservicebus sub --subscription my-topic/my-subscription \
  --output-file out.jsonl --limit 100 --timeout 30s

# non-destructive peek (messages are not acknowledged)
gopipe-azservicebus sub --subscription my-topic/my-subscription --peek
```

## Known Issues

See [KNOWN-ISSUES.md](KNOWN-ISSUES.md) for documented edge cases and mitigations.

## License

MIT
