# API Reference

Complete API documentation for `gopipe-azservicebus`.

## Package `azservicebus`

```go
import azservicebus "github.com/fxsml/gopipe-azservicebus"
```

---

## Client Management

### NewClient

Creates a new Azure Service Bus client.

```go
func NewClient(connOrNamespace string) (*azservicebus.Client, error)
```

**Parameters:**
- `connOrNamespace` - Either a connection string (starts with "Endpoint=") or a namespace URL

**Returns:**
- Azure Service Bus client
- Error if connection fails

**Example:**
```go
// Using connection string
client, err := azservicebus.NewClient("Endpoint=sb://your-namespace.servicebus.windows.net/;SharedAccessKeyName=...;SharedAccessKey=...")

// Using namespace with DefaultAzureCredential
client, err := azservicebus.NewClient("your-namespace.servicebus.windows.net")
```

### NewClientWithConfig

Creates a new Azure Service Bus client with custom configuration.

```go
func NewClientWithConfig(connOrNamespace string, config ClientConfig) (*azservicebus.Client, error)
```

**ClientConfig:**
```go
type ClientConfig struct {
    MaxRetries    int32         // Default: 5
    RetryDelay    time.Duration // Default: 1 second
    MaxRetryDelay time.Duration // Default: 60 seconds
}
```

---

## Sender

The Sender provides low-level batch send operations with connection pooling to multiple destinations.

### NewSender

Creates a new Azure Service Bus sender.

```go
func NewSender(client *azservicebus.Client, config SenderConfig) *Sender
```

**SenderConfig:**
```go
type SenderConfig struct {
    SendTimeout  time.Duration // Default: 30s
    CloseTimeout time.Duration // Default: 30s
}
```

### Sender.Send

Sends a batch of messages to a queue or topic.

```go
func (s *Sender) Send(ctx context.Context, queueOrTopic string, msgs []*message.Message[[]byte]) error
```

**Parameters:**
- `ctx` - Context for cancellation
- `queueOrTopic` - Destination queue or topic name
- `msgs` - Slice of messages to send

**Returns:**
- Error if send fails

**Example:**
```go
sender := azservicebus.NewSender(client, azservicebus.SenderConfig{})
defer sender.Close()

msgs := []*message.Message[[]byte]{
    message.New([]byte("message 1")),
    message.New([]byte("message 2")),
}

err := sender.Send(ctx, "my-queue", msgs)
```

### Sender.Close

Closes the sender and releases resources.

```go
func (s *Sender) Close() error
```

---

## Receiver

The Receiver provides low-level batch receive operations with connection pooling.

### NewReceiver

Creates a new Azure Service Bus receiver.

```go
func NewReceiver(client *azservicebus.Client, config ReceiverConfig) *Receiver
```

**ReceiverConfig:**
```go
type ReceiverConfig struct {
    ReceiveTimeout  time.Duration // Default: 30s
    AckTimeout      time.Duration // Default: 30s
    CloseTimeout    time.Duration // Default: 30s
    MaxMessageCount int           // Default: 10
}
```

### Receiver.Receive

Receives a batch of messages from a queue or topic subscription.

```go
func (r *Receiver) Receive(ctx context.Context, queueOrTopic string) ([]*message.Message[[]byte], error)
```

**Parameters:**
- `ctx` - Context for cancellation
- `queueOrTopic` - Queue name or "topic/subscription" format

**Returns:**
- Slice of received messages
- Error if receive fails

**Example:**
```go
receiver := azservicebus.NewReceiver(client, azservicebus.ReceiverConfig{
    MaxMessageCount: 10,
})
defer receiver.Close()

// From a queue
msgs, err := receiver.Receive(ctx, "my-queue")

// From a topic subscription
msgs, err := receiver.Receive(ctx, "my-topic/my-subscription")

for _, msg := range msgs {
    // Process message
    msg.Ack()
}
```

### Receiver.Close

Closes the receiver and releases resources.

```go
func (r *Receiver) Close() error
```

---

## gopipe Integration Helpers

### NewMessageGenerator

Creates a gopipe Generator that produces messages from the Receiver.

```go
func NewMessageGenerator(receiver *Receiver, queueOrTopic string, opts ...gopipe.Option[struct{}, *message.Message[[]byte]]) gopipe.Generator[*message.Message[[]byte]]
```

**Parameters:**
- `receiver` - The Receiver to use for fetching messages
- `queueOrTopic` - Queue name or "topic/subscription" format
- `opts` - Optional gopipe Generator options

**Returns:**
- A gopipe Generator that produces messages

