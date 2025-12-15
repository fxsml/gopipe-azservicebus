package azservicebus

import (
	"context"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe/message"
)

// Publisher publishes messages to Azure Service Bus queues and topics using a channel-based API.
// It implements the Publisher interface from CLAUDE.md specification.
type Publisher struct {
	sender *Sender
	config PublisherConfig
}

// PublisherConfig holds configuration for the Azure Service Bus publisher
type PublisherConfig struct {
	// BatchSize is the maximum number of messages to batch together for sending.
	// Default: 10
	BatchSize int

	// BatchTimeout is the maximum time to wait for a batch to fill up.
	// Default: 100ms
	BatchTimeout time.Duration

	// SendTimeout is the timeout for send operations.
	// Default: 30 seconds
	SendTimeout time.Duration

	// CloseTimeout is the timeout for closing the publisher.
	// Default: 30 seconds
	CloseTimeout time.Duration
}

// setDefaults sets default values for PublisherConfig
func (c *PublisherConfig) setDefaults() {
	if c.BatchSize <= 0 {
		c.BatchSize = 10
	}
	if c.BatchTimeout <= 0 {
		c.BatchTimeout = 100 * time.Millisecond
	}
	if c.SendTimeout <= 0 {
		c.SendTimeout = 30 * time.Second
	}
	if c.CloseTimeout <= 0 {
		c.CloseTimeout = 30 * time.Second
	}
}

// NewPublisher creates a new Azure Service Bus publisher.
func NewPublisher(client *azservicebus.Client, config PublisherConfig) *Publisher {
	config.setDefaults()

	sender := NewSender(client, SenderConfig{
		SendTimeout:  config.SendTimeout,
		CloseTimeout: config.CloseTimeout,
	})

	return &Publisher{
		sender: sender,
		config: config,
	}
}

// Publish publishes messages from the input channel to the specified Azure Service Bus queue or topic.
// It returns a done channel that is closed when all messages have been processed.
//
// The method batches messages for efficient sending and supports graceful shutdown via context cancellation.
//
// Usage:
//
//	publisher := NewPublisher(client, PublisherConfig{})
//	msgs := make(chan *message.Message[[]byte])
//	done, err := publisher.Publish(ctx, "my-queue", msgs)
//	if err != nil {
//	    // handle error
//	}
//	// Send messages to the channel
//	msgs <- message.New([]byte("hello"))
//	close(msgs) // Signal completion
//	<-done // Wait for all messages to be sent
func (p *Publisher) Publish(ctx context.Context, queueOrTopic string, msgs <-chan *message.Message[[]byte]) (<-chan struct{}, error) {
	done := make(chan struct{})

	go func() {
		defer close(done)
		p.processBatches(ctx, queueOrTopic, msgs)
	}()

	return done, nil
}

// processBatches collects messages into batches and sends them
func (p *Publisher) processBatches(ctx context.Context, queueOrTopic string, msgs <-chan *message.Message[[]byte]) {
	batch := make([]*message.Message[[]byte], 0, p.config.BatchSize)
	timer := time.NewTimer(p.config.BatchTimeout)
	defer timer.Stop()

	// Helper to send and reset batch
	sendBatch := func() {
		if len(batch) == 0 {
			return
		}
		// Copy batch to avoid race conditions
		toSend := make([]*message.Message[[]byte], len(batch))
		copy(toSend, batch)
		batch = batch[:0]

		sendCtx, cancel := context.WithTimeout(ctx, p.config.SendTimeout)
		_ = p.sender.Send(sendCtx, queueOrTopic, toSend)
		cancel()
	}

	for {
		select {
		case <-ctx.Done():
			// Send remaining messages before exiting
			sendBatch()
			return

		case msg, ok := <-msgs:
			if !ok {
				// Channel closed, send remaining messages
				sendBatch()
				return
			}

			batch = append(batch, msg)
			if len(batch) >= p.config.BatchSize {
				sendBatch()
				// Reset timer after sending
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(p.config.BatchTimeout)
			}

		case <-timer.C:
			sendBatch()
			timer.Reset(p.config.BatchTimeout)
		}
	}
}

// PublishSync publishes messages synchronously to the specified Azure Service Bus queue or topic.
// This is a convenience method for sending a slice of messages without using channels.
func (p *Publisher) PublishSync(ctx context.Context, queueOrTopic string, msgs []*message.Message[[]byte]) error {
	return p.sender.Send(ctx, queueOrTopic, msgs)
}

// Close gracefully shuts down the publisher
func (p *Publisher) Close() error {
	return p.sender.Close()
}

// MultiPublisher publishes messages to multiple Azure Service Bus queues or topics concurrently.
type MultiPublisher struct {
	publisher *Publisher
	config    MultiPublisherConfig
}

// MultiPublisherConfig holds configuration for the multi-publisher
type MultiPublisherConfig struct {
	// PublisherConfig is embedded for single publisher configuration
	PublisherConfig

	// Concurrency is the number of concurrent publishers per destination.
	// Default: 1
	Concurrency int
}

// setDefaults sets default values for MultiPublisherConfig
func (c *MultiPublisherConfig) setDefaults() {
	c.PublisherConfig.setDefaults()
	if c.Concurrency <= 0 {
		c.Concurrency = 1
	}
}

// NewMultiPublisher creates a new multi-destination Azure Service Bus publisher.
func NewMultiPublisher(client *azservicebus.Client, config MultiPublisherConfig) *MultiPublisher {
	config.setDefaults()

	return &MultiPublisher{
		publisher: NewPublisher(client, config.PublisherConfig),
		config:    config,
	}
}

// Publish publishes messages from the input channel to multiple destinations based on a routing function.
// The router function determines the destination queue/topic for each message.
// Returns a done channel that is closed when all messages have been processed.
func (mp *MultiPublisher) Publish(
	ctx context.Context,
	msgs <-chan *message.Message[[]byte],
	router func(*message.Message[[]byte]) string,
) (<-chan struct{}, error) {
	done := make(chan struct{})

	go func() {
		defer close(done)

		// Create channels for each destination
		destinations := make(map[string]chan *message.Message[[]byte])
		var destinationsMu sync.RWMutex
		var wg sync.WaitGroup

		// Helper to get or create channel for destination
		getOrCreateChan := func(dest string) chan *message.Message[[]byte] {
			destinationsMu.RLock()
			ch, exists := destinations[dest]
			destinationsMu.RUnlock()
			if exists {
				return ch
			}

			destinationsMu.Lock()
			defer destinationsMu.Unlock()
			// Double-check after acquiring write lock
			if ch, exists = destinations[dest]; exists {
				return ch
			}

			ch = make(chan *message.Message[[]byte], mp.config.BatchSize)
			destinations[dest] = ch

			// Start publisher for this destination
			wg.Add(1)
			go func(destination string, msgCh chan *message.Message[[]byte]) {
				defer wg.Done()
				pubDone, _ := mp.publisher.Publish(ctx, destination, msgCh)
				<-pubDone
			}(dest, ch)

			return ch
		}

		// Route messages to appropriate destinations
		for msg := range msgs {
			dest := router(msg)
			ch := getOrCreateChan(dest)
			select {
			case ch <- msg:
			case <-ctx.Done():
				return
			}
		}

		// Close all destination channels
		destinationsMu.Lock()
		for _, ch := range destinations {
			close(ch)
		}
		destinationsMu.Unlock()

		// Wait for all publishers to complete
		wg.Wait()
	}()

	return done, nil
}

// Close gracefully shuts down the multi-publisher
func (mp *MultiPublisher) Close() error {
	return mp.publisher.Close()
}
