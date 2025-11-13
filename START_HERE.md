# Gopipe Framework - START HERE

This document provides a quick orientation to the gopipe framework documentation created for the gopipe-azservicebus project.

## Documentation Structure

### For Quick Understanding (Start Here)
**File:** `GOPIPE_KEY_INSIGHTS.md`

Read this first (10-15 minutes):
- Quick reference for Message, Processor, Pipe, Generator interfaces
- 4 practical implementation patterns with complete code
- Configuration options cheat sheet
- 5 critical design decisions
- Implementation checklist for Azure Service Bus

### For Comprehensive Reference
**File:** `GOPIPE_FRAMEWORK_GUIDE.md`

Read this for deep understanding and as a reference while coding:
- Complete Message interface documentation with all methods
- Ack/Nack pattern and at-least-once delivery semantics
- All 6 pipe types (Transform, Process, Filter, Batch, Sink, Message)
- Generator and SinkPipe patterns
- Full pipeline composition examples
- Error handling strategies
- Configuration options with examples
- Copy message pattern for payload transformation

### For Project Requirements
**File:** `CLAUDE.md`

Read this to understand project-specific constraints:
- Architecture expectations
- Integration points with gopipe
- Publisher/Subscriber interface specifications
- Error handling requirements
- Concurrency guidelines

## The Core Concept in 30 Seconds

Gopipe implements the **adapter pattern** for data pipelines:

```
Generator         MessagePipe        MessagePipe        SinkPipe
(Source)        (Transform)        (Process)         (Terminal)
   |                 |                 |                  |
   | Pull messages   | Process with   | Publish         | Send to
   | from broker     | ack/nack       | results         | broker
   |                 |                 |                  |
   ↓                 ↓                 ↓                  ↓
chan Message[T] ← Message[T] ← Message[T] ← done signal
```

### Key Points

1. **Message[T]** - Carries data + ack/nack handlers
2. **Processor** - Transforms input to output (0...N items)
3. **Pipe** - Configures and runs processors
4. **Generator** - Source stage (pull model)
5. **SinkPipe** - Terminal stage (push model)
6. **At-least-once delivery** - Via automatic ack/nack

## The Message[T] Type

```go
type Message[T any] struct {
    Metadata Metadata        // Key-value metadata map
    Payload  T               // Your actual data
    
    // Methods:
    Ack()                    // Success: remove from broker
    Nack(err)                // Failure: requeue/deadletter
    Deadline() time.Time     // Get message deadline
    SetTimeout(duration)     // Set deadline from now
}
```

**Critical insight:** In `NewMessagePipe`, you don't call Ack/Nack yourself. The framework does it automatically:
- Success → ack() called
- Error → nack(err) called
- This keeps your code clean and simple

## The Three Core Interfaces

### 1. Processor[In, Out]
Implements business logic:
```go
type Processor[In, Out any] interface {
    Process(ctx context.Context, In) ([]Out, error)
    Cancel(In, error)
}
```
Usually created via pipe factory functions, not directly.

### 2. Pipe[Pre, Out]
Configures and orchestrates processing:
```go
type Pipe[Pre, Out any] interface {
    Start(ctx context.Context, pre <-chan Pre) <-chan Out
}
```
Created via: NewTransformPipe, NewProcessPipe, NewFilterPipe, NewBatchPipe, NewSinkPipe, NewMessagePipe

### 3. Generator[Out]
Source stage (pull model):
```go
type Generator[Out any] interface {
    Generate(ctx context.Context) <-chan Out
}
```
Creates: NewGenerator with a function that's called repeatedly

## For Azure Service Bus: You Need

### 1. Subscriber[T] - Source Stage
```go
type Subscriber[T any] interface {
    Subscribe(ctx context.Context, topic string) (<-chan Message[T], error)
}
```
**Implementation:** Use `gopipe.NewGenerator` to repeatedly fetch messages from Service Bus

### 2. Publisher[T] - Sink Stage
```go
type Publisher[T any] interface {
    Publish(ctx context.Context, topic string, msgs <-chan Message[T]) (<-chan struct{}, error)
}
```
**Implementation:** Use `gopipe.NewSinkPipe` to send messages to Service Bus

### 3. Message Mapping
Convert Azure SDK messages to gopipe.Message[T]:
```go
gopipe.NewMessage(
    metadata,           // Extract from Azure properties
    payloadData,        // Message body
    deadline,           // Message TTL
    ackFunc,            // Complete in Azure
    nackFunc,           // Abandon in Azure
)
```

