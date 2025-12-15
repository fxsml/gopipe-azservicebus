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

### 3. Send Messages

```go
import (
    azservicebus "github.com/fxsml/gopipe-azservicebus"
    "github.com/fxsml/gopipe/message"
)

// Create sender
sender := azservicebus.NewSender(client, azservicebus.SenderConfig{
    SendTimeout: 30 * time.Second,
})
defer sender.Close()

// Create messages
messages := []*message.Message[[]byte]{
    message.New([]byte(`{"id": 1}`)),
    message.New([]byte(`{"id": 2}`)),
}

// Send batch
err = sender.Send(ctx, "my-queue", messages)
if err != nil {
    log.Fatal(err)
}
```

### 4. Receive Messages

```go
// Create receiver
receiver := azservicebus.NewReceiver(client, azservicebus.ReceiverConfig{
    ReceiveTimeout:  10 * time.Second,
    MaxMessageCount: 10,
})
defer receiver.Close()

// Receive from queue
msgs, err := receiver.Receive(ctx, "my-queue")
if err != nil {
    log.Fatal(err)
}

// Process messages
for _, msg := range msgs {
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
// Send to topic
err = sender.Send(ctx, "my-topic", messages)

// Receive from topic subscription
// Format: "topic-name/subscription-name"
msgs, err := receiver.Receive(ctx, "my-topic/my-subscription")
```

## Using with gopipe

### Continuous Receiving with Generator

```go
// Create receiver and generator
receiver := azservicebus.NewReceiver(client, azservicebus.ReceiverConfig{})
generator := azservicebus.NewMessageGenerator(receiver, "my-queue")

// Start receiving (continuous)
msgs := generator.Generate(ctx)

// Process messages
for msg := range msgs {
    // Process
    msg.Ack()
}
```

### Sending with SinkPipe

```go
// Create sender and sink pipe
sender := azservicebus.NewSender(client, azservicebus.SenderConfig{})
sinkPipe := azservicebus.NewMessageSinkPipe(sender, "my-queue", 10, 100*time.Millisecond)

// Create message channel
msgs := make(chan *message.Message[[]byte])
done := sinkPipe.Start(ctx, msgs)

// Send messages
go func() {
    defer close(msgs)
    for i := 0; i < 10; i++ {
        body, _ := json.Marshal(map[string]int{"id": i})
        msgs <- message.New(body)
    }
}()

// Wait for completion
for range done {}
```

### Processing Pipeline

```go
import (
    "github.com/fxsml/gopipe"
    "github.com/fxsml/gopipe/channel"
)

// Create receiver and generator
receiver := azservicebus.NewReceiver(client, azservicebus.ReceiverConfig{})
generator := azservicebus.NewMessageGenerator(receiver, "my-queue")

// Start receiving
msgs := generator.Generate(ctx)

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

## Basic Examples

### Sending with Properties

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

### Multi-Destination Routing

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
for _, msg := range msgs {
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

// Generator will close channel when context is cancelled
generator := azservicebus.NewMessageGenerator(receiver, "queue")
msgs := generator.Generate(ctx)

for msg := range msgs {
    // Process
    msg.Ack()
}

// Cleanup
receiver.Close()
sender.Close()
client.Close(context.Background())
```

## Next Steps

- See [Architecture](architecture.md) for detailed component information
- Check [API Reference](api-reference.md) for complete API documentation
- See [examples/](../examples/) for complete working examples
