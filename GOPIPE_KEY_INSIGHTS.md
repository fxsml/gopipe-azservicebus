# Gopipe Framework: Key Insights for Azure Service Bus Integration

## Quick Reference: Essential Interfaces

### 1. Message[T] - At-Least-Once Delivery

```go
// The fundamental unit of work with acknowledgment semantics
type Message[T any] struct {
    Metadata Metadata           // Key-value metadata map
    Payload  T                  // Your data
    
    // Private fields accessed via methods:
    ack  func()                 // Called on success (broker removes message)
    nack func(error)            // Called on failure (broker may retry)
}

// Construction patterns:
msg1 := gopipe.NewMessage(metadata, payload, deadline, ackFn, nackFn)
msg2 := gopipe.CopyMessage(originalMsg, newPayload)  // Preserves ack/nack

// Methods:
msg.Ack()                       // Returns bool: success, false if already ack'd/nack'd
msg.Nack(err)                   // Returns bool: success, false if already ack'd/nack'd
msg.Deadline()                  // Returns time.Time
msg.SetTimeout(duration)        // Sets deadline = now + duration
```

### 2. Processor[In, Out] - Core Logic

```go
type Processor[In, Out any] interface {
    // Process one item, return 0-many outputs or error
    Process(ctx context.Context, In) ([]Out, error)
    
    // Handle errors (called by framework on failure)
    Cancel(In, error)
}

// Usually created via factory functions, but raw creation:
proc := gopipe.NewProcessor(
    func(ctx context.Context, in int) ([]int, error) {
        return []int{in * 2}, nil
    },
    func(in int, err error) {
        log.Printf("Failed: %v", err)
    },
)
```

### 3. Pipe[Pre, Out] - Orchestration

```go
type Pipe[Pre, Out any] interface {
    Start(ctx context.Context, pre <-chan Pre) <-chan Out
}

// Key pipe types for Azure Service Bus integration:

// Consume: NewMessagePipe
//   Input:  chan *Message[T]
//   Output: chan *Message[T]
//   Auto:   Ack on success, Nack on error, Metadata propagation
pipe := gopipe.NewMessagePipe(
    func(ctx context.Context, data T) ([]T, error) {
        // Process payload
        return []T{...}, nil
    },
)

// Publish: NewSinkPipe
//   Input:  chan T
//   Output: chan struct{}
//   Auto:   Terminal stage, signals completion
pipe := gopipe.NewSinkPipe(
    func(ctx context.Context, msg *Message[T]) error {
        return serviceBus.Send(ctx, msg)
    },
)

// Other useful pipes:
NewTransformPipe(handler)       // 1 input → 1 output
NewProcessPipe(handler)         // 1 input → 0..N outputs
NewFilterPipe(predicate)        // 1 input → 0 or 1 output
NewBatchPipe(handler, size, maxWait)  // Multiple inputs → batch processing
```

### 4. Generator[Out] - Source Stage

```go
type Generator[Out any] interface {
    Generate(ctx context.Context) <-chan Out
}

// For consuming from Service Bus:
gen := gopipe.NewGenerator(
    func(ctx context.Context) ([]Message[T], error) {
        // Called repeatedly until context cancellation
        // Fetch messages from Service Bus
        messages, err := client.ReceiveMessages(ctx, 10)
        return messages, err
    },
    gopipe.WithConcurrency[struct{}, Message[T]](5),
)

out := gen.Generate(ctx)  // <-chan Message[T]
```

## Core Patterns

### Pattern 1: Message Broker Integration

```go
// 1. Create messages with ack/nack handlers tied to broker operations
msg := gopipe.NewMessage(
    metadata{"id": msgID},
    payload,
    time.Time{},
    func() { 
        // Called on success - tell broker to delete message
        broker.Complete(ctx, msgID)
    },
    func(err error) {
        // Called on failure - tell broker to requeue/deadletter
        broker.Abandon(ctx, msgID, err)
    },
)

// 2. Process using NewMessagePipe - automatically calls ack/nack
pipe := gopipe.NewMessagePipe(
    func(ctx context.Context, payload T) ([]T, error) {
        // Your business logic
        return process(payload), nil
    },
)

// 3. Success = ack() called automatically
//    Error = nack(err) called automatically
//    Timeout/Cancel = nack(err) called automatically
```

### Pattern 2: Error Handling with Retries

```go
// Automatic retry with exponential backoff
pipe := gopipe.NewMessagePipe(
    handler,
    gopipe.WithRetryConfig(
        gopipe.ExponentialBackoff(
            100*time.Millisecond,  // Initial delay
            2.0,                   // Factor (doubles each retry)
            5*time.Second,         // Max delay cap
            0.1,                   // Jitter: ±10%
        ),
        gopipe.ShouldRetry(
            // Only retry transient errors
            net.ErrClosed,
            context.DeadlineExceeded,
            // ... other retryable errors
        ),
    ),
)

// Failed messages:
// - Transient errors: retry up to N times (framework handles)
// - Fatal errors: nack'd immediately (your code handles)
// - Exhausted retries: nack'd with ErrRetryMaxAttempts
```

### Pattern 3: Metadata Propagation

