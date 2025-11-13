package azservicebus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe"
)

// Publisher implements the gopipe Publisher interface for Azure Service Bus
type Publisher[T any] struct {
	client     *azservicebus.Client
	config     PublisherConfig
	senders    map[string]*azservicebus.Sender
	senderLock sync.RWMutex
	closed     bool
	closedLock sync.RWMutex
}

// NewPublisher creates a new Azure Service Bus publisher
func NewPublisher[T any](client *azservicebus.Client, config PublisherConfig) *Publisher[T] {
	return &Publisher[T]{
		client:  client,
		config:  config,
		senders: make(map[string]*azservicebus.Sender),
	}
}

// Publish implements the gopipe Publisher interface.
// It publishes messages from the input channel to the specified Azure Service Bus topic or queue.
// Returns a done channel that closes when all messages have been published or an error occurs.
//
// The topic parameter can be either a queue name or a topic name.
// Messages are automatically marshaled to JSON before sending.
// Metadata from gopipe.Message is mapped to Azure Service Bus message properties.
func (s *Publisher[T]) Publish(ctx context.Context, topic string, msgs <-chan *gopipe.Message[T]) (<-chan struct{}, error) {
	s.closedLock.RLock()
	if s.closed {
		s.closedLock.RUnlock()
		return nil, fmt.Errorf("publisher is closed")
	}
	s.closedLock.RUnlock()

	// Get or create sender for this topic/queue
	sender, err := s.getOrCreateSender(topic)
	if err != nil {
		return nil, fmt.Errorf("failed to create sender: %w", err)
	}

	// Create done channel
	done := make(chan struct{})

	// Start publishing loop
	go func() {
		defer close(done)
		s.publishLoop(ctx, sender, msgs)
	}()

	return done, nil
}

// getOrCreateSender gets an existing sender or creates a new one
func (s *Publisher[T]) getOrCreateSender(queueOrTopic string) (*azservicebus.Sender, error) {
	// Fast path: check if sender already exists
	s.senderLock.RLock()
	sender, exists := s.senders[queueOrTopic]
	s.senderLock.RUnlock()
	if exists {
		return sender, nil
	}

	// Slow path: create new sender
	s.senderLock.Lock()
	defer s.senderLock.Unlock()

	// Double-check after acquiring write lock
	sender, exists = s.senders[queueOrTopic]
	if exists {
		return sender, nil
	}

	var err error
	sender, err = s.client.NewSender(queueOrTopic, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create sender for %s: %w", queueOrTopic, err)
	}

	s.senders[queueOrTopic] = sender
	return sender, nil
}

// publishLoop continuously publishes messages from the input channel
func (s *Publisher[T]) publishLoop(ctx context.Context, sender *azservicebus.Sender, msgs <-chan *gopipe.Message[T]) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				// Channel closed, stop publishing
				return
			}

			// Check if publisher is closed
			s.closedLock.RLock()
			if s.closed {
				s.closedLock.RUnlock()
				// Nack the message since we can't publish it
				msg.Nack(fmt.Errorf("publisher is closed"))
				return
			}
			s.closedLock.RUnlock()

			// Publish the message
			if err := s.publishMessage(ctx, sender, msg); err != nil {
				// Nack on error
				msg.Nack(err)
			} else {
				// Ack on success
				msg.Ack()
			}
		}
	}
}

// publishMessage publishes a single message to Azure Service Bus
func (s *Publisher[T]) publishMessage(ctx context.Context, sender *azservicebus.Sender, msg *gopipe.Message[T]) error {
	// Marshal payload to JSON
	body, err := json.Marshal(msg.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal message payload: %w", err)
	}

	// Create Azure Service Bus message
	sbMsg := &azservicebus.Message{
		Body:                  body,
		ApplicationProperties: make(map[string]interface{}),
	}

	// Map metadata to Service Bus properties
	for key, value := range msg.Metadata {
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
	}

	// Send message with timeout
	publishCtx, cancel := context.WithTimeout(ctx, s.config.PublishTimeout)
	defer cancel()

	if err := sender.SendMessage(publishCtx, sbMsg, nil); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// Close gracefully shuts down the publisher
func (s *Publisher[T]) Close() error {
	s.closedLock.Lock()
	if s.closed {
		s.closedLock.Unlock()
		return nil
	}
	s.closed = true
	s.closedLock.Unlock()

	// Close all senders
	s.senderLock.Lock()
	defer s.senderLock.Unlock()

	var errs []error
	ctx, cancel := context.WithTimeout(context.Background(), s.config.CloseTimeout)
	defer cancel()

	for topic, sender := range s.senders {
		if err := sender.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to close sender for %s: %w", topic, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing senders: %v", errs)
	}

	return nil
}
