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
	"github.com/fxsml/gopipe/message"
)

type receiver struct {
	sbReceiver   *azservicebus.Receiver
	receiverKey  string
	cancel       func()
	recreateLock sync.Mutex
}

// Subscriber implements subscription from Azure Service Bus for gopipe pipelines
type Subscriber struct {
	client *azservicebus.Client
	config SubscriberConfig

	receiverMu sync.RWMutex
	receivers  map[string]*receiver

	closedMu sync.RWMutex
	closed   bool
}

// NewSubscriber creates a new Azure Service Bus subscriber
func NewSubscriber(client *azservicebus.Client, config SubscriberConfig) *Subscriber {
	config.setDefaults()
	return &Subscriber{
		client:    client,
		config:    config,
		receivers: make(map[string]*receiver),
	}
}

// SubscriberConfig holds configuration for Azure Service Bus subscriber
type SubscriberConfig struct {
	// ReceiveTimeout is the timeout for receive operations
	// Default: 30 seconds
	ReceiveTimeout time.Duration

	// CloseTimeout is the timeout for closing receivers during shutdown
	// Default: 30 seconds
	CloseTimeout time.Duration

	// ShutdownTimeout is the maximum time to wait for graceful shutdown
	// Default: 60 seconds
	ShutdownTimeout time.Duration

	// MaxMessageCount is the maximum number of messages to receive in a single batch
	// Default: 10
	MaxMessageCount int

	// UnmarshalFunc is a function to unmarshal the message payload
	// Default: json.Unmarshal
	UnmarshalFunc func([]byte, any) error

	// ErrorHandler is called when an error occurs during message processing
	// Default: no-op (errors are silently ignored)
	ErrorHandler func(error)
}

func (c *SubscriberConfig) setDefaults() {
	if c.ReceiveTimeout <= 0 {
		c.ReceiveTimeout = 30 * time.Second
	}
	if c.CloseTimeout <= 0 {
		c.CloseTimeout = 30 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 60 * time.Second
	}
	if c.MaxMessageCount <= 0 {
		c.MaxMessageCount = 10
	}
	if c.UnmarshalFunc == nil {
		c.UnmarshalFunc = json.Unmarshal
	}
	if c.ErrorHandler == nil {
		c.ErrorHandler = func(err error) {
		}
	}
}

// Subscribe subscribes to messages from the specified Azure Service Bus queue or topic.
// Returns a channel of messages that will be continuously populated as messages arrive.
// The subscription continues until the context is cancelled or the subscriber is closed.
//
// The queueOrTopic parameter should be:
//   - A queue name for queue subscriptions (e.g., "my-queue")
//   - A topic/subscription path for topic subscriptions (e.g., "my-topic/my-subscription")
//
// The method automatically detects the subscription type based on the presence of "/" in the input.
// Messages are automatically unmarshaled from JSON.
// Azure Service Bus message properties are mapped to message metadata.
func (s *Subscriber) Subscribe(ctx context.Context, queueOrTopic string) (<-chan *message.Message, error) {
	s.closedMu.RLock()
	if s.closed {
		s.closedMu.RUnlock()
		return nil, fmt.Errorf("subscriber is closed")
	}
	defer s.closedMu.RUnlock()

	// Check if receiver already exists
	s.receiverMu.RLock()
	_, exists := s.receivers[queueOrTopic]
	s.receiverMu.RUnlock()
	if exists {
		return nil, fmt.Errorf("already subscribed to %s", queueOrTopic)
	}

	// Create Azure Service Bus receiver based on input format
	var sbReceiver *azservicebus.Receiver
	var err error

	// Check if input contains "/" to determine if it's a topic/subscription or queue
	if idx := indexOf(queueOrTopic, "/"); idx >= 0 {
		// Topic subscription format: "topic/subscription"
		topicName := queueOrTopic[:idx]
		subscriptionName := queueOrTopic[idx+1:]
		sbReceiver, err = s.client.NewReceiverForSubscription(topicName, subscriptionName, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create receiver for topic %s subscription %s: %w", topicName, subscriptionName, err)
		}
	} else {
		// Queue format
		sbReceiver, err = s.client.NewReceiverForQueue(queueOrTopic, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create receiver for queue %s: %w", queueOrTopic, err)
		}
	}

	// Create cancellable context for this subscription
	subCtx, cancel := context.WithCancel(ctx)

	// Track receiver for cleanup
	r := &receiver{
		sbReceiver:  sbReceiver,
		receiverKey: queueOrTopic,
		cancel:      cancel,
	}

	s.receiverMu.Lock()
	s.receivers[queueOrTopic] = r
	s.receiverMu.Unlock()

	// Use gopipe.NewGenerator to continuously fetch messages
	generator := gopipe.NewGenerator(s.generateMessages(queueOrTopic),
		gopipe.WithCancel[struct{}, *message.Message](func(in struct{}, err error) {
			s.config.ErrorHandler(err)
		}))

	msgChan := generator.Generate(subCtx)

	return msgChan, nil
}