## Quick Decision Guide

| I want to... | Use this | Reference |
|---|---|---|
| Understand ack/nack pattern | NewMessagePipe | GOPIPE_KEY_INSIGHTS.md - Pattern 1 |
| Set up error retry | WithRetryConfig | GOPIPE_KEY_INSIGHTS.md - Pattern 2 |
| Log message metadata | WithMetadataProvider | GOPIPE_KEY_INSIGHTS.md - Pattern 3 |
| Build complete pipeline | See examples | GOPIPE_KEY_INSIGHTS.md - Pattern 4 |
| Configure concurrency | WithConcurrency | GOPIPE_KEY_INSIGHTS.md - Config section |
| Handle transformation | NewTransformPipe | GOPIPE_FRAMEWORK_GUIDE.md - Pipe Factory |
| Batch messages | NewBatchPipe | GOPIPE_FRAMEWORK_GUIDE.md - Batch Pipe |
| Filter messages | NewFilterPipe | GOPIPE_FRAMEWORK_GUIDE.md - Filter Pipe |
| Deep understanding | Read all | GOPIPE_FRAMEWORK_GUIDE.md |

## Most Important Concepts for Your Use Case

1. **Message[T] has built-in ack/nack** - Don't manually call these in handlers
2. **NewMessagePipe handles errors automatically** - Return error, get nack'd
3. **Metadata flows through pipeline** - Automatically propagated to output messages
4. **Generator for pull-based sources** - Perfect for Service Bus consumer
5. **SinkPipe for terminal sinks** - Perfect for Service Bus publisher
6. **WithRetryConfig for transient errors** - Configure retry strategy once
7. **WithConcurrency controls parallelism** - Set per stage independently
8. **No output buffer** - Per CLAUDE.md requirement, let downstream handle concurrency

## Reading Recommendations

### Scenario: "I need to implement this quickly"
1. Read this file (5 min)
2. Read GOPIPE_KEY_INSIGHTS.md (15 min)
3. Start implementing with GOPIPE_KEY_INSIGHTS.md as reference

### Scenario: "I need to understand the framework deeply"
1. Read GOPIPE_KEY_INSIGHTS.md (quick overview - 15 min)
2. Read GOPIPE_FRAMEWORK_GUIDE.md (deep dive - 30 min)
3. Review CLAUDE.md for project constraints (5 min)
4. Look at gopipe/examples/ for real examples (10 min)

### Scenario: "I'm stuck implementing something"
1. Search GOPIPE_FRAMEWORK_GUIDE.md for the concept
2. Find code examples in the same file
3. Check gopipe/examples/ for working implementations
4. Consult GOPIPE_KEY_INSIGHTS.md for common patterns

## Key Files Location

```
/home/sml/git/github.com/fxsml/gopipe-azservicebus/
├── START_HERE.md                      ← You are here
├── GOPIPE_KEY_INSIGHTS.md             (Quick reference)
├── GOPIPE_FRAMEWORK_GUIDE.md          (Deep reference)
├── CLAUDE.md                          (Project requirements)
└── README.md                          (To be filled)

/home/sml/git/github.com/fxsml/gopipe/
├── message.go                         (Message[T] source)
├── processor.go                       (Processor interface)
├── pipe.go                            (Pipe factory functions)
├── generator.go                       (Generator interface)
└── examples/                          (Working examples)
    ├── generator/main.go
    ├── message-ack-nack/main.go
    ├── transform-pipe/main.go
    └── batch-pipe/main.go
```

## The Mental Model

Think of gopipe as a **pipeline factory**:

1. **Define your stages** - Each stage is a Pipe or Generator
2. **Configure each stage** - Concurrency, buffer, timeout, retry, etc.
3. **Wire them together** - Output of one feeds input of next
4. **Let it run** - Framework handles concurrency, cancellation, error handling
5. **Messages flow through** - With automatic ack/nack, metadata propagation
6. **On completion** - Terminal sink signals done via channel

The framework handles:
- Goroutine management
- Error propagation
- Message acknowledgment
- Context cancellation
- Metadata tracking
- Automatic retries (configurable)
- Graceful shutdown

You provide:
- Business logic (handler functions)
- Configuration (options)
- Message mapping (Azure SDK ↔ gopipe.Message)
- Ack/nack semantics (usually just call Complete/Abandon)

## Next Step

Open `GOPIPE_KEY_INSIGHTS.md` and read the "Quick Reference: Essential Interfaces" section.

