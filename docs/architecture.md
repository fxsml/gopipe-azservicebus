# Architecture

This document describes the architecture of `gopipe-azservicebus`, an Azure Service Bus integration for the gopipe pipeline framework.

## Overview

The library provides a seamless integration between Azure Service Bus and gopipe pipelines, enabling:

- **Message consumption** from Azure Service Bus queues and topic subscriptions
- **Message publishing** to Azure Service Bus queues and topics
- **Pipeline integration** with gopipe's Generator, Processor, and Sink patterns
- **Reliability features** including automatic retries, connection recovery, and proper message acknowledgment

## Component Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           gopipe-azservicebus                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐  │
│  │   Client    │    │  Publisher  │    │ Subscriber  │    │   Sender    │  │
│  │             │───▶│             │    │             │    │             │  │
│  │ Connection  │    │  Channel    │    │  Channel    │    │   Batch     │  │
│  │ Management  │    │  Batching   │    │  Streaming  │    │   Sending   │  │
│  └─────────────┘    └─────────────┘    └─────────────┘    └─────────────┘  │
│         │                  │                  │                  │          │
│         │                  ▼                  ▼                  │          │
│         │           ┌─────────────┐    ┌─────────────┐          │          │
│         │           │ MultiPub    │    │ MultiSub    │          │          │
│         │           │             │    │             │          │          │
│         │           │  Routing    │    │   Merge     │          │          │
│         │           └─────────────┘    └─────────────┘          │          │
│         │                                                       │          │
│         ▼                                                       ▼          │
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

### 2. Publisher (`publisher.go`)

The `Publisher` provides a channel-based API for publishing messages.

**Features:**
- Batching for efficient sending
- Configurable batch size and timeout
- Asynchronous publishing via channels
- Synchronous publishing option

**Interface:**
```go
type Publisher interface {
    Publish(ctx context.Context, queueOrTopic string, msgs <-chan *message.Message[[]byte]) (<-chan struct{}, error)
    PublishSync(ctx context.Context, queueOrTopic string, msgs []*message.Message[[]byte]) error
    Close() error
}
```

### 3. Subscriber (`subscriber.go`)

The `Subscriber` provides a channel-based API for receiving messages.

**Features:**
- Continuous message streaming
- Configurable polling and buffering
- Automatic message acknowledgment handling
- Graceful shutdown support

**Interface:**
```go
type Subscriber interface {
    Subscribe(ctx context.Context, queueOrTopic string) (<-chan *message.Message[[]byte], error)
    Close() error
}
```

### 4. Sender (`sender.go`)

The low-level `Sender` sends messages directly to Azure Service Bus.

**Features:**
- Connection pooling for multiple destinations
- Automatic sender recreation on connection loss
- Thread-safe operations
- Batch sending

### 5. Receiver (`receiver.go`)

The low-level `Receiver` receives messages directly from Azure Service Bus.

**Features:**
- Connection pooling for multiple sources
- Automatic receiver recreation on connection loss
- Message property mapping
- Ack/Nack callbacks

### 6. MultiPublisher & MultiSubscriber

Higher-level components for multi-destination scenarios.

**MultiPublisher:**
- Content-based routing
- Publish to multiple queues/topics
- Dynamic destination management

**MultiSubscriber:**
- Merge messages from multiple sources
- Single output channel
- Parallel subscription handling

## Message Flow

### Publishing Flow

```
                      gopipe Pipeline
                            │
                            ▼
┌─────────────────────────────────────────────────────┐
│                    Publisher                         │
│                                                      │
│   ┌──────────┐    ┌──────────┐    ┌──────────┐    │
│   │ Input    │───▶│ Batching │───▶│  Sender  │    │
│   │ Channel  │    │  Logic   │    │          │    │
│   └──────────┘    └──────────┘    └──────────┘    │
│                                         │          │
└─────────────────────────────────────────│──────────┘
                                          │
                                          ▼
                               Azure Service Bus Queue
```

### Subscription Flow

```
                               Azure Service Bus Queue
                                          │
                                          ▼
┌─────────────────────────────────────────────────────┐
│                    Subscriber                        │
│                                                      │
│   ┌──────────┐    ┌──────────┐    ┌──────────┐    │
│   │ Receiver │───▶│ Message  │───▶│ Output   │    │
│   │          │    │ Transform│    │ Channel  │    │
│   └──────────┘    └──────────┘    └──────────┘    │
│                                                      │
└─────────────────────────────────────────────────────┘
                            │
                            ▼
                      gopipe Pipeline
                            │
                            ▼
                      ┌──────────┐
                      │ msg.Ack()│  (on success)
                      │msg.Nack()│  (on failure)
                      └──────────┘
```

