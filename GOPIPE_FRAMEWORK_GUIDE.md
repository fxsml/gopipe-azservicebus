# Gopipe Framework Guide: Understanding Core Interfaces and Patterns

## Overview

Gopipe is a lightweight, generic Go library for orchestrating complex data pipelines using composable pipes. It provides powerful orchestration primitives for building robust, concurrent, and context-aware pipelines.

## 1. Message Interface and Ack/Nack Pattern

### The Message Type

```go
type Message[T any] struct {
    Metadata Metadata          // Key-value store for additional information
    Payload  T                 // The actual data being processed
    deadline time.Time         // Optional message deadline
    
    mu   sync.Mutex           // Thread-safe access
    ack  func()               // Called on successful processing
    nack func(error)          // Called on failed processing
    ackType ackType           // Tracks whether message was ack'd or nack'd
}
```

### Constructor: NewMessage

```go
// Full constructor with all parameters
func NewMessage[T any](
    metadata Metadata,
    payload T,
    deadline time.Time,
    ack func(),
    nack func(error),
) *Message[T]

// Example usage:
msg := NewMessage(
    gopipe.Metadata{"id": "msg-1"},
    42,
    time.Time{},  // No deadline
    func() { 
        fmt.Println("Message acknowledged") 
    },
    func(err error) { 
        fmt.Printf("Message nacked: %v\n", err) 
    },
)
```

### Helper Constructors (to be implemented for Azure Service Bus)

These are commonly expected patterns based on the test examples:

```go
// NewMessageWithAck - Convenience constructor with metadata and ID
// (These need to be implemented)
func NewMessageWithAck[T any](
    id string,
    payload T,
    ack func(),
    nack func(error),
) *Message[T] {
    return NewMessage(
        Metadata{"id": id},
        payload,
        time.Time{},
        ack,
        nack,
    )
}

// NewMessage - Simple constructor without ack/nack
func NewMessage[T any](id string, payload T) *Message[T] {
    return NewMessage(
        Metadata{"id": id},
        payload,
        time.Time{},
        nil,  // No ack handler
        nil,  // No nack handler
    )
}
```

### Ack/Nack Methods

```go
// Ack attempts to acknowledge the message
// Returns true if successful, false if already ack'd/nack'd or no ack handler
func (m *Message[T]) Ack() bool

// Nack attempts to negatively acknowledge the message with an error
// Returns true if successful, false if already ack'd/nack'd or no nack handler
func (m *Message[T]) Nack(err error) bool

// Example:
func processMessage(msg *Message[int]) {
    result := msg.Payload * 2
    if result > 100 {
        msg.Nack(fmt.Errorf("result too large: %d", result))
    } else {
        msg.Ack()
    }
}
```

### Message Deadline

```go
// Set a timeout on the message (deadline from now)
msg.SetTimeout(50 * time.Millisecond)

// Get the message deadline
deadline := msg.Deadline()

// How it works:
// - When SetTimeout is called, it calculates: deadline = now + timeout
// - NewMessagePipe automatically applies this deadline to the processing context
// - If processing exceeds the deadline, context.DeadlineExceeded is triggered
// - The message is automatically nack'd with the deadline error
```

### Metadata

```go
// Metadata is a key-value store (map[string]any)
type Metadata map[string]any

// Convert to logging args
args := metadata.Args()  // Returns []any{key1, val1, key2, val2, ...}

// Extract metadata from context (set by framework)
metadata := gopipe.MetadataFromContext(ctx)

// Extract metadata from error (attached by middleware)
metadata := gopipe.MetadataFromError(err)
```

### Key Pattern: At-Least-Once Delivery

