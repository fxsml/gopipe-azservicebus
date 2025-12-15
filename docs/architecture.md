# Architecture

This document describes the architecture of `gopipe-azservicebus`, an Azure Service Bus integration for the gopipe pipeline framework.

## Overview

The library provides a seamless integration between Azure Service Bus and gopipe pipelines, enabling:

- **Message sending** to Azure Service Bus queues and topics
- **Message receiving** from Azure Service Bus queues and topic subscriptions
- **Pipeline integration** with gopipe's Generator, Pipe, and FanIn patterns
- **Reliability features** including automatic retries, connection recovery, and proper message acknowledgment

## Component Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           gopipe-azservicebus                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────────────────────────┐  │
│  │   Client    │    │   Sender    │    │          Receiver               │  │
│  │             │───▶│             │    │                                 │  │
│  │ Connection  │    │   Batch     │    │   Batch Receiving with         │  │
│  │ Management  │    │   Sending   │    │   Connection Pooling           │  │
│  └─────────────┘    └─────────────┘    └─────────────────────────────────┘  │
│         │                  │                          │                      │
│         │                  │                          │                      │
│         │                  ▼                          ▼                      │
│         │           ┌─────────────────┐    ┌─────────────────────────────┐  │
│         │           │ NewMessage      │    │ NewMessageGenerator         │  │
│         │           │ SinkPipe        │    │                             │  │
│         │           │                 │    │ gopipe Generator            │  │
│         │           │ gopipe BatchPipe│    │ integration                 │  │
│         │           └─────────────────┘    └─────────────────────────────┘  │
│         │                                                                    │
│         ▼                                                                    │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                    Azure Service Bus SDK                             │   │
│  │                                                                      │   │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐    │   │
│  │  │ Client   │  │ Sender   │  │ Receiver │  │ Admin Client     │    │   │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────────────┘    │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
              ┌───────────────────────────────────────────────────┐
              │              Azure Service Bus                     │
              │                                                    │
              │    ┌──────────┐    ┌──────────────────────┐      │
              │    │  Queues  │    │       Topics         │      │
              │    │          │    │                      │      │
              │    │ queue-1  │    │  topic-1             │      │
              │    │ queue-2  │    │    ├── sub-1        │      │
              │    │ ...      │    │    └── sub-2        │      │
              │    └──────────┘    └──────────────────────┘      │
              │                                                    │
              └───────────────────────────────────────────────────┘
```

## Core Components

### 1. Client (`client.go`)

The `Client` component manages connections to Azure Service Bus.

**Features:**
- Connection string authentication
- DefaultAzureCredential authentication (for production)
- Configurable retry options
- Auto-detection of authentication method

```go
// Connection string
client, err := azservicebus.NewClient("Endpoint=sb://...")

// DefaultAzureCredential
client, err := azservicebus.NewClient("namespace.servicebus.windows.net")

// With custom config
client, err := azservicebus.NewClientWithConfig(connStr, ClientConfig{
    MaxRetries:    5,
    RetryDelay:    time.Second,
    MaxRetryDelay: 60 * time.Second,
})
```

### 2. Sender (`sender.go`)

The `Sender` provides batch send operations with connection pooling.

**Features:**
- Batch sending for efficient throughput
- Connection pooling for multiple destinations
- Thread-safe operations
- Automatic sender recreation on connection loss

```go
sender := azservicebus.NewSender(client, SenderConfig{
    SendTimeout:  30 * time.Second,
})

msgs := []*message.Message[[]byte]{
    message.New([]byte("message 1")),
    message.New([]byte("message 2")),
}

err := sender.Send(ctx, "my-queue", msgs)
```

### 3. Receiver (`receiver.go`)

The `Receiver` provides batch receive operations with connection pooling.

**Features:**
- Batch receiving for efficient throughput
- Connection pooling for multiple sources
- Thread-safe operations
- Automatic receiver recreation on connection loss
- Support for both queues and topic subscriptions

```go
receiver := azservicebus.NewReceiver(client, ReceiverConfig{
    ReceiveTimeout:  30 * time.Second,
    MaxMessageCount: 10,
})

