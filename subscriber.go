package azservicebus

import (
	"context"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe/message"
)

// Subscriber subscribes to messages from Azure Service Bus queues and topics using a channel-based API.
// It implements the Subscriber interface from CLAUDE.md specification.
type Subscriber struct {
	receiver *Receiver
	config   SubscriberConfig
	closeMu  sync.RWMutex
	closed   bool
}

// SubscriberConfig holds configuration for the Azure Service Bus subscriber
type SubscriberConfig struct {
	// ReceiveTimeout is the timeout for receive operations.
	// Default: 30 seconds
	ReceiveTimeout time.Duration

	// AckTimeout is the timeout for acknowledging messages.
	// Default: 30 seconds
	AckTimeout time.Duration

	// CloseTimeout is the timeout for closing the subscriber.
	// Default: 30 seconds
	CloseTimeout time.Duration

	// MaxMessageCount is the maximum number of messages to receive in a single batch.
	// Default: 10
	MaxMessageCount int

	// BufferSize is the size of the output channel buffer.
	// Default: 100
	BufferSize int

	// PollInterval is the interval between receive attempts when no messages are available.
	// Default: 1 second
	PollInterval time.Duration
}

// setDefaults sets default values for SubscriberConfig
func (c *SubscriberConfig) setDefaults() {
	if c.ReceiveTimeout <= 0 {
		c.ReceiveTimeout = 30 * time.Second
	}
	if c.AckTimeout <= 0 {
		c.AckTimeout = 30 * time.Second
	}
	if c.CloseTimeout <= 0 {
		c.CloseTimeout = 30 * time.Second
	}
	if c.MaxMessageCount <= 0 {
		c.MaxMessageCount = 10
	}
	if c.BufferSize <= 0 {
		c.BufferSize = 100
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 1 * time.Second
	}
}

// NewSubscriber creates a new Azure Service Bus subscriber.
func NewSubscriber(client *azservicebus.Client, config SubscriberConfig) *Subscriber {
	config.setDefaults()

	receiver := NewReceiver(client, ReceiverConfig{
		ReceiveTimeout:  config.ReceiveTimeout,
		AckTimeout:      config.AckTimeout,
		CloseTimeout:    config.CloseTimeout,
		MaxMessageCount: config.MaxMessageCount,
	})

	return &Subscriber{
		receiver: receiver,
		config:   config,
	}
}

// Subscribe subscribes to messages from the specified Azure Service Bus queue or topic subscription.
// It returns an output channel that streams messages as they are received.
//
// The queueOrTopic parameter should be:
//   - A queue name for queue subscriptions (e.g., "my-queue")
//   - A topic/subscription path for topic subscriptions (e.g., "my-topic/my-subscription")
//
// The output channel is closed when the context is cancelled or when Close() is called.
// Messages must be acknowledged (Ack) or rejected (Nack) after processing.
//
// Usage:
//
//	subscriber := NewSubscriber(client, SubscriberConfig{})
//	msgs, err := subscriber.Subscribe(ctx, "my-queue")
//	if err != nil {
//	    // handle error
//	}
//	for msg := range msgs {
//	    // Process message
//	    payload := msg.Payload()
//	    // ...
//	    msg.Ack() // Acknowledge successful processing
//	}
func (s *Subscriber) Subscribe(ctx context.Context, queueOrTopic string) (<-chan *message.Message[[]byte], error) {
	s.closeMu.RLock()
	if s.closed {
		s.closeMu.RUnlock()
		return nil, ErrSubscriberClosed
	}
	s.closeMu.RUnlock()

	out := make(chan *message.Message[[]byte], s.config.BufferSize)

	go func() {
		defer close(out)
		s.receiveLoop(ctx, queueOrTopic, out)
	}()

	return out, nil
}

// receiveLoop continuously receives messages and sends them to the output channel
func (s *Subscriber) receiveLoop(ctx context.Context, queueOrTopic string, out chan<- *message.Message[[]byte]) {
	for {
		// Check if subscriber is closed
		s.closeMu.RLock()
		closed := s.closed
		s.closeMu.RUnlock()
		if closed {
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		// Receive messages
		messages, err := s.receiver.Receive(ctx, queueOrTopic)
		if err != nil {
			// Check if context is cancelled
			if ctx.Err() != nil {
				return
			}
			// On error, wait before retrying
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.config.PollInterval):
				continue
			}
		}

		// Send messages to output channel
		for _, msg := range messages {
			select {
			case <-ctx.Done():
				// Nack remaining messages before exiting
				for _, m := range messages {
					m.Nack(ctx.Err())
				}
				return
			case out <- msg:
			}
		}

		// If no messages received, wait before polling again
		if len(messages) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.config.PollInterval):
			}
		}
	}
}

