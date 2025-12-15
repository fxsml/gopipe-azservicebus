# gopipe-azservicebus

Azure Service Bus integration for the [gopipe](https://github.com/fxsml/gopipe) pipeline framework.

## Features

- **Sender**: Send messages to Azure Service Bus queues and topics
- **Receiver**: Receive messages from Azure Service Bus queues and topic subscriptions
- **gopipe Integration**: Helper functions for seamless integration with gopipe pipelines
- **Message Mapping**: Automatic conversion between gopipe messages and Azure Service Bus messages
- **Reliability**: Connection recovery, message acknowledgment, retry support
- **Multi-Destination**: Send to multiple destinations using a single Sender

## Installation

```bash
go get github.com/fxsml/gopipe-azservicebus
```

## Quick Start

### Sending Messages

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    "os"

    azservicebus "github.com/fxsml/gopipe-azservicebus"
    "github.com/fxsml/gopipe/message"
)

func main() {
    client, err := azservicebus.NewClient(os.Getenv("AZURE_SERVICEBUS_CONNECTION_STRING"))
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close(context.Background())

    sender := azservicebus.NewSender(client, azservicebus.SenderConfig{})
    defer sender.Close()

    // Create message
    body, _ := json.Marshal(map[string]string{"hello": "world"})
    msg := message.New(body, message.WithID[[]byte]("msg-001"))

    // Send
    err = sender.Send(context.Background(), "my-queue", []*message.Message[[]byte]{msg})
    if err != nil {
        log.Fatal(err)
    }
    log.Println("Message sent!")
}
```

### Receiving Messages

```go
receiver := azservicebus.NewReceiver(client, azservicebus.ReceiverConfig{
    ReceiveTimeout:  10 * time.Second,
    MaxMessageCount: 10,
})
defer receiver.Close()

msgs, err := receiver.Receive(ctx, "my-queue")
if err != nil {
    log.Fatal(err)
}

for _, msg := range msgs {
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
    azservicebus "github.com/fxsml/gopipe-azservicebus"
)

// Create receiver and generator
receiver := azservicebus.NewReceiver(client, azservicebus.ReceiverConfig{})
generator := azservicebus.NewMessageGenerator(receiver, "my-queue")

// Start receiving messages
msgs := generator.Generate(ctx)

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
```

### Sending with gopipe Pipeline

```go
// Create sender and sink pipe
sender := azservicebus.NewSender(client, azservicebus.SenderConfig{})
sinkPipe := azservicebus.NewMessageSinkPipe(sender, "my-queue", 10, 100*time.Millisecond)

// Create input channel
msgs := make(chan *message.Message[[]byte])

// Start the pipeline
done := sinkPipe.Start(ctx, msgs)

// Send messages
go func() {
    defer close(msgs)
    for _, order := range orders {
        body, _ := json.Marshal(order)
        msgs <- message.New(body)
    }
}()

// Wait for completion
for range done {}
```

### Content-Based Routing

```go
sender := azservicebus.NewSender(client, azservicebus.SenderConfig{})
defer sender.Close()

for msg := range msgs {
    // Route based on message properties
    targetQueue := "standard-queue"
    if priority, ok := msg.Properties().Get("priority"); ok && priority == "high" {
        targetQueue = "high-priority-queue"
    }
    sender.Send(ctx, targetQueue, []*message.Message[[]byte]{msg})
}
```

### Multi-Source Subscription with Fan-In

```go
// Create receivers for multiple queues
receiver1 := azservicebus.NewReceiver(client, azservicebus.ReceiverConfig{})
receiver2 := azservicebus.NewReceiver(client, azservicebus.ReceiverConfig{})

// Create generators
gen1 := azservicebus.NewMessageGenerator(receiver1, "queue-1")
gen2 := azservicebus.NewMessageGenerator(receiver2, "queue-2")

// Merge using gopipe FanIn
fanIn := gopipe.NewFanIn[*message.Message[[]byte]](gopipe.FanInConfig{})
fanIn.Add(gen1.Generate(ctx))
fanIn.Add(gen2.Generate(ctx))
merged := fanIn.Start(ctx)

for msg := range merged {
    // Messages from any source
    msg.Ack()
}
```

## Testing

```bash
# Run all tests (requires Azure Service Bus connection string)
make test

# Run unit tests only
make test-unit

# Run benchmarks
make bench
```

Set `AZURE_SERVICEBUS_CONNECTION_STRING` environment variable or create a `.env` file.

## Configuration

### Sender Configuration

```go
config := azservicebus.SenderConfig{
    SendTimeout:  30*time.Second,  // Timeout for send operations
    CloseTimeout: 30*time.Second,  // Timeout for closing
}
```

### Receiver Configuration

```go
config := azservicebus.ReceiverConfig{
    ReceiveTimeout:  30*time.Second, // Timeout for receive
    AckTimeout:      30*time.Second, // Timeout for ack/nack
    CloseTimeout:    30*time.Second, // Timeout for closing
    MaxMessageCount: 10,             // Messages per batch
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
gopipe Pipeline → Sender → Azure Service Bus → Receiver → gopipe Pipeline
```

### Key Components

- **Client**: Manages connection to Azure Service Bus
- **Sender**: Low-level batch send operations with connection pooling
- **Receiver**: Low-level batch receive operations with connection pooling
- **NewMessageGenerator**: Creates a gopipe Generator from a Receiver
- **NewMessageSinkPipe**: Creates a gopipe BatchPipe that sends messages via Sender

## Documentation

- [Getting Started](docs/getting-started.md)
- [Architecture](docs/architecture.md)
- [API Reference](docs/api-reference.md)

## Examples

See the [examples](examples/) directory:

- [Basic Send/Receive](examples/basic/) - Simple sending and receiving with gopipe integration
- [Sender Example](examples/sender/) - Direct sender usage
- [Pipeline Integration](examples/pipeline/) - Full gopipe pipeline with routing
- [Reliability Patterns](examples/reliability/) - Retry configuration and error handling

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
