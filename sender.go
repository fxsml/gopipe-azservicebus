package azservicebus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe/message"
)

// Ensure Sender implements message.Sender interface
var _ message.Sender = (*Sender)(nil)

// Sender sends messages to Azure Service Bus queues and topics.
// It implements the message.Sender interface from gopipe.
type Sender struct {
	client    *azservicebus.Client
	config    SenderConfig
	sendersMu sync.RWMutex
	senders   map[string]*azservicebus.Sender
	closeMu   sync.RWMutex
	closed    bool
}

// SenderConfig holds configuration for Azure Service Bus sender
type SenderConfig struct {
	// SendTimeout is the timeout for send operations
	// Default: 30 seconds
	SendTimeout time.Duration

	// CloseTimeout is the timeout for closing senders during shutdown
	// Default: 30 seconds
	CloseTimeout time.Duration
}

// setDefaults sets default values for SenderConfig
func (c *SenderConfig) setDefaults() {
	if c.SendTimeout <= 0 {
		c.SendTimeout = 30 * time.Second
	}
	if c.CloseTimeout <= 0 {
		c.CloseTimeout = 30 * time.Second
	}
}

// NewSender creates a new Azure Service Bus sender.
func NewSender(client *azservicebus.Client, config SenderConfig) *Sender {
	config.setDefaults()

	return &Sender{
		client:  client,
		config:  config,
		senders: make(map[string]*azservicebus.Sender),
	}
}

// Send sends a batch of messages to the specified Azure Service Bus queue or topic.
// It implements the message.Sender interface from gopipe.
//
// The topic parameter can be either a queue name or a topic name.
//
// The method includes resilience mechanisms:
//   - Automatic sender recreation on connection loss
//   - Timeout handling for send operations
func (s *Sender) Send(ctx context.Context, topic string, msgs []*message.Message) error {
	// Check if sender is closed
	s.closeMu.RLock()
	if s.closed {
		s.closeMu.RUnlock()
		return fmt.Errorf("sender is closed")
	}
	s.closeMu.RUnlock()

	if len(msgs) == 0 {
		return nil
	}

	// Get or create sender for this queue/topic
	sbSender, err := s.getOrCreateSender(topic)
	if err != nil {
		return err
	}

	// Convert to azservicebus.Messages
	sbMessages := make([]*azservicebus.Message, 0, len(msgs))
	for _, msg := range msgs {
		sbMsg := s.transformMessage(msg)
		sbMessages = append(sbMessages, sbMsg)
	}

	// If all messages failed to transform, return early
	if len(sbMessages) == 0 {
		return fmt.Errorf("all messages failed to transform")
	}

	// Attempt to send message batch
	err = s.attemptSendBatch(ctx, sbSender, sbMessages)
	if err != nil {
		// Check if it's a connection lost or closed error (includes idle timeout)
		var sbErr *azservicebus.Error
		if errors.As(err, &sbErr) && (sbErr.Code == azservicebus.CodeConnectionLost || sbErr.Code == azservicebus.CodeClosed) {
			// Recreate the sender and retry once
			newSender, recreateErr := s.recreateSender(topic)
			if recreateErr != nil {
				return fmt.Errorf("failed to recreate sender after connection loss: %w", recreateErr)
			}

			// Retry sending with new sender
			if err := s.attemptSendBatch(ctx, newSender, sbMessages); err != nil {
				return fmt.Errorf("failed to send message batch after recreating sender: %w", err)
			}
			return nil
		}
		return err
	}

	return nil
}

// getOrCreateSender gets an existing sender or creates a new one
func (s *Sender) getOrCreateSender(queueOrTopic string) (*azservicebus.Sender, error) {
	// Fast path: check if sender already exists
	s.sendersMu.RLock()
	sbSender, exists := s.senders[queueOrTopic]
	s.sendersMu.RUnlock()
	if exists {
		return sbSender, nil
	}

	// Slow path: create new sender
	s.sendersMu.Lock()
	defer s.sendersMu.Unlock()

	// Double-check after acquiring write lock
	sbSender, exists = s.senders[queueOrTopic]
	if exists {
		return sbSender, nil
	}

	// Create Azure Service Bus sender
	var err error
	sbSender, err = s.client.NewSender(queueOrTopic, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create sender for %s: %w", queueOrTopic, err)
	}

	s.senders[queueOrTopic] = sbSender
	return sbSender, nil
}