## Reliability Features

### Connection Recovery

The library automatically handles connection loss:

1. Detects `CodeConnectionLost` or `CodeClosed` errors
2. Recreates the sender/receiver
3. Retries the operation once

```go
// Handled automatically inside Sender.Send() and Receiver.Receive()
if errors.As(err, &sbErr) && sbErr.Code == azservicebus.CodeConnectionLost {
    newSender, _ := s.recreateSender(queueOrTopic)
    return s.attemptSendBatch(ctx, newSender, messages)
}
```

### Message Acknowledgment

Proper acknowledgment ensures message delivery guarantees:

```go
// On successful processing
msg.Ack()  // Calls CompleteMessage - message removed from queue

// On transient failure (will retry)
msg.Nack(err)  // Calls AbandonMessage - message returned to queue

// Error details are preserved
opts.PropertiesToModify = map[string]any{
    "abandon_error": err.Error(),
}
```

### Retry Configuration

Use gopipe's retry configuration for pipeline-level retries:

```go
gopipe.WithRetryConfig[In, Out](gopipe.RetryConfig{
    MaxAttempts: 3,
    Backoff:     gopipe.ExponentialBackoff(100*time.Millisecond, 2.0, 5*time.Second, 0.1),
    ShouldRetry: func(err error) bool {
        return errors.Is(err, ErrTransient)
    },
})
```

## Concurrency Model

### Thread Safety

All components use proper synchronization:

- `sync.RWMutex` for sender/receiver maps
- `sync.RWMutex` for closed state
- Double-check locking pattern for lazy initialization

### Parallel Processing

The library supports concurrent message processing:

```go
// Subscriber doesn't block downstream processing
msgs, _ := subscriber.Subscribe(ctx, queue)
for msg := range msgs {
    go processMessage(msg)  // Process in parallel
}

// Or use gopipe's concurrency
processPipe := gopipe.NewTransformPipe(
    processFunc,
    gopipe.WithConcurrency[In, Out](10),  // 10 concurrent workers
)
```

## Integration with gopipe

### Using with Generator

```go
gen := gopipe.NewGenerator(func(ctx context.Context) ([]*message.Message[[]byte], error) {
    return receiver.Receive(ctx, queueName)
})
out := gen.Generate(ctx)
```

### Using with ProcessPipe

```go
msgs, _ := subscriber.Subscribe(ctx, queue)

pipe := gopipe.NewProcessPipe(
    func(ctx context.Context, msg *message.Message[[]byte]) ([]Result, error) {
        // Process message
        msg.Ack()
        return results, nil
    },
    gopipe.WithConcurrency[*message.Message[[]byte], Result](5),
)

results := pipe.Start(ctx, msgs)
```

### Using with SinkPipe

```go
msgs, _ := subscriber.Subscribe(ctx, queue)

sink := gopipe.NewSinkPipe(
    func(ctx context.Context, msg *message.Message[[]byte]) error {
        // Final processing
        msg.Ack()
        return nil
    },
)

done := sink.Start(ctx, msgs)
<-done
```

## Configuration Reference

### ClientConfig

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| MaxRetries | int32 | 5 | Maximum retry attempts |
| RetryDelay | time.Duration | 1s | Initial delay between retries |
| MaxRetryDelay | time.Duration | 60s | Maximum delay between retries |

### PublisherConfig

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| BatchSize | int | 10 | Messages per batch |
| BatchTimeout | time.Duration | 100ms | Max wait for batch fill |
| SendTimeout | time.Duration | 30s | Timeout for send operations |
| CloseTimeout | time.Duration | 30s | Timeout for closing |

### SubscriberConfig

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| ReceiveTimeout | time.Duration | 30s | Timeout for receive operations |
| AckTimeout | time.Duration | 30s | Timeout for ack/nack |
| CloseTimeout | time.Duration | 30s | Timeout for closing |
| MaxMessageCount | int | 10 | Messages per receive batch |
| BufferSize | int | 100 | Output channel buffer size |
| PollInterval | time.Duration | 1s | Polling interval when no messages |