**Example:**
```go
receiver := azservicebus.NewReceiver(client, azservicebus.ReceiverConfig{})
generator := azservicebus.NewMessageGenerator(receiver, "my-queue")

msgs := generator.Generate(ctx)
for msg := range msgs {
    // Process message
    msg.Ack()
}
```

### NewMessageSinkPipe

Creates a gopipe Pipe that sends messages using the Sender.

```go
func NewMessageSinkPipe(sender *Sender, queueOrTopic string, batchSize int, batchTimeout time.Duration, opts ...gopipe.Option[[]*message.Message[[]byte], struct{}]) gopipe.Pipe[*message.Message[[]byte], struct{}]
```

**Parameters:**
- `sender` - The Sender to use for sending messages
- `queueOrTopic` - Destination queue or topic name
- `batchSize` - Number of messages per batch
- `batchTimeout` - Maximum time to wait for a full batch
- `opts` - Optional gopipe Pipe options

**Returns:**
- A gopipe Pipe that batches and sends messages

**Example:**
```go
sender := azservicebus.NewSender(client, azservicebus.SenderConfig{})
sinkPipe := azservicebus.NewMessageSinkPipe(sender, "my-queue", 10, 100*time.Millisecond)

msgs := make(chan *message.Message[[]byte])
done := sinkPipe.Start(ctx, msgs)

// Send messages
go func() {
    defer close(msgs)
    msgs <- message.New([]byte("hello"))
}()

// Wait for completion
for range done {}
```

### NewMessageTransformSink

Creates a gopipe Pipe that transforms and sends messages.

```go
func NewMessageTransformSink[In any](
    sender *Sender,
    queueOrTopic string,
    batchSize int,
    batchTimeout time.Duration,
    transform func(context.Context, In) (*message.Message[[]byte], error),
    opts ...gopipe.Option[In, struct{}],
) gopipe.Pipe[In, struct{}]
```

**Parameters:**
- `sender` - The Sender to use for sending messages
- `queueOrTopic` - Destination queue or topic name
- `batchSize` - Number of messages per batch
- `batchTimeout` - Maximum time to wait for a full batch
- `transform` - Function to transform input to a message
- `opts` - Optional gopipe Pipe options

**Example:**
```go
type Order struct {
    ID     string
    Amount float64
}

sender := azservicebus.NewSender(client, azservicebus.SenderConfig{})
sinkPipe := azservicebus.NewMessageTransformSink(
    sender, "orders-queue", 10, 100*time.Millisecond,
    func(ctx context.Context, order Order) (*message.Message[[]byte], error) {
        body, err := json.Marshal(order)
        if err != nil {
            return nil, err
        }
        return message.New(body, message.WithID[[]byte](order.ID)), nil
    },
)

orders := make(chan Order)
done := sinkPipe.Start(ctx, orders)
```

---

## Message Properties

Messages use gopipe's `message.Message[[]byte]` type with these standard properties:

| Property | Method | Azure Service Bus Mapping |
|----------|--------|---------------------------|
| ID | `WithID[T]()` | MessageID |
| CorrelationID | `WithCorrelationID[T]()` | CorrelationID |
| Subject | `WithSubject[T]()` | Subject |
| ContentType | `WithContentType[T]()` | ContentType |
| ReplyTo | `WithReplyTo[T]()` | ReplyTo |
| TTL | `WithTTL[T]()` | TimeToLive |
| Custom | `WithProperty[T]()` | ApplicationProperties |

**Read-Only Properties (set by Azure):**
- CreatedAt (EnqueuedTime)
- RetryCount (DeliveryCount)
- Deadline (LockedUntil)
- SequenceNumber
- PartitionKey

**Example:**
```go
msg := message.New(body,
    message.WithID[[]byte]("id-123"),
    message.WithCorrelationID[[]byte]("corr-456"),
    message.WithSubject[[]byte]("order.created"),
    message.WithContentType[[]byte]("application/json"),
    message.WithTTL[[]byte](1*time.Hour),
    message.WithProperty[[]byte]("customer_id", "cust-789"),
)
```

---

## Acknowledgment

### Ack

Acknowledges successful message processing.

```go
func (m *Message[T]) Ack() bool
```

Internally calls `CompleteMessage` to remove the message from the queue.

### Nack

Rejects the message for redelivery.

```go
func (m *Message[T]) Nack(err error) bool
```

Internally calls `AbandonMessage` with error details. The message returns to the queue for redelivery.

**Example:**
```go
for _, msg := range msgs {
    if err := process(msg); err != nil {
        msg.Nack(err) // Will be redelivered
        continue
    }
    msg.Ack() // Removed from queue
}
```
