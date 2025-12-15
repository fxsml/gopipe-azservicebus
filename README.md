# gopipe-azservicebus

Azure Service Bus integration for the [gopipe](https://github.com/fxsml/gopipe) pipeline framework.

## Features

- **Publisher**: Publish messages from gopipe pipelines to Azure Service Bus queues and topics
- **Subscriber**: Consume messages from Azure Service Bus and feed them into gopipe pipelines
- **Multi-Destination Routing**: Route messages to different queues/topics based on content
- **Multi-Source Subscription**: Merge messages from multiple sources into a single stream
- **Message Mapping**: Automatic conversion between gopipe messages and Azure Service Bus messages
- **Reliability**: Connection recovery, message acknowledgment, retry support
- **Local Development**: Run tests against the Azure Service Bus emulator

## Installation

```bash
go get github.com/fxsml/gopipe-azservicebus
```

## Quick Start

### Publishing Messages

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    "os"

    azservicebus "github.com/fxsml/gopipe-azservicebus"
    "github.com/fxsml/gopipe/message"
    "github.com/joho/godotenv"
)

func main() {
    _ = godotenv.Load()

    client, err := azservicebus.NewClient(os.Getenv("AZURE_SERVICEBUS_CONNECTION_STRING"))
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close(context.Background())

    publisher := azservicebus.NewPublisher(client, azservicebus.PublisherConfig{})
    defer publisher.Close()

    // Create message
    body, _ := json.Marshal(map[string]string{"hello": "world"})
    msg := message.New(body, message.WithID[[]byte]("msg-001"))

    // Publish
    err = publisher.PublishSync(context.Background(), "my-queue", []*message.Message[[]byte]{msg})
    if err != nil {
        log.Fatal(err)
    }
    log.Println("Message sent!")
}
```

### Subscribing to Messages

```go
subscriber := azservicebus.NewSubscriber(client, azservicebus.SubscriberConfig{
    ReceiveTimeout:  10 * time.Second,
    MaxMessageCount: 10,
})
defer subscriber.Close()

msgs, err := subscriber.Subscribe(ctx, "my-queue")
if err != nil {
    log.Fatal(err)
}

for msg := range msgs {
    var data map[string]string
    json.Unmarshal(msg.Payload(), &data)
    log.Printf("Received: %v", data)
    msg.Ack() // Acknowledge the message
}
```

### Using with gopipe Pipeline

```go
import (
    "github.com/fxsml/gopipe"
    "github.com/fxsml/gopipe/channel"
)

// Subscribe to messages
msgs, _ := subscriber.Subscribe(ctx, "my-queue")

// Process with gopipe
processPipe := gopipe.NewTransformPipe(
    func(ctx context.Context, msg *message.Message[[]byte]) (Result, error) {
        result := processMessage(msg.Payload())
        msg.Ack()
        return result, nil
    },
    gopipe.WithConcurrency[*message.Message[[]byte], Result](5),
)

results := processPipe.Start(ctx, msgs)

<-channel.Sink(results, func(r Result) {
    log.Printf("Processed: %v", r)
})
```

### Content-Based Routing

```go
multiPub := azservicebus.NewMultiPublisher(client, azservicebus.MultiPublisherConfig{})
defer multiPub.Close()

router := func(msg *message.Message[[]byte]) string {
    if priority, ok := msg.Properties().Get("priority"); ok && priority == "high" {
        return "high-priority-queue"
    }
    return "standard-queue"
}

done, _ := multiPub.Publish(ctx, msgs, router)
<-done
```

### Multi-Source Subscription

```go
multiSub := azservicebus.NewMultiSubscriber(client, azservicebus.MultiSubscriberConfig{})
defer multiSub.Close()

msgs, _ := multiSub.Subscribe(ctx, "queue-1", "queue-2", "topic/subscription")