// Close gracefully shuts down the subscriber
func (s *Subscriber) Close() error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return nil
	}
	s.closed = true
	s.closeMu.Unlock()

	return s.receiver.Close()
}

// MultiSubscriber subscribes to messages from multiple Azure Service Bus queues or topics concurrently.
type MultiSubscriber struct {
	client      *azservicebus.Client
	config      MultiSubscriberConfig
	subscribers []*Subscriber
	mu          sync.Mutex
	closeMu     sync.RWMutex
	closed      bool
}

// MultiSubscriberConfig holds configuration for the multi-subscriber
type MultiSubscriberConfig struct {
	// SubscriberConfig is embedded for single subscriber configuration
	SubscriberConfig

	// MergeBufferSize is the size of the merged output channel buffer.
	// Default: 1000
	MergeBufferSize int
}

// setDefaults sets default values for MultiSubscriberConfig
func (c *MultiSubscriberConfig) setDefaults() {
	c.SubscriberConfig.setDefaults()
	if c.MergeBufferSize <= 0 {
		c.MergeBufferSize = 1000
	}
}

// NewMultiSubscriber creates a new multi-source Azure Service Bus subscriber.
func NewMultiSubscriber(client *azservicebus.Client, config MultiSubscriberConfig) *MultiSubscriber {
	config.setDefaults()

	return &MultiSubscriber{
		client:      client,
		config:      config,
		subscribers: make([]*Subscriber, 0),
	}
}

// Subscribe subscribes to messages from multiple Azure Service Bus queues or topic subscriptions.
// Returns a single merged output channel that combines messages from all sources.
//
// Each queueOrTopic can be:
//   - A queue name (e.g., "my-queue")
//   - A topic/subscription path (e.g., "my-topic/my-subscription")
//
// Usage:
//
//	multiSub := NewMultiSubscriber(client, MultiSubscriberConfig{})
//	msgs, err := multiSub.Subscribe(ctx, "queue-1", "topic-1/sub-1", "queue-2")
//	if err != nil {
//	    // handle error
//	}
//	for msg := range msgs {
//	    // Process message from any source
//	    msg.Ack()
//	}
func (ms *MultiSubscriber) Subscribe(ctx context.Context, queuesOrTopics ...string) (<-chan *message.Message[[]byte], error) {
	ms.closeMu.RLock()
	if ms.closed {
		ms.closeMu.RUnlock()
		return nil, ErrSubscriberClosed
	}
	ms.closeMu.RUnlock()

	if len(queuesOrTopics) == 0 {
		out := make(chan *message.Message[[]byte])
		close(out)
		return out, nil
	}

	out := make(chan *message.Message[[]byte], ms.config.MergeBufferSize)

	// Create channels for each source
	var wg sync.WaitGroup
	wg.Add(len(queuesOrTopics))

	ms.mu.Lock()
	for _, queueOrTopic := range queuesOrTopics {
		subscriber := NewSubscriber(ms.client, ms.config.SubscriberConfig)
		ms.subscribers = append(ms.subscribers, subscriber)

		go func(sub *Subscriber, source string) {
			defer wg.Done()

			msgs, err := sub.Subscribe(ctx, source)
			if err != nil {
				return
			}

			for msg := range msgs {
				select {
				case <-ctx.Done():
					return
				case out <- msg:
				}
			}
		}(subscriber, queueOrTopic)
	}
	ms.mu.Unlock()

	// Close output channel when all subscribers are done
	go func() {
		wg.Wait()
		close(out)
	}()

	return out, nil
}

// Close gracefully shuts down all subscribers
func (ms *MultiSubscriber) Close() error {
	ms.closeMu.Lock()
	if ms.closed {
		ms.closeMu.Unlock()
		return nil
	}
	ms.closed = true
	ms.closeMu.Unlock()

	ms.mu.Lock()
	defer ms.mu.Unlock()

	var errs []error
	for _, sub := range ms.subscribers {
		if err := sub.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errs[0] // Return first error
	}
	return nil
}

// ErrSubscriberClosed is returned when attempting to subscribe with a closed subscriber
var ErrSubscriberClosed = newError("subscriber is closed")

// subsciberError represents a subscriber-specific error
type subscriberError struct {
	msg string
}

func newError(msg string) *subscriberError {
	return &subscriberError{msg: msg}
}

func (e *subscriberError) Error() string {
	return e.msg
}
