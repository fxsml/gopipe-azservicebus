package azservicebus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe"
)

type sender struct {
	add   func(msgs <-chan *gopipe.Message[any]) (<-chan struct{}, error)
	close func() error
}

// Publisher implements the gopipe Publisher interface for Azure Service Bus
type Publisher struct {
	client *azservicebus.Client
	config PublisherConfig

	senderMu sync.RWMutex
	senders  map[string]*sender

	closedMu sync.RWMutex
	closed   bool
}

// NewPublisher creates a new Azure Service Bus publisher
func NewPublisher(client *azservicebus.Client, config PublisherConfig) *Publisher {
	config.setDefaults()
	return &Publisher{
		client:  client,
		config:  config,
		senders: make(map[string]*sender),
	}
}

// PublisherConfig holds configuration for Azure Service Bus publisher
type PublisherConfig struct {
	// PublishTimeout is the timeout for publish operations
	// Default: 30 seconds
	PublishTimeout time.Duration

	// CloseTimeout is the timeout for closing senders during shutdown
	// Default: 30 seconds
	CloseTimeout time.Duration

	// ShutdownTimeout is the maximum time to wait for graceful shutdown
	// Default: 60 seconds
	ShutdownTimeout time.Duration

	// BatchMaxSize is the number of messages to send in a single batch
	// Default: 1
	BatchMaxSize int

	// BatchMaxDuration is the maximum duration to wait before sending a batch
	// Default: 0 (no delay)
	BatchMaxDuration time.Duration

	// MarshalFunc is a function to marshal the message payload
	// Default: json.Marshal
	MarshalFunc func(any) ([]byte, error)
}

func (c *PublisherConfig) setDefaults() {
	if c.PublishTimeout <= 0 {
		c.PublishTimeout = 30 * time.Second
	}
	if c.CloseTimeout <= 0 {
		c.CloseTimeout = 30 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 60 * time.Second
	}
	if c.BatchMaxSize <= 0 {
		c.BatchMaxSize = 1
	}
	if c.BatchMaxDuration <= 0 {
		// Use 1ms as minimum to avoid panic in NewTicker
		c.BatchMaxDuration = 1 * time.Millisecond
	}
	if c.MarshalFunc == nil {
		c.MarshalFunc = json.Marshal
	}
}

// Publish implements the gopipe Publisher interface.
// It publishes messages from the input channel to the specified Azure Service Bus topic or queue.
// Returns a done channel that closes when all messages have been published or an error occurs.
//
// The topic parameter can be either a queue name or a topic name.
// Messages are automatically marshaled to JSON before sending.
// Metadata from gopipe.Message is mapped to Azure Service Bus message properties.
func (p *Publisher) Publish(topic string, msgs <-chan *gopipe.Message[any]) (<-chan struct{}, error) {
	p.closedMu.RLock()
	if p.closed {
		p.closedMu.RUnlock()
		return nil, fmt.Errorf("publisher is closed")
	}
	defer p.closedMu.RUnlock()

	sender, err := p.getOrCreateSender(topic)
	if err != nil {
		return nil, err
	}

	return sender.add(msgs)
}

// getOrCreateSender gets an existing sender or creates a new one
func (p *Publisher) getOrCreateSender(queueOrTopic string) (*sender, error) {
	// Fast path: check if sender already exists
	p.senderMu.RLock()
	s, exists := p.senders[queueOrTopic]
	p.senderMu.RUnlock()
	if exists {
		return s, nil
	}

	// Slow path: create new sender
	p.senderMu.Lock()
	defer p.senderMu.Unlock()

	// Double-check after acquiring write lock
	s, exists = p.senders[queueOrTopic]
	if exists {
		return s, nil
	}

	sbSender, err := p.client.NewSender(queueOrTopic, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create sender for %s: %w", queueOrTopic, err)
	}

	// Create FanIn to merge messages from multiple Publish calls
	ctx, cancel := context.WithCancel(context.Background())
	fan := gopipe.NewFanIn[*gopipe.Message[any]](gopipe.FanInConfig{
		ShutdownTimeout: p.config.ShutdownTimeout,
	})

	// Transform messages to Azure Service Bus format
	in := gopipe.NewTransformPipe(p.transformMessage).Start(context.Background(), fan.Start(ctx))

	// Batch messages before sending
	done := gopipe.NewBatchPipe(
		p.publishMessageBatch(sbSender),
		p.config.BatchMaxSize,
		p.config.BatchMaxDuration,
	).Start(context.Background(), in)

	s = &sender{
		add: fan.Add,
		close: func() error {
			cancel()
			<-done
			ctx, cancel := context.WithTimeout(context.Background(), p.config.CloseTimeout)
			defer cancel()
			return sbSender.Close(ctx)
		},
	}

	p.senders[queueOrTopic] = s
	return s, nil
}