// generateMessages returns a function that continuously fetches messages from Azure Service Bus
func (s *Subscriber) generateMessages(queueOrTopic string) func(context.Context) ([]*message.Message, error) {
	return func(ctx context.Context) ([]*message.Message, error) {
		// Check if subscriber is closed
		s.closedMu.RLock()
		if s.closed {
			s.closedMu.RUnlock()
			return nil, fmt.Errorf("subscriber is closed")
		}
		s.closedMu.RUnlock()

		// Get the current receiver
		s.receiverMu.RLock()
		r, exists := s.receivers[queueOrTopic]
		s.receiverMu.RUnlock()
		if !exists {
			return nil, fmt.Errorf("receiver not found for %s", queueOrTopic)
		}

		sbReceiver := r.sbReceiver

		// Attempt to receive messages
		messages, err := s.attemptReceiveMessages(ctx, sbReceiver)
		if err != nil {
			// If context was canceled, return empty result without error to stop gracefully
			if errors.Is(err, context.Canceled) {
				return []*message.Message{}, nil
			}

			// Check if it's a connection lost or closed error (includes idle timeout)
			var sbErr *azservicebus.Error
			if errors.As(err, &sbErr) && (sbErr.Code == azservicebus.CodeConnectionLost || sbErr.Code == azservicebus.CodeClosed) {
				// Recreate the receiver and retry
				newReceiver, recreateErr := s.recreateReceiver(queueOrTopic)
				if recreateErr != nil {
					// For recreation errors, call error handler and return
					receiveErr := fmt.Errorf("failed to recreate receiver after connection loss: %w", recreateErr)
					s.config.ErrorHandler(receiveErr)
					return nil, receiveErr
				}

				// Retry receiving with new receiver
				messages, err = s.attemptReceiveMessages(ctx, newReceiver)
				if err != nil {
					// If context was canceled, return empty result without error
					if errors.Is(err, context.Canceled) {
						return []*message.Message{}, nil
					}
					receiveErr := fmt.Errorf("failed to receive messages after recreating receiver: %w", err)
					s.config.ErrorHandler(receiveErr)
					return nil, receiveErr
				}
			} else {
				// For other errors, call error handler and return the error
				receiveErr := fmt.Errorf("failed to receive messages: %w", err)
				s.config.ErrorHandler(receiveErr)
				return nil, receiveErr
			}
		}

		// Transform Azure Service Bus messages to gopipe messages
		result := make([]*message.Message, 0, len(messages))
		for _, sbMsg := range messages {
			msg, err := s.transformMessage(ctx, sbReceiver, sbMsg)
			if err != nil {
				return nil, err
			}
			result = append(result, msg)
		}

		return result, nil
	}
}

// attemptReceiveMessages attempts to receive messages from Azure Service Bus with timeout
func (s *Subscriber) attemptReceiveMessages(ctx context.Context, sbReceiver *azservicebus.Receiver) ([]*azservicebus.ReceivedMessage, error) {
	receiveCtx, cancel := context.WithTimeout(ctx, s.config.ReceiveTimeout)
	defer cancel()

	messages, err := sbReceiver.ReceiveMessages(receiveCtx, s.config.MaxMessageCount, nil)
	if err != nil {
		return nil, err
	}

	return messages, nil
}

