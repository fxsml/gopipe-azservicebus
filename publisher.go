package azservicebus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe"
	"github.com/fxsml/gopipe/channel"
	"github.com/fxsml/gopipe/message"
)

type sender struct {
	sbSender *azservicebus.Sender
	add      func(msgs <-chan *message.Message) (<-chan struct{}, error)
	close    func() error
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

	// ErrorHandler is an optional function to handle message publish errors
	ErrorHandler func(*message.Message, error)
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
	if c.ErrorHandler == nil {
		c.ErrorHandler = func(*message.Message, error) {}
	}
}

// Publish implements the gopipe Publisher interface.
// It publishes messages from the input channel to the specified Azure Service Bus topic or queue.
// Returns a done channel that closes when all messages have been published or an error occurs.
//
// The topic parameter can be either a queue name or a topic name.
// Messages are automatically marshaled to JSON before sending.
// Metadata from message.Message is mapped to Azure Service Bus message properties.
func (p *Publisher) Publish(topic string, msgs <-chan *message.Message) (<-chan struct{}, error) {
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

type batchResult struct {
	msg *message.Message
	err error
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
	fan := gopipe.NewFanIn[*message.Message](gopipe.FanInConfig{
		ShutdownTimeout: p.config.ShutdownTimeout,
	})
	in := fan.Start(ctx)

	// Batch messages before sending
	batchRes := gopipe.NewBatchPipe(
		p.publishMessageBatch(queueOrTopic),
		p.config.BatchMaxSize,
		p.config.BatchMaxDuration,
		gopipe.WithCancel[[]*message.Message, batchResult](func(msg []*message.Message, err error) {
			for _, m := range msg {
				p.config.ErrorHandler(m, err)
			}
		}),
	).Start(context.Background(), in)

	done := channel.Sink(batchRes, func(res batchResult) {
		p.config.ErrorHandler(res.msg, res.err)
	})

	s = &sender{
		sbSender: sbSender,
		add:      fan.Add,
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

func (p *Publisher) transformMessage(msg *message.Message) (*azservicebus.Message, error) {
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
			v := strValue
			sbMsg.MessageID = &v
		case "subject":
			v := strValue
			sbMsg.Subject = &v
		case "correlation_id":
			v := strValue
			sbMsg.CorrelationID = &v
		case "content_type":
			v := strValue
			sbMsg.ContentType = &v
		case "to":
			v := strValue
			sbMsg.To = &v
		case "reply_to":
			v := strValue
			sbMsg.ReplyTo = &v
		case "session_id":
			v := strValue
			sbMsg.SessionID = &v
		default:
			// All other metadata goes to application properties
			sbMsg.ApplicationProperties[key] = value
		}
		return true
	})
	return sbMsg, nil
}

func (p *Publisher) publishMessageBatch(queueOrTopic string) func(ctx context.Context, i []*message.Message) ([]batchResult, error) {
	return func(ctx context.Context, i []*message.Message) ([]batchResult, error) {
		// Send batch with retry logic
		// Check if publisher is closed
		p.closedMu.RLock()
		if p.closed {
			p.closedMu.RUnlock()
			return nil, fmt.Errorf("publisher is closed")
		}
		p.closedMu.RUnlock()

		// Get the current sender
		p.senderMu.RLock()
		s, exists := p.senders[queueOrTopic]
		p.senderMu.RUnlock()
		if !exists {
			return nil, fmt.Errorf("sender not found for %s", queueOrTopic)
		}

		sender := s.sbSender

		// Prepare result slice
		results := make([]batchResult, 0, len(i))

		// Convert to azservicebus.Messages, filtering out failed transformations
		sbMessages := make([]*azservicebus.Message, 0, len(i))
		for _, msg := range i {
			sbMsg, err := p.transformMessage(msg)
			if err != nil {
				results = append(results, batchResult{msg: msg, err: fmt.Errorf("failed to transform message: %w", err)})
				continue
			}
			sbMessages = append(sbMessages, sbMsg)
		}

		// If all messages failed to transform, return early with errors
		if len(sbMessages) == 0 {
			return results, nil
		}

		// Attempt to send message batch
		err := p.attemptSendBatch(ctx, sender, sbMessages)
		if err != nil {
			// Check if it's a connection lost or closed error (includes idle timeout)
			var sbErr *azservicebus.Error
			if errors.As(err, &sbErr) && (sbErr.Code == azservicebus.CodeConnectionLost || sbErr.Code == azservicebus.CodeClosed) {
				// Recreate the sender and retry once
				newSender, recreateErr := p.recreateSender(queueOrTopic)
				if recreateErr != nil {
					return nil, fmt.Errorf("failed to recreate sender after connection loss: %w", recreateErr)
				}

				// Retry sending with new sender
				if err := p.attemptSendBatch(ctx, newSender, sbMessages); err != nil {
					return nil, fmt.Errorf("failed to send message batch after recreating sender: %w", err)
				}
				return results, nil
			}
			return nil, err
		}

		return results, nil
	}
}

// attemptSendBatch attempts to send a batch of messages with the given sender
func (p *Publisher) attemptSendBatch(ctx context.Context, sender *azservicebus.Sender, messages []*azservicebus.Message) error {
	batch, err := sender.NewMessageBatch(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to create message batch: %w", err)
	}

	for _, msg := range messages {
		if err := batch.AddMessage(msg, nil); err != nil {
			return fmt.Errorf("failed to add message to batch: %w", err)
		}
	}

	// Send message with timeout
	publishCtx, cancel := context.WithTimeout(ctx, p.config.PublishTimeout)
	defer cancel()
	if err := sender.SendMessageBatch(publishCtx, batch, nil); err != nil {
		return fmt.Errorf("failed to send message batch: %w", err)
	}

	return nil
}

// recreateSender closes and recreates a sender for the given topic/queue
func (p *Publisher) recreateSender(queueOrTopic string) (*azservicebus.Sender, error) {
	p.senderMu.Lock()
	defer p.senderMu.Unlock()

	// Check if we're closing
	p.closedMu.RLock()
	if p.closed {
		p.closedMu.RUnlock()
		return nil, fmt.Errorf("publisher is closing, cannot recreate sender")
	}
	p.closedMu.RUnlock()

	// Close old sender if it exists
	if oldSender, exists := p.senders[queueOrTopic]; exists && oldSender.sbSender != nil {
		ctx, cancel := context.WithTimeout(context.Background(), p.config.CloseTimeout)
		defer cancel()
		_ = oldSender.sbSender.Close(ctx) // Best effort close, ignore errors
	}

	// Create new sender
	newSbSender, err := p.client.NewSender(queueOrTopic, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create new sender: %w", err)
	}

	// Update the sender reference
	if s, exists := p.senders[queueOrTopic]; exists {
		s.sbSender = newSbSender
	}

	return newSbSender, nil
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