// From queue
msgs, err := receiver.Receive(ctx, "my-queue")

// From topic subscription
msgs, err := receiver.Receive(ctx, "my-topic/my-subscription")

for _, msg := range msgs {
    msg.Ack()
}
```

### 4. gopipe Integration Helpers (`gopipe.go`)

Helper functions for seamless gopipe integration.

#### NewMessageGenerator

Creates a gopipe Generator from a Receiver for continuous message consumption.

```go
generator := azservicebus.NewMessageGenerator(receiver, "my-queue")
msgs := generator.Generate(ctx)

for msg := range msgs {
    // Process continuously
    msg.Ack()
}
```

#### NewMessageSinkPipe

Creates a gopipe BatchPipe for efficient message sending.

```go
sinkPipe := azservicebus.NewMessageSinkPipe(sender, "my-queue", 10, 100*time.Millisecond)
out := sinkPipe.Start(ctx, inputChannel)
```

## Message Flow

### Sending Flow

```
Application → Message Channel → BatchPipe → Sender.Send() → Azure Service Bus
                                    │
                               Batching with
                               timeout/size
```

### Receiving Flow

```
Azure Service Bus → Receiver.Receive() → Generator → gopipe Pipeline → Application
                         │
                    Connection pooling
                    and batch receiving
```

## Reliability Features

### Connection Recovery

The Sender and Receiver automatically recreate connections when:
- Connection errors occur
- Azure Service Bus returns recoverable errors
- The underlying sender/receiver becomes invalid

### Message Acknowledgment

Messages support Ack/Nack for delivery guarantees:

```go
for _, msg := range msgs {
    if err := process(msg); err != nil {
        msg.Nack(err) // Returns to queue
        continue
    }
    msg.Ack() // Removed from queue
}
```

## Concurrency Model

### Thread Safety

- `Sender` uses `sync.RWMutex` for thread-safe sender pool access
- `Receiver` uses `sync.RWMutex` for thread-safe receiver pool access
- Both support concurrent operations from multiple goroutines

### Parallel Processing

Use gopipe's concurrency features for parallel message processing:

```go
processPipe := gopipe.NewTransformPipe(
    processFunc,
    gopipe.WithConcurrency[In, Out](10), // 10 concurrent workers
)
```

## gopipe Integration Patterns

### Generator Pattern (Consuming)

```go
generator := azservicebus.NewMessageGenerator(receiver, "queue")
msgs := generator.Generate(ctx)

// Feed into pipeline
results := processPipe.Start(ctx, msgs)
```

### Sink Pattern (Producing)

```go
sinkPipe := azservicebus.NewMessageSinkPipe(sender, "queue", 10, 100*time.Millisecond)
done := sinkPipe.Start(ctx, inputChannel)
```

### Fan-In Pattern (Multi-Source)

```go
gen1 := azservicebus.NewMessageGenerator(receiver1, "queue-1")
gen2 := azservicebus.NewMessageGenerator(receiver2, "queue-2")

fanIn := gopipe.NewFanIn[*message.Message[[]byte]](gopipe.FanInConfig{})
fanIn.Add(gen1.Generate(ctx))
fanIn.Add(gen2.Generate(ctx))
merged := fanIn.Start(ctx)
```

## Configuration Reference

### SenderConfig

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| SendTimeout | time.Duration | 30s | Timeout for send operations |
| CloseTimeout | time.Duration | 30s | Timeout for closing |

### ReceiverConfig

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| ReceiveTimeout | time.Duration | 30s | Timeout for receive operations |
| AckTimeout | time.Duration | 30s | Timeout for ack/nack operations |
| CloseTimeout | time.Duration | 30s | Timeout for closing |
| MaxMessageCount | int | 10 | Maximum messages per batch |

### ClientConfig

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| MaxRetries | int32 | 5 | Maximum retry attempts |
| RetryDelay | time.Duration | 1s | Initial retry delay |
| MaxRetryDelay | time.Duration | 60s | Maximum retry delay |