// transformMessage transforms a gopipe Message to an Azure Service Bus Message
func (s *Sender) transformMessage(msg *message.Message) *azservicebus.Message {
	// Use Data directly as body (it's already []byte)
	body := msg.Data

	// Create Azure Service Bus message
	sbMsg := &azservicebus.Message{
		Body:                  body,
		ApplicationProperties: make(map[string]any),
	}

	// Map attributes to Service Bus properties
	for key, value := range msg.Attributes {
		// Convert value to string if it's not already
		strValue, ok := value.(string)
		if !ok {
			strValue = fmt.Sprintf("%v", value)
		}

		switch key {
		case message.AttrID:
			v := strValue
			sbMsg.MessageID = &v
		case message.AttrCorrelationID:
			v := strValue
			sbMsg.CorrelationID = &v
		case message.AttrSubject:
			v := strValue
			sbMsg.Subject = &v
		case message.AttrDataContentType:
			v := strValue
			sbMsg.ContentType = &v
		case "ttl":
			if ttl, ok := value.(time.Duration); ok {
				sbMsg.TimeToLive = &ttl
			}
		default:
			// All other attributes go to application properties
			sbMsg.ApplicationProperties[key] = value
		}
	}
	return sbMsg
}

// attemptSendBatch attempts to send a batch of messages with the given sender
func (s *Sender) attemptSendBatch(ctx context.Context, sender *azservicebus.Sender, messages []*azservicebus.Message) error {
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
	sendCtx, cancel := context.WithTimeout(ctx, s.config.SendTimeout)
	defer cancel()
	if err := sender.SendMessageBatch(sendCtx, batch, nil); err != nil {
		return fmt.Errorf("failed to send message batch: %w", err)
	}

	return nil
}

// recreateSender closes and recreates a sender for the configured topic/queue
func (s *Sender) recreateSender(queueOrTopic string) (*azservicebus.Sender, error) {
	s.sendersMu.Lock()
	defer s.sendersMu.Unlock()

	// Check if we're closing
	s.closeMu.RLock()
	if s.closed {
		s.closeMu.RUnlock()
		return nil, fmt.Errorf("sender is closing, cannot recreate sender")
	}
	s.closeMu.RUnlock()

	// Close old sender if it exists
	if oldSender, exists := s.senders[queueOrTopic]; exists {
		ctx, cancel := context.WithTimeout(context.Background(), s.config.CloseTimeout)
		defer cancel()
		_ = oldSender.Close(ctx) // Best effort close, ignore errors
	}

	// Create new sender
	newSbSender, err := s.client.NewSender(queueOrTopic, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create new sender: %w", err)
	}

	// Update the sender reference
	s.senders[queueOrTopic] = newSbSender

	return newSbSender, nil
}

// Close gracefully shuts down the sender
func (s *Sender) Close() error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return nil
	}
	s.closed = true
	s.closeMu.Unlock()

	// Close all senders
	s.sendersMu.Lock()
	defer s.sendersMu.Unlock()

	var wg sync.WaitGroup
	wg.Add(len(s.senders))

	mu := sync.Mutex{}
	var errs []error

	for key, sbSender := range s.senders {
		go func(key string, sbSender *azservicebus.Sender) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), s.config.CloseTimeout)
			defer cancel()
			if err := sbSender.Close(ctx); err != nil {
				mu.Lock()
				defer mu.Unlock()
				errs = append(errs, fmt.Errorf("failed to close sender for %s: %w", key, err))
			}
		}(key, sbSender)
	}

	wg.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("errors closing senders: %v", errs)
	}

	return nil
}