for msg := range msgs {
    // Messages from any source
    msg.Ack()
}
```

## Local Development with Emulator

### Prerequisites

- Docker Desktop for Mac (with Rosetta 2 support for Apple Silicon)
- Go 1.24 or later

### Start the Emulator

```bash
make emulator-start
```

See [EMULATOR.md](EMULATOR.md) for detailed setup instructions.

### Run Tests

```bash
# All tests
make test

# Integration tests only (requires emulator or Azure)
make test-integration

# Unit tests only
make test-unit
```

## Configuration

### Publisher Configuration

```go
config := azservicebus.PublisherConfig{
    BatchSize:    10,              // Messages per batch
    BatchTimeout: 100*time.Millisecond, // Max wait for batch
    SendTimeout:  30*time.Second,  // Timeout for send operations
    CloseTimeout: 30*time.Second,  // Timeout for closing
}
```

### Subscriber Configuration

```go
config := azservicebus.SubscriberConfig{
    ReceiveTimeout:  30*time.Second, // Timeout for receive
    AckTimeout:      30*time.Second, // Timeout for ack/nack
    CloseTimeout:    30*time.Second, // Timeout for closing
    MaxMessageCount: 10,             // Messages per batch
    BufferSize:      100,            // Output channel buffer
    PollInterval:    time.Second,    // Polling interval
}
```

### Authentication

```go
// Connection string (development)
client, err := azservicebus.NewClient("Endpoint=sb://...")

// DefaultAzureCredential (production)
client, err := azservicebus.NewClient("your-namespace.servicebus.windows.net")

// With custom retry config
client, err := azservicebus.NewClientWithConfig(connStr, azservicebus.ClientConfig{
    MaxRetries:    5,
    RetryDelay:    time.Second,
    MaxRetryDelay: 60*time.Second,
})
```

## Message Properties

Message properties are automatically mapped between gopipe and Azure Service Bus:

```go
msg := message.New(body,
    message.WithID[[]byte]("id-123"),
    message.WithCorrelationID[[]byte]("corr-456"),
    message.WithSubject[[]byte]("order.created"),
    message.WithContentType[[]byte]("application/json"),
    message.WithTTL[[]byte](1*time.Hour),
    message.WithProperty[[]byte]("custom", "value"),
)
```

| gopipe Property | Azure Service Bus |
|-----------------|-------------------|
| ID | MessageID |
| CorrelationID | CorrelationID |
| Subject | Subject |
| ContentType | ContentType |
| ReplyTo | ReplyTo |
| TTL | TimeToLive |
| Custom properties | ApplicationProperties |

## Architecture

```
gopipe Pipeline → Publisher → Azure Service Bus → Subscriber → gopipe Pipeline
```

### Key Components

- **Client**: Manages connection to Azure Service Bus
- **Publisher/Subscriber**: Channel-based high-level API
- **Sender/Receiver**: Low-level batch operations
- **MultiPublisher/MultiSubscriber**: Multi-destination/source support

## Documentation

- [Getting Started](docs/getting-started.md)
- [Architecture](docs/architecture.md)
- [API Reference](docs/api-reference.md)
- [Emulator Setup](EMULATOR.md)

## Examples

See the [examples](examples/) directory:

- [Basic Publish/Subscribe](examples/basic/)
- [Pipeline Integration](examples/pipeline/)
- [Reliability Patterns](examples/reliability/)

## Development

```bash
# Format code
make fmt

# Run linter
make lint

# Run go vet
make vet

# Tidy modules
make tidy

# Build
make build
```

## Known Issues

### macOS Emulator DNS Issues

The Azure Service Bus emulator may experience DNS resolution issues on macOS. See [EMULATOR.md](EMULATOR.md) for workarounds.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests: `make test`
5. Submit a pull request

## License

See [LICENSE](LICENSE) file.

## Resources

- [gopipe Framework](https://github.com/fxsml/gopipe)
- [Azure Service Bus Documentation](https://learn.microsoft.com/en-us/azure/service-bus-messaging/)
- [Azure SDK for Go](https://github.com/Azure/azure-sdk-for-go)