func (p *Publisher) transformMessage(_ context.Context, msg *gopipe.Message[any]) (*azservicebus.Message, error) {
	// Marshal payload
	body, err := p.config.MarshalFunc(msg.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message payload: %w", err)
	}

	// Create Azure Service Bus message
	sbMsg := &azservicebus.Message{
		Body:                  body,
		ApplicationProperties: make(map[string]any),
	}

	// Map metadata to Service Bus properties
	msg.Properties().Range(func(key string, value any) bool {
		// Convert value to string if it's not already
		strValue, ok := value.(string)
		if !ok {
			strValue = fmt.Sprintf("%v", value)
		}

		switch key {
		case "message_id":
			sbMsg.MessageID = &strValue
		case "subject":
			sbMsg.Subject = &strValue
		case "correlation_id":
			sbMsg.CorrelationID = &strValue
		case "content_type":
			sbMsg.ContentType = &strValue
		case "to":
			sbMsg.To = &strValue
		case "reply_to":
			sbMsg.ReplyTo = &strValue
		case "session_id":
			sbMsg.SessionID = &strValue
		default:
			// All other metadata goes to application properties
			sbMsg.ApplicationProperties[key] = value
		}
		return true
	})
	return sbMsg, nil
}

func (p *Publisher) publishMessageBatch(sender *azservicebus.Sender) func(ctx context.Context, i []*azservicebus.Message) ([]struct{}, error) {
	return func(ctx context.Context, i []*azservicebus.Message) ([]struct{}, error) {
		// Check if publisher is closed
		p.closedMu.RLock()
		defer p.closedMu.RUnlock()
		if p.closed {
			return nil, fmt.Errorf("publisher is closed")
		}

		batch, err := sender.NewMessageBatch(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create message batch: %w", err)
		}

		for _, msg := range i {
			if err := batch.AddMessage(msg, nil); err != nil {
				return nil, fmt.Errorf("failed to add message to batch: %w", err)
			}
		}

		// Send message with timeout
		publishCtx, cancel := context.WithTimeout(ctx, p.config.PublishTimeout)
		defer cancel()
		if err := sender.SendMessageBatch(publishCtx, batch, nil); err != nil {
			return nil, fmt.Errorf("failed to send message batch: %w", err)
		}

		return nil, nil
	}
}

// Close gracefully shuts down the publisher
func (p *Publisher) Close() error {
	p.closedMu.Lock()
	if p.closed {
		p.closedMu.Unlock()
		return nil
	}
	p.closed = true
	p.closedMu.Unlock()

	// Close all senders
	p.senderMu.Lock()
	defer p.senderMu.Unlock()

	var wg sync.WaitGroup
	wg.Add(len(p.senders))

	mu := sync.Mutex{}
	var errs []error

	for topic, s := range p.senders {
		go func(topic string, s *sender) {
			defer wg.Done()
			if err := s.close(); err != nil {
				mu.Lock()
				defer mu.Unlock()
				errs = append(errs, fmt.Errorf("failed to close sender for %s: %w", topic, err))
			}
		}(topic, s)
	}

	wg.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("errors closing senders: %v", errs)
	}

	return nil
}
