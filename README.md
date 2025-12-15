# gopipe-azservicebus

Azure Service Bus integration for the [gopipe](https://github.com/fxsml/gopipe) pipeline framework.

> **Note:** This library requires **gopipe v0.10.0** or later which uses a CloudEvents-aligned message API.

## Features

- **Sender**: Send messages to Azure Service Bus queues and topics (implements `message.Sender`)
- **Receiver**: Receive messages from Azure Service Bus queues and topic subscriptions (implements `message.Receiver`)
- **Publisher**: Channel-based message publishing using gopipe's `message.NewPublisher`
- **Subscriber**: Channel-based message subscription using gopipe's `message.NewSubscriber`
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

    // Create message with attributes
    body, _ := json.Marshal(map[string]string{"hello": "world"})
    msg := message.New(body, message.Attributes{
        message.AttrID: "msg-001",
    })

    // Send
    err = sender.Send(context.Background(), "my-queue", []*message.Message{msg})
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
    json.Unmarshal(msg.Data, &data)
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
    func(ctx context.Context, msg *message.Message) (Result, error) {
        result := processMessage(msg.Data)
        msg.Ack()
        return result, nil
    },
    gopipe.WithConcurrency[*message.Message, Result](5),
)

results := processPipe.Start(ctx, msgs)
```

### Sending with gopipe Pipeline

```go
// Create sender and sink pipe
sender := azservicebus.NewSender(client, azservicebus.SenderConfig{})
sinkPipe := azservicebus.NewMessageSinkPipe(sender, "my-queue", 10, 100*time.Millisecond)

// Create input channel
msgs := make(chan *message.Message)

// Start the pipeline
done := sinkPipe.Start(ctx, msgs)

// Send messages
go func() {
    defer close(msgs)
    for _, order := range orders {
        body, _ := json.Marshal(order)
        msgs <- message.New(body, nil)
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
    // Route based on message attributes
    targetQueue := "standard-queue"
    if priority, ok := msg.Attributes["priority"]; ok && priority == "high" {
        targetQueue = "high-priority-queue"
    }
    sender.Send(ctx, targetQueue, []*message.Message{msg})
}
```

### Using Publisher and Subscriber

The library provides `Publisher` and `Subscriber` types that wrap gopipe's `message.NewPublisher` and `message.NewSubscriber` for channel-based message handling:

```go
// Create a subscriber for continuous message polling
receiver := azservicebus.NewReceiver(client, azservicebus.ReceiverConfig{})
subscriber := azservicebus.NewSubscriber(receiver, azservicebus.SubscriberConfig{
    Concurrency: 1,
})

msgs := subscriber.Subscribe(ctx, "my-queue")
for msg := range msgs {
    log.Printf("Received: %s", msg.Data)
    msg.Ack()
}

// Create a publisher for batched message publishing
sender := azservicebus.NewSender(client, azservicebus.SenderConfig{})
publisher := azservicebus.NewPublisher(sender, azservicebus.PublisherConfig{
    MaxBatchSize: 10,
    MaxDuration:  100 * time.Millisecond,
})

// Messages need AttrTopic attribute for routing
msgs := make(chan *message.Message)
done := publisher.Publish(ctx, msgs)

go func() {
    defer close(msgs)
    for _, order := range orders {
        body, _ := json.Marshal(order)
        msgs <- message.New(body, message.Attributes{
            message.AttrTopic: "my-queue",
        })
    }
}()

<-done
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
fanIn := gopipe.NewFanIn[*message.Message](gopipe.FanInConfig{})
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

Message attributes are automatically mapped between gopipe and Azure Service Bus:

```go
msg := message.New(body, message.Attributes{
    message.AttrID:              "id-123",
    message.AttrCorrelationID:   "corr-456",
    message.AttrSubject:         "order.created",
    message.AttrDataContentType: "application/json",
    "ttl":                       1*time.Hour,
    "custom":                    "value",
})
```

| gopipe Attribute | Azure Service Bus |
|-----------------|-------------------|
| `message.AttrID` | MessageID |
| `message.AttrCorrelationID` | CorrelationID |
| `message.AttrSubject` | Subject |
| `message.AttrDataContentType` | ContentType |
| `message.AttrTime` | EnqueuedTime (read-only) |
| `"ttl"` | TimeToLive |
| Custom attributes | ApplicationProperties |

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

## Architecture Evaluation

### gopipe v0.10.0 Integration

This library was refactored to leverage gopipe v0.10.0's built-in `message.NewPublisher` and `message.NewSubscriber` implementations, following the principle of not reinventing the wheel.

#### Design Decisions

1. **Interface Implementation**: `Sender` and `Receiver` implement gopipe's `message.Sender` and `message.Receiver` interfaces directly, enabling seamless integration with gopipe's Publisher/Subscriber infrastructure.

2. **Type Aliases**: `Publisher` and `Subscriber` are simple type aliases to gopipe's implementations with convenience constructors, reducing code duplication and ensuring consistent behavior.

3. **CloudEvents Alignment**: gopipe v0.10.0 uses a CloudEvents-aligned message model with `message.Attributes` map instead of generic type parameters, simplifying the API.

#### Comparison: Direct vs gopipe Integration

| Approach | Pros | Cons |
|----------|------|------|
| **Direct Sender/Receiver** | Full control, explicit batching, simpler debugging | Manual batch management, no built-in polling |
| **gopipe Publisher/Subscriber** | Automatic batching, continuous polling, consistent API | Less control over timing, additional abstraction layer |
| **gopipe Generator/SinkPipe** | Pipeline integration, composable with other gopipe stages | Requires understanding gopipe patterns |

#### When to Use What

- **Sender.Send/Receiver.Receive**: Use for simple, one-off operations or when you need precise control over batching and timing.
- **Publisher/Subscriber**: Use for continuous message streams with automatic batching and polling.
- **Generator/SinkPipe helpers**: Use when building complex pipelines with gopipe transformations, fan-in/fan-out, or parallel processing.

### Benchmark Results

Run benchmarks with an Azure Service Bus connection:

```bash
AZURE_SERVICEBUS_CONNECTION_STRING="..." go test -bench=. -benchmem
```

The benchmarks compare:
- `BenchmarkSenderDirect`: Direct Sender.Send calls with manual batching
- `BenchmarkSenderGopipeBatchPipe`: Using gopipe's BatchPipe
- `BenchmarkSenderGopipeHelper`: Using the NewMessageSinkPipe helper
- `BenchmarkPublisher`: Using the Publisher wrapper
- `BenchmarkSubscriber`: Using the Subscriber wrapper

Expected characteristics:
- Direct sends have lowest overhead but require manual batch management
- gopipe integration adds minimal overhead while providing richer functionality
- Publisher/Subscriber provide the most convenient API with automatic batching

## Resources

- [gopipe Framework](https://github.com/fxsml/gopipe)
- [Azure Service Bus Documentation](https://learn.microsoft.com/en-us/azure/service-bus-messaging/)
- [Azure SDK for Go](https://github.com/Azure/azure-sdk-for-go)