```go
// The ack/nack pattern implements at-least-once delivery:
// 1. Message is received from broker
// 2. Processing begins with ack/nack handlers registered
// 3. If successful: ack() is called → broker removes message
// 4. If error: nack(err) is called → broker may retry
// 5. If cancelled/timeout: nack(err) is called automatically
// 6. If no handlers: works fine, no acknowledgment needed

func exampleMessageBroker() {
    messages := []int{10, 20, -1, 30}  // -1 will fail
    
    in := make(chan *gopipe.Message[int], len(messages))
    
    for i, payload := range messages {
        id := fmt.Sprintf("msg-%d", i)
        in <- gopipe.NewMessageWithAck(
            id,
            payload,
            func() { fmt.Printf("✓ %s acknowledged\n", id) },
            func(err error) { fmt.Printf("✗ %s nacked: %v\n", id, err) },
        )
    }
    close(in)
    
    // Create processing pipe
    pipe := gopipe.NewMessagePipe(
        func(ctx context.Context, value int) ([]int, error) {
            if value < 0 {
                return nil, errors.New("negative value not allowed")
            }
            return []int{value * 2}, nil
        },
    )
    
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    out := pipe.Start(ctx, in)
    for result := range out {
        fmt.Printf("→ Processed value: %d\n", result.Payload)
    }
}
```

## 2. Core Pipeline Interfaces

### Processor Interface

The fundamental unit of work in gopipe:

```go
// Processor combines processing and cancellation logic
type Processor[In, Out any] interface {
    // Process handles single input and returns zero or more outputs
    Process(context.Context, In) ([]Out, error)
    
    // Cancel handles errors when processing fails
    Cancel(In, error)
}

// Create a processor:
proc := gopipe.NewProcessor(
    // ProcessFunc
    func(ctx context.Context, in int) ([]int, error) {
        return []int{in * 2}, nil
    },
    // Optional CancelFunc
    func(in int, err error) {
        log.Printf("Processing failed for %d: %v", in, err)
    },
)
```

### Pipe Interface

Orchestrates processing with configuration:

```go
type Pipe[Pre, Out any] interface {
    // Start begins processing from input channel
    Start(ctx context.Context, pre <-chan Pre) <-chan Out
}
```

### Pipe Factory Functions

**1. Transform Pipe - One input → One output**

```go
// Each input produces exactly one output
pipe := gopipe.NewTransformPipe(
    func(ctx context.Context, val string) (int, error) {
        return strconv.Atoi(val)
    },
    gopipe.WithConcurrency[string, int](5),
    gopipe.WithBuffer[string, int](10),
)

out := pipe.Start(ctx, in)  // <-chan int
```

**2. Process Pipe - One input → Zero/Many outputs**

```go
// Each input can produce 0, 1, or many outputs
pipe := gopipe.NewProcessPipe(
    func(ctx context.Context, val int) ([]int, error) {
        if val < 0 {
            return nil, errors.New("negative")  // 0 outputs
        }
        if val == 0 {
            return []int{}, nil  // 0 outputs
        }
        return []int{val, val * 2}, nil  // 2 outputs
    },
)

out := pipe.Start(ctx, in)  // <-chan int
```

**3. Filter Pipe - One input → Same output (pass/drop)**

```go
pipe := gopipe.NewFilterPipe(
    func(ctx context.Context, val int) (bool, error) {
        return val%2 == 0, nil  // Only even numbers pass
    },
)

out := pipe.Start(ctx, in)  // <-chan int (only even values)
```

**4. Batch Pipe - Group inputs, process batches**

```go
pipe := gopipe.NewBatchPipe(
    func(ctx context.Context, batch []string) ([]int, error) {
        // Process entire batch at once
        results := make([]int, len(batch))
        for i, s := range batch {
            n, _ := strconv.Atoi(s)
            results[i] = n
        }
        return results, nil
    },
    5,                   // Max batch size
    100*time.Millisecond, // Max wait time
)

out := pipe.Start(ctx, in)  // <-chan int
```

**5. Sink Pipe - Consume without producing output**

```go
pipe := gopipe.NewSinkPipe(
    func(ctx context.Context, val int) error {
        fmt.Println(val)
        return nil
    },
)

done := pipe.Start(ctx, in)  // <-chan struct{} (signals completion)
<-done  // Wait for completion
```

**6. Message Pipe - Special pipe for Message[T]**

```go
pipe := gopipe.NewMessagePipe(
    func(ctx context.Context, payload int) ([]int, error) {
        return []int{payload * 2}, nil
    },
)

// Input MUST be chan *Message[int]
// Output is chan *Message[int]
// - On success: msg.Ack() is called automatically
// - On error: msg.Nack(err) is called automatically
// - Metadata from input message is propagated to output

in := make(chan *gopipe.Message[int])
out := pipe.Start(ctx, in)  // <-chan *Message[int]
```

