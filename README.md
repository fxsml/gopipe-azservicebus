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

## Known Issues

See [KNOWN-ISSUES.md](KNOWN-ISSUES.md) for documented edge cases and mitigations.

## License

MIT
