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

## Publisher

### NewPublisher

Creates a new Azure Service Bus publisher.

```go
func NewPublisher(client *azservicebus.Client, config PublisherConfig) *Publisher
```

**PublisherConfig:**
```go
type PublisherConfig struct {
    BatchSize    int           // Default: 10
    BatchTimeout time.Duration // Default: 100ms
    SendTimeout  time.Duration // Default: 30s
    CloseTimeout time.Duration // Default: 30s
}
```

### Publisher.Publish

Publishes messages from a channel asynchronously.

```go
func (p *Publisher) Publish(ctx context.Context, queueOrTopic string, msgs <-chan *message.Message[[]byte]) (<-chan struct{}, error)
```

**Parameters:**
- `ctx` - Context for cancellation
- `queueOrTopic` - Destination queue or topic name
- `msgs` - Input channel of messages

**Returns:**
- Done channel (closed when all messages sent)
- Error if publish fails to start

**Example:**
```go
msgs := make(chan *message.Message[[]byte])
done, err := publisher.Publish(ctx, "my-queue", msgs)

go func() {
    defer close(msgs)
    msgs <- message.New([]byte("hello"))
}()

<-done // Wait for completion
```

### Publisher.PublishSync

Publishes messages synchronously.

```go
func (p *Publisher) PublishSync(ctx context.Context, queueOrTopic string, msgs []*message.Message[[]byte]) error
```

### Publisher.Close

Closes the publisher and releases resources.

```go
func (p *Publisher) Close() error
```

---

## Subscriber

### NewSubscriber

Creates a new Azure Service Bus subscriber.

```go
func NewSubscriber(client *azservicebus.Client, config SubscriberConfig) *Subscriber
```

**SubscriberConfig:**
```go
type SubscriberConfig struct {
    ReceiveTimeout  time.Duration // Default: 30s
    AckTimeout      time.Duration // Default: 30s
    CloseTimeout    time.Duration // Default: 30s
    MaxMessageCount int           // Default: 10
    BufferSize      int           // Default: 100
    PollInterval    time.Duration // Default: 1s
}
```

### Subscriber.Subscribe

Subscribes to messages from a queue or topic subscription.

```go
func (s *Subscriber) Subscribe(ctx context.Context, queueOrTopic string) (<-chan *message.Message[[]byte], error)
```

**Parameters:**
- `ctx` - Context for cancellation
- `queueOrTopic` - Queue name or "topic/subscription" format

**Returns:**
- Output channel of messages
- Error if subscription fails

**Example:**
```go
// Queue
msgs, err := subscriber.Subscribe(ctx, "my-queue")

// Topic subscription
msgs, err := subscriber.Subscribe(ctx, "my-topic/my-subscription")

for msg := range msgs {
    // Process
    msg.Ack()
}
```

### Subscriber.Close

Closes the subscriber and releases resources.

```go
func (s *Subscriber) Close() error
```

---

## MultiPublisher

### NewMultiPublisher

Creates a publisher that can route to multiple destinations.

```go
func NewMultiPublisher(client *azservicebus.Client, config MultiPublisherConfig) *MultiPublisher
```

**MultiPublisherConfig:**
```go
type MultiPublisherConfig struct {
    PublisherConfig
    Concurrency int // Default: 1
}
```

### MultiPublisher.Publish

Publishes messages with custom routing.

```go
func (mp *MultiPublisher) Publish(
    ctx context.Context,
    msgs <-chan *message.Message[[]byte],
    router func(*message.Message[[]byte]) string,
) (<-chan struct{}, error)
```

**Parameters:**
- `ctx` - Context for cancellation
- `msgs` - Input channel of messages
- `router` - Function that returns destination for each message

**Example:**
```go
router := func(msg *message.Message[[]byte]) string {
    if val, ok := msg.Properties().Get("priority"); ok && val == "high" {
        return "high-priority-queue"
    }
    return "standard-queue"
}

done, err := multiPub.Publish(ctx, msgs, router)
```

---

## MultiSubscriber

### NewMultiSubscriber

Creates a subscriber for multiple sources.

```go
func NewMultiSubscriber(client *azservicebus.Client, config MultiSubscriberConfig) *MultiSubscriber
```

**MultiSubscriberConfig:**
```go
type MultiSubscriberConfig struct {
    SubscriberConfig
    MergeBufferSize int // Default: 1000
}
```

### MultiSubscriber.Subscribe

Subscribes to multiple sources with a single output channel.

```go
func (ms *MultiSubscriber) Subscribe(ctx context.Context, queuesOrTopics ...string) (<-chan *message.Message[[]byte], error)
```

**Example:**
```go
msgs, err := multiSub.Subscribe(ctx, "queue-1", "queue-2", "topic/subscription")

for msg := range msgs {
    // Messages from any source
    msg.Ack()
}
```

---

## Sender (Low-Level)

### NewSender

Creates a low-level sender.

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

Sends a batch of messages.

```go
func (s *Sender) Send(ctx context.Context, queueOrTopic string, msgs []*message.Message[[]byte]) error
```

---

## Receiver (Low-Level)

### NewReceiver

Creates a low-level receiver.

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

Receives a batch of messages.

```go
func (r *Receiver) Receive(ctx context.Context, queueOrTopic string) ([]*message.Message[[]byte], error)
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
for msg := range msgs {
    if err := process(msg); err != nil {
        msg.Nack(err) // Will be redelivered
        continue
    }
    msg.Ack() // Removed from queue
}
```

---

## Errors

### ErrSubscriberClosed

Returned when attempting to subscribe with a closed subscriber.

```go
var ErrSubscriberClosed = newError("subscriber is closed")
```

**Example:**
```go
msgs, err := subscriber.Subscribe(ctx, "queue")
if err == azservicebus.ErrSubscriberClosed {
    // Handle closed subscriber
}
```