// recreateReceiver closes and recreates a receiver for the given topic/queue
func (s *Subscriber) recreateReceiver(queueOrTopic string) (*azservicebus.Receiver, error) {
	s.receiverMu.Lock()
	defer s.receiverMu.Unlock()

	// Check if we're closing
	s.closedMu.RLock()
	if s.closed {
		s.closedMu.RUnlock()
		return nil, fmt.Errorf("subscriber is closing, cannot recreate receiver")
	}
	s.closedMu.RUnlock()

	r, exists := s.receivers[queueOrTopic]
	if !exists {
		return nil, fmt.Errorf("receiver not found for %s", queueOrTopic)
	}

	// Close old receiver if it exists
	if r.sbReceiver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), s.config.CloseTimeout)
		defer cancel()
		_ = r.sbReceiver.Close(ctx) // Best effort close, ignore errors
	}

	// Create new receiver based on format (queue vs topic/subscription)
	var newSbReceiver *azservicebus.Receiver
	var err error

	if idx := indexOf(queueOrTopic, "/"); idx >= 0 {
		// Topic subscription format: "topic/subscription"
		topicName := queueOrTopic[:idx]
		subscriptionName := queueOrTopic[idx+1:]
		newSbReceiver, err = s.client.NewReceiverForSubscription(topicName, subscriptionName, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to recreate receiver for topic %s subscription %s: %w", topicName, subscriptionName, err)
		}
	} else {
		// Queue format
		newSbReceiver, err = s.client.NewReceiverForQueue(queueOrTopic, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to recreate receiver for queue %s: %w", queueOrTopic, err)
		}
	}

	// Update the receiver reference
	r.sbReceiver = newSbReceiver

	return newSbReceiver, nil
}

// transformMessage transforms an Azure Service Bus ReceivedMessage to a gopipe Message
func (s *Subscriber) transformMessage(ctx context.Context, sbReceiver *azservicebus.Receiver, sbMsg *azservicebus.ReceivedMessage) (*message.Message, error) {
	// Unmarshal payload
	var payload any
	if len(sbMsg.Body) > 0 {
		if err := s.config.UnmarshalFunc(sbMsg.Body, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal message payload: %w", err)
		}
	}

	// Create properties and map Service Bus metadata
	props := &message.Properties{}

	// Map standard Service Bus properties to metadata
	if sbMsg.MessageID != "" {
		props.Set("message_id", sbMsg.MessageID)
	}
	if sbMsg.Subject != nil {
		props.Set("subject", *sbMsg.Subject)
	}
	if sbMsg.ContentType != nil {
		props.Set("content_type", *sbMsg.ContentType)
	}
	if sbMsg.CorrelationID != nil {
		props.Set("correlation_id", *sbMsg.CorrelationID)
	}
	if sbMsg.To != nil {
		props.Set("to", *sbMsg.To)
	}
	if sbMsg.ReplyTo != nil {
		props.Set("reply_to", *sbMsg.ReplyTo)
	}
	if sbMsg.SessionID != nil {
		props.Set("session_id", *sbMsg.SessionID)
	}

	// Map application properties
	for key, value := range sbMsg.ApplicationProperties {
		props.Set(key, value)
	}

	// Create ack/nack callbacks
	ack := func() {
		ackCtx, cancel := context.WithTimeout(context.Background(), s.config.CloseTimeout)
		defer cancel()
		if err := sbReceiver.CompleteMessage(ackCtx, sbMsg, nil); err != nil {
			// Log error but don't fail - message will be redelivered
			fmt.Printf("ERROR: failed to complete message: %v\n", err)
		}
	}

	nack := func(err error) {
		nackCtx, cancel := context.WithTimeout(context.Background(), s.config.CloseTimeout)
		defer cancel()
		if err := sbReceiver.AbandonMessage(nackCtx, sbMsg, nil); err != nil {
			// Log error but don't fail
			fmt.Printf("ERROR: failed to abandon message: %v\n", err)
		}
	}

	// Create gopipe message with properties and ack/nack callbacks
	return message.NewMessage(props, payload, time.Time{}, ack, nack), nil
}

// Close gracefully shuts down the subscriber
func (s *Subscriber) Close() error {
	s.closedMu.Lock()
	if s.closed {
		s.closedMu.Unlock()
		return nil
	}
	s.closed = true
	s.closedMu.Unlock()

	// Close all receivers
	s.receiverMu.Lock()
	defer s.receiverMu.Unlock()

	var wg sync.WaitGroup
	wg.Add(len(s.receivers))

	mu := sync.Mutex{}
	var errs []error

	for key, r := range s.receivers {
		go func(key string, r *receiver) {
			defer wg.Done()

			// Cancel the context to stop message generation
			r.cancel()

			// Close the receiver
			closeCtx, cancel := context.WithTimeout(context.Background(), s.config.CloseTimeout)
			defer cancel()
			if err := r.sbReceiver.Close(closeCtx); err != nil {
				mu.Lock()
				defer mu.Unlock()
				errs = append(errs, fmt.Errorf("failed to close receiver for %s: %w", key, err))
			}
		}(key, r)
	}

	wg.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("errors closing receivers: %v", errs)
	}

	return nil
}

// indexOf returns the index of the first occurrence of the character in the string,
// or -1 if not found
func indexOf(s string, char string) int {
	for i := range s {
		if s[i] == char[0] {
			return i
		}
	}
	return -1
}