```go
// Metadata flows through pipeline automatically in MessagePipe:
msg := gopipe.NewMessage(
    gopipe.Metadata{
        "id": "msg-123",
        "source": "service-bus",
        "priority": "high",
    },
    payload,
    time.Time{},
    ackFn,
    nackFn,
)

// Available in processing context:
pipe := gopipe.NewMessagePipe(
    func(ctx context.Context, data T) ([]T, error) {
        // Extract metadata
        metadata := gopipe.MetadataFromContext(ctx)
        log.Printf("Processing message %s", metadata["id"])
        
        // On error, metadata is attached to the error
        if err != nil {
            return nil, err  // Nack will get the error with metadata
        }
        
        return []T{...}, nil  // Output message inherits metadata
    },
)

// In error handler (nack function):
if err != nil {
    metadata := gopipe.MetadataFromError(err)
    log.Printf("Error for message %v: %v", metadata["id"], err)
}
```

### Pattern 4: Complete Pipeline

```go
// Source: Generate from Service Bus
source := gopipe.NewGenerator(
    func(ctx context.Context) ([]Message[[]byte], error) {
        return client.ReceiveMessages(ctx, 10)
    },
    gopipe.WithConcurrency[struct{}, Message[[]byte]](1),  // 1 receiver
)

// Stage 1: Parse and validate
validator := gopipe.NewMessagePipe(
    func(ctx context.Context, data []byte) ([]Request, error) {
        var req Request
        if err := json.Unmarshal(data, &req); err != nil {
            return nil, err  // Nack invalid message
        }
        return []Request{req}, nil
    },
    gopipe.WithConcurrency[Message[[]byte], Message[Request]](5),
)

// Stage 2: Process business logic
processor := gopipe.NewMessagePipe(
    func(ctx context.Context, req Request) ([]Response, error) {
        result, err := businessLogic(ctx, req)
        if err != nil {
            return nil, err  // Nack and retry
        }
        return []Response{result}, nil
    },
    gopipe.WithConcurrency[Message[Request], Message[Response]](10),
    gopipe.WithRetryConfig(
        gopipe.ExponentialBackoff(...),
        gopipe.ShouldRetry(),
    ),
)

// Sink: Publish results back to Service Bus
publisher := gopipe.NewSinkPipe(
    func(ctx context.Context, msg Message[Response]) error {
        data, _ := json.Marshal(msg.Payload)
        return client.SendMessage(ctx, "output-topic", data)
    },
    gopipe.WithConcurrency[Message[Response], struct{}](5),
)

// Wire together:
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

messages := source.Generate(ctx)
validated := validator.Start(ctx, messages)
processed := processor.Start(ctx, validated)
done := publisher.Start(ctx, processed)

<-done  // Wait for all messages processed
```

## Configuration Options You'll Use

```go
// Concurrency: Process N items in parallel
WithConcurrency[In, Out](5)

// Buffer: Channel buffer size
WithBuffer[In, Out](100)

// Timeout: Per-message processing timeout
WithTimeout[In, Out](30*time.Second)

// Retry: Transient error recovery
WithRetryConfig[In, Out](backoff, shouldRetry)

// Panic safety
WithRecover[In, Out]()

// Custom cancellation handling
WithCancel[In, Out](func(in In, err error) {
    // Called when processing fails or context cancels
    // For Message[T]: ack/nack already handled automatically
})

// Metadata enrichment
WithMetadataProvider[In, Out](func(in In) Metadata {
    return Metadata{"key": "value"}
})

// Logging
WithLogConfig[In, Out](&LogConfig{
    Level: slog.LevelInfo,
})

// Metrics
WithMetricsCollector[In, Out](collector)
```

## Critical Design Decisions

1. **Message[T] handles ack/nack automatically in NewMessagePipe**
   - Success → ack() called
   - Error → nack(err) called
   - You don't need to call them manually in the handler

2. **Errors trigger automatic nack**
   - Processing error → nack'd
   - Context cancel/timeout → nack'd
   - Framework handles it via WithCancel middleware

3. **Metadata is preserved through pipeline**
   - Input message metadata flows to output messages
   - Available in context for logging/metrics
   - Attached to errors

4. **Generator for source, SinkPipe for terminal**
   - Generator: 0 inputs → N outputs (pull model)
   - SinkPipe: N inputs → no output (push model)
   - Pipes can be chained: generator → pipe → ... → pipe → sinkpipe

5. **No output buffer for parallelism delegation**
   - Per CLAUDE.md: Use no output buffer (buffer=0)
   - Downstream workers handle their own concurrency
   - Each stage is independent

## Implementation Checklist for Azure Service Bus

- [ ] Create `Subscriber[T]` using `NewGenerator`
- [ ] Create `Publisher[T]` using `NewSinkPipe`
- [ ] Map Azure Service Bus ReceivedMessage → gopipe.Message[T]
- [ ] Implement ack/nack handlers → Complete/Abandon on broker
- [ ] Handle metadata from Service Bus properties
- [ ] Configure retry for transient errors (network, timeouts)
- [ ] Configure concurrency for throughput
- [ ] Handle dead-letter queue for failed messages
- [ ] Add proper logging with metadata
- [ ] Add metrics collection
- [ ] Test at-least-once delivery semantics
