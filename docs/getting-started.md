# Getting Started

This guide will help you get started with `gopipe-azservicebus`.

## Installation

```bash
go get github.com/fxsml/gopipe-azservicebus
```

## Prerequisites

- Go 1.24 or later
- Azure Service Bus namespace (Standard or Premium tier for topics)
- Connection string or Azure credentials

## Quick Start

### 1. Set Up Environment

Create a `.env` file with your Azure Service Bus connection string:

```env
AZURE_SERVICEBUS_CONNECTION_STRING=Endpoint=sb://your-namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=your-key
```

### 2. Create a Client

```go
package main

import (
    "context"
    "log"
    "os"

    azservicebus "github.com/fxsml/gopipe-azservicebus"
    "github.com/joho/godotenv"
)

func main() {
    _ = godotenv.Load()

    connectionString := os.Getenv("AZURE_SERVICEBUS_CONNECTION_STRING")

    client, err := azservicebus.NewClient(connectionString)
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close(context.Background())

    log.Println("Connected to Azure Service Bus!")
}
```

### 3. Publish Messages

```go
import (
    azservicebus "github.com/fxsml/gopipe-azservicebus"
    "github.com/fxsml/gopipe/message"
)

// Create publisher
publisher := azservicebus.NewPublisher(client, azservicebus.PublisherConfig{
    BatchSize:    10,
    BatchTimeout: 100 * time.Millisecond,
})
defer publisher.Close()

// Option 1: Channel-based (async)
msgs := make(chan *message.Message[[]byte])
done, err := publisher.Publish(ctx, "my-queue", msgs)
if err != nil {
    log.Fatal(err)
}

go func() {
    defer close(msgs)
    for i := 0; i < 10; i++ {
        body, _ := json.Marshal(map[string]int{"id": i})
        msgs <- message.New(body)
    }
}()

<-done // Wait for completion

// Option 2: Synchronous
messages := []*message.Message[[]byte]{
    message.New([]byte(`{"id": 1}`)),
    message.New([]byte(`{"id": 2}`)),
}
err = publisher.PublishSync(ctx, "my-queue", messages)
```

### 4. Subscribe to Messages

```go
// Create subscriber
subscriber := azservicebus.NewSubscriber(client, azservicebus.SubscriberConfig{
    ReceiveTimeout:  10 * time.Second,
    MaxMessageCount: 10,
})
defer subscriber.Close()

// Subscribe to queue
msgs, err := subscriber.Subscribe(ctx, "my-queue")
if err != nil {
    log.Fatal(err)
}

// Process messages
for msg := range msgs {
    var data map[string]int
    if err := json.Unmarshal(msg.Payload(), &data); err != nil {
        msg.Nack(err)
        continue
    }

    log.Printf("Received: %v", data)
    msg.Ack() // Acknowledge successful processing
}
```

### 5. Using with Topics

```go
// Publish to topic
err = publisher.PublishSync(ctx, "my-topic", messages)

// Subscribe to topic subscription
// Format: "topic-name/subscription-name"
msgs, err := subscriber.Subscribe(ctx, "my-topic/my-subscription")
```

## Basic Examples

### Publishing with Properties

```go
msg := message.New(body,
    message.WithID[[]byte]("unique-id"),
    message.WithCorrelationID[[]byte]("correlation-123"),
    message.WithSubject[[]byte]("order.created"),
    message.WithContentType[[]byte]("application/json"),
    message.WithProperty[[]byte]("custom_field", "value"),
    message.WithTTL[[]byte](1 * time.Hour),
)
```

### Processing Pipeline

```go
import (
    "github.com/fxsml/gopipe"
    "github.com/fxsml/gopipe/channel"
)

// Subscribe
msgs, _ := subscriber.Subscribe(ctx, "my-queue")

// Transform
transformPipe := gopipe.NewTransformPipe(
    func(ctx context.Context, msg *message.Message[[]byte]) (Result, error) {
        // Process message
        result := processMessage(msg.Payload())
        msg.Ack()
        return result, nil
    },
    gopipe.WithConcurrency[*message.Message[[]byte], Result](5),
)

results := transformPipe.Start(ctx, msgs)

// Sink
<-channel.Sink(results, func(r Result) {
    log.Printf("Result: %v", r)
})
```

### Multi-Destination Routing

```go
multiPub := azservicebus.NewMultiPublisher(client, azservicebus.MultiPublisherConfig{
    PublisherConfig: azservicebus.PublisherConfig{
        BatchSize: 10,
    },
})
defer multiPub.Close()

router := func(msg *message.Message[[]byte]) string {
    if priority, ok := msg.Properties().Get("priority"); ok {
        if priority == "high" {
            return "high-priority-queue"
        }
    }
    return "standard-queue"
}

done, _ := multiPub.Publish(ctx, msgs, router)
<-done
```

### Multi-Source Subscription

```go
multiSub := azservicebus.NewMultiSubscriber(client, azservicebus.MultiSubscriberConfig{
    SubscriberConfig: azservicebus.SubscriberConfig{
        ReceiveTimeout:  10 * time.Second,
        MaxMessageCount: 10,
    },
})
defer multiSub.Close()

// Subscribe to multiple sources
msgs, _ := multiSub.Subscribe(ctx, "queue-1", "queue-2", "topic/subscription")

for msg := range msgs {
    // Messages from any source
    msg.Ack()
}
```

## Authentication Methods

### Connection String (Development)

```go
client, err := azservicebus.NewClient("Endpoint=sb://your-namespace.servicebus.windows.net/;SharedAccessKeyName=...;SharedAccessKey=...")
```

### DefaultAzureCredential (Production)

```go
// Uses environment variables, managed identity, etc.
client, err := azservicebus.NewClient("your-namespace.servicebus.windows.net")
```

## Error Handling

### Transient Errors

Use `Nack()` to return messages to the queue for retry:

```go
for msg := range msgs {
    if err := process(msg); err != nil {
        if isTransient(err) {
            msg.Nack(err) // Message returns to queue
            continue
        }
        // Permanent error
        log.Printf("Failed: %v", err)
        msg.Ack() // Remove from queue to avoid infinite loop
        continue
    }
    msg.Ack()
}
```

### Using gopipe Retry

```go
pipe := gopipe.NewTransformPipe(
    processFunc,
    gopipe.WithRetryConfig[In, Out](gopipe.RetryConfig{
        MaxAttempts: 3,
        Backoff:     gopipe.ExponentialBackoff(100*time.Millisecond, 2.0, 5*time.Second, 0.1),
        ShouldRetry: func(err error) bool {
            return isTransient(err)
        },
    }),
)
```

## Graceful Shutdown

```go
ctx, cancel := context.WithCancel(context.Background())

// Handle signals
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
go func() {
    <-sigChan
    cancel() // Triggers graceful shutdown
}()

// Subscriber will close channel when context is cancelled
msgs, _ := subscriber.Subscribe(ctx, "queue")

for msg := range msgs {
    // Process
    msg.Ack()
}

// Cleanup
subscriber.Close()
publisher.Close()
client.Close(context.Background())
```

## Next Steps

- See [Architecture](architecture.md) for detailed component information
- Check [examples/](../examples/) for complete working examples
- Read [EMULATOR.md](../EMULATOR.md) for local development setup