## 3. Generator and SinkPipe Patterns

### Generator - Source Stage

Generates values from nothing (repeatedly calls a function):

```go
// Generator interface
type Generator[Out any] interface {
    Generate(ctx context.Context) <-chan Out
}

// Create a generator
gen := gopipe.NewGenerator(
    func(ctx context.Context) ([]int, error) {
        // Called repeatedly until ctx cancellation or error
        // Can return multiple values per call
        return []int{rand.Intn(100)}, nil
    },
    gopipe.WithConcurrency[struct{}, int](1),
)

// Use it
out := gen.Generate(ctx)  // <-chan int

// Example: Message broker consumer
brokerGen := gopipe.NewGenerator(
    func(ctx context.Context) ([]Message[string], error) {
        // Fetch batch from service bus
        messages, err := serviceBusClient.Receive(ctx, 10)
        if err != nil {
            return nil, err
        }
        return messages, nil
    },
    gopipe.WithConcurrency[struct{}, Message[string]](5),
)

messages := brokerGen.Generate(ctx)
```

### SinkPipe - Terminal Stage

Consumes values without producing output:

```go
// Create a sink pipe
pipe := gopipe.NewSinkPipe(
    func(ctx context.Context, val string) error {
        // Publish to service bus
        return serviceBusClient.Publish(ctx, val)
    },
    gopipe.WithConcurrency[string, struct{}](5),
)

// Use it
done := pipe.Start(ctx, in)  // <-chan struct{}
<-done  // Wait for all values to be published
```

## 4. Pipeline Composition Pattern

```go
// Source: Generate from Azure Service Bus
consumer := gopipe.NewGenerator(
    func(ctx context.Context) ([]Message[string], error) {
        return serviceBusClient.ReceiveMessages(ctx)
    },
)

// Stage 1: Transform messages
transform := gopipe.NewMessagePipe(
    func(ctx context.Context, data string) ([]string, error) {
        return []string{strings.ToUpper(data)}, nil
    },
)

// Stage 2: Filter
filter := gopipe.NewFilterPipe(
    func(ctx context.Context, msg *Message[string]) (bool, error) {
        return msg.Payload != "", nil
    },
)

// Stage 3: Batch and publish
publisher := gopipe.NewBatchPipe(
    func(ctx context.Context, batch []*Message[string]) ([]struct{}, error) {
        return nil, serviceBusClient.PublishBatch(ctx, batch)
    },
    20,                  // Batch size
    100*time.Millisecond, // Max duration
)

// Wire together:
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

messages := consumer.Generate(ctx)
transformed := transform.Start(ctx, messages)
filtered := filter.Start(ctx, transformed)
done := publisher.Start(ctx, filtered)

<-done  // Wait for completion
```

## 5. Error Handling Patterns

### Automatic Error Handling with Messages

```go
// NewMessagePipe automatically:
// 1. Nacks on processing error
// 2. Nacks on context cancellation
// 3. Acks on success
// 4. Propagates metadata to error context

pipe := gopipe.NewMessagePipe(
    func(ctx context.Context, data int) ([]int, error) {
        if err := doSomething(ctx, data); err != nil {
            // nack(err) will be called automatically by framework
            return nil, err
        }
        // ack() will be called automatically by framework
        return []int{data}, nil
    },
)
```

### Error Extraction

```go
// Errors are wrapped to maintain causality
// ErrFailure: Processing error
// ErrCancel: Context cancellation error

// Extract original cause:
var originalErr error
if errors.Is(err, gopipe.ErrFailure) {
    originalErr = errors.Unwrap(err)
}

// Get metadata from error:
metadata := gopipe.MetadataFromError(err)
if metadata != nil {
    log.Printf("Error for message %v: %v", metadata["id"], err)
}
```

## 6. Options and Configuration

### Common Options

