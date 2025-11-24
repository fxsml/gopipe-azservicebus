package azservicebus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe"
	"github.com/fxsml/gopipe/message"
)

type receiver struct {
	sbReceiver *azservicebus.Receiver
	cancel     func()
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
}

// Subscribe subscribes to messages from the specified Azure Service Bus queue or topic.
// Returns a channel of messages that will be continuously populated as messages arrive.
// The subscription continues until the context is cancelled or the subscriber is closed.
//
// The queueOrTopic parameter should be a queue name for queue subscriptions.
// For topic subscriptions, use SubscribeToTopic instead.
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

	// Create Azure Service Bus receiver
	sbReceiver, err := s.client.NewReceiverForQueue(queueOrTopic, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create receiver for queue %s: %w", queueOrTopic, err)
	}

	// Create cancellable context for this subscription
	subCtx, cancel := context.WithCancel(ctx)

	// Use gopipe.NewGenerator to continuously fetch messages
	generator := gopipe.NewGenerator(s.generateMessages(subCtx, sbReceiver))
	msgChan := generator.Generate(subCtx)

	// Track receiver for cleanup
	r := &receiver{
		sbReceiver: sbReceiver,
		cancel:     cancel,
	}

	s.receiverMu.Lock()
	s.receivers[queueOrTopic] = r
	s.receiverMu.Unlock()

	return msgChan, nil
}

// SubscribeToTopic subscribes to messages from the specified Azure Service Bus topic and subscription.
// Returns a channel of messages that will be continuously populated as messages arrive.
// The subscription continues until the context is cancelled or the subscriber is closed.
//
// Messages are automatically unmarshaled from JSON.
// Azure Service Bus message properties are mapped to message metadata.
func (s *Subscriber) SubscribeToTopic(ctx context.Context, topicName, subscriptionName string) (<-chan *message.Message, error) {
	s.closedMu.RLock()
	if s.closed {
		s.closedMu.RUnlock()
		return nil, fmt.Errorf("subscriber is closed")
	}
	defer s.closedMu.RUnlock()

	key := topicName + "/" + subscriptionName

	// Check if receiver already exists
	s.receiverMu.RLock()
	_, exists := s.receivers[key]
	s.receiverMu.RUnlock()
	if exists {
		return nil, fmt.Errorf("already subscribed to %s", key)
	}

	// Create Azure Service Bus receiver
	sbReceiver, err := s.client.NewReceiverForSubscription(topicName, subscriptionName, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create receiver for topic %s subscription %s: %w", topicName, subscriptionName, err)
	}

	// Create cancellable context for this subscription
	subCtx, cancel := context.WithCancel(ctx)

	// Use gopipe.NewGenerator to continuously fetch messages
	generator := gopipe.NewGenerator(s.generateMessages(subCtx, sbReceiver))
	msgChan := generator.Generate(subCtx)

	// Track receiver for cleanup
	r := &receiver{
		sbReceiver: sbReceiver,
		cancel:     cancel,
	}

	s.receiverMu.Lock()
	s.receivers[key] = r
	s.receiverMu.Unlock()

	return msgChan, nil
}

// generateMessages returns a function that continuously fetches messages from Azure Service Bus
func (s *Subscriber) generateMessages(ctx context.Context, sbReceiver *azservicebus.Receiver) func(context.Context) ([]*message.Message, error) {
	return func(ctx context.Context) ([]*message.Message, error) {
		// Check if subscriber is closed
		s.closedMu.RLock()
		defer s.closedMu.RUnlock()
		if s.closed {
			return nil, fmt.Errorf("subscriber is closed")
		}

		// Receive messages with timeout
		receiveCtx, cancel := context.WithTimeout(ctx, s.config.ReceiveTimeout)
		defer cancel()

		messages, err := sbReceiver.ReceiveMessages(receiveCtx, s.config.MaxMessageCount, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to receive messages: %w", err)
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