```go
// Concurrency: How many workers process in parallel
gopipe.WithConcurrency[In, Out](5)

// Buffer: Size of output channel buffer (0 = unbuffered)
gopipe.WithBuffer[In, Out](100)

// Timeout: Per-item processing timeout
gopipe.WithTimeout[In, Out](5*time.Second)

// Context Propagation: Whether to pass parent context
gopipe.WithoutContextPropagation[In, Out]()

// Cancel Handler: Custom cancellation logic
gopipe.WithCancel[In, Out](func(in In, err error) {
    log.Printf("Canceled: %v", err)
})

// Retry: Automatic retry on failure
gopipe.WithRetryConfig[In, Out](
    gopipe.ConstantBackoff(100*time.Millisecond, 0.1),
    gopipe.ShouldRetry(), // Retry all errors
)

// Recover: Panic recovery
gopipe.WithRecover[In, Out]()

// Logging
gopipe.WithLogConfig[In, Out](&gopipe.LogConfig{
    Level: slog.LevelInfo,
})

// Metrics
gopipe.WithMetricsCollector[In, Out](metricsCollector)

// Metadata Provider: Enrich context with metadata
gopipe.WithMetadataProvider[In, Out](func(in In) gopipe.Metadata {
    return gopipe.Metadata{"source": "service-bus"}
})
```

### Example Configuration

```go
pipe := gopipe.NewMessagePipe(
    handler,
    gopipe.WithConcurrency[*Message[string], *Message[string]](10),
    gopipe.WithBuffer[*Message[string], *Message[string]](100),
    gopipe.WithTimeout[*Message[string], *Message[string]](30*time.Second),
    gopipe.WithRetryConfig[*Message[string], *Message[string]](
        gopipe.ExponentialBackoff(
            100*time.Millisecond,  // initial delay
            2.0,                   // factor
            5*time.Second,         // max delay
            0.1,                   // jitter
        ),
        gopipe.ShouldRetry(),
    ),
    gopipe.WithRecover[*Message[string], *Message[string]](),
    gopipe.WithMetadataProvider[*Message[string], *Message[string]](
        func(msg *Message[string]) gopipe.Metadata {
            return msg.Metadata
        },
    ),
)
```

## 7. Copy Message Pattern

When transforming message payloads while preserving ack/nack handlers:

```go
// Copy a message with different payload type
originalMsg := *Message[string]  // Original message
newMsg := gopipe.CopyMessage(originalMsg, newPayload)

// The new message has:
// - Same metadata
// - Same deadline
// - Same ack/nack handlers
// - New payload (different type)

// Used in NewMessagePipe internally:
func handler(ctx context.Context, msg *Message[int]) ([]*Message[int], error) {
    results, err := processPayload(msg.Payload)
    if err != nil {
        msg.Nack(err)
        return nil, err
    }
    
    msg.Ack()
    
    // Create output messages preserving ack/nack
    var messages []*Message[int]
    for _, result := range results {
        messages = append(messages, gopipe.CopyMessage(msg, result))
    }
    return messages, nil
}
```

## Summary: Implementation Checklist for Azure Service Bus

For `gopipe-azservicebus`, you'll need:

### 1. Subscriber (Source Stage)
```go
type Subscriber[T any] interface {
    Subscribe(ctx context.Context, topic string) (<-chan Message[T], error)
}

// Implementation approach:
// - Use gopipe.NewGenerator to repeatedly fetch from Service Bus
// - Create Message[T] with ack/nack handlers that call Complete/Abandon on broker
// - Handle metadata from Service Bus properties
```

### 2. Publisher (Sink Stage)
```go
type Publisher[T any] interface {
    Publish(ctx context.Context, topic string, msgs <-chan Message[T]) (<-chan struct{}, error)
}

// Implementation approach:
// - Use gopipe.NewSinkPipe or similar pattern
// - Send messages to Service Bus
// - Call Ack on successful send
// - Call Nack on send failure
```

### 3. Message Mapping
```go
// Convert Azure Service Bus message to gopipe.Message
func toGopipeMessage(sbMsg *azservicebus.ReceivedMessage) *gopipe.Message[[]byte] {
    return gopipe.NewMessageWithAck(
        sbMsg.MessageID,
        sbMsg.Body,
        func() { /* Complete in broker */ },
        func(err error) { /* Abandon in broker */ },
    )
}
```

### 4. Error Handling
- Use `gopipe.WithRetryConfig` for transient errors
- Implement `gopipe.ShouldRetry` to distinguish retryable from fatal errors
- Use dead-letter queue for messages that fail after retries
