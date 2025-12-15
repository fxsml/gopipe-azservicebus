package azservicebus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe/message"
)

// Ensure Receiver implements message.Receiver interface
var _ message.Receiver = (*Receiver)(nil)

// Receiver receives messages from Azure Service Bus queues and topics.
// It implements the message.Receiver interface from gopipe.
type Receiver struct {
	client      *azservicebus.Client
	config      ReceiverConfig
	receiversMu sync.RWMutex
	receivers   map[string]*azservicebus.Receiver
	closeMu     sync.RWMutex
	closed      bool
}

// ReceiverConfig holds configuration for Azure Service Bus receiver
type ReceiverConfig struct {
	// ReceiveTimeout is the timeout for receive operations
	// Default: 30 seconds
	ReceiveTimeout time.Duration

	// AckTimeout is the timeout for acknowledging messages
	// Default: 30 seconds
	AckTimeout time.Duration

	// CloseTimeout is the timeout for closing receivers during shutdown
	// Default: 30 seconds
	CloseTimeout time.Duration

	// MaxMessageCount is the maximum number of messages to receive in a single batch
	// Default: 10
	MaxMessageCount int
}

// setDefaults sets default values for ReceiverConfig
func (c *ReceiverConfig) setDefaults() {
	if c.ReceiveTimeout <= 0 {
		c.ReceiveTimeout = 30 * time.Second
	}
	if c.CloseTimeout <= 0 {
		c.CloseTimeout = 30 * time.Second
	}
	if c.MaxMessageCount <= 0 {
		c.MaxMessageCount = 10
	}
	if c.AckTimeout <= 0 {
		c.AckTimeout = 30 * time.Second
	}
}

// NewReceiver creates a new Azure Service Bus receiver.
func NewReceiver(client *azservicebus.Client, config ReceiverConfig) *Receiver {
	config.setDefaults()

	return &Receiver{
		client:    client,
		config:    config,
		receivers: make(map[string]*azservicebus.Receiver),
	}
}

// Receive receives a batch of messages from the specified Azure Service Bus queue or topic.
// It implements the message.Receiver interface from gopipe.
// It returns up to MaxMessageCount messages, or fewer if not enough messages are available.
//
// The topic parameter should be:
//   - A queue name for queue subscriptions (e.g., "my-queue")
//   - A topic/subscription path for topic subscriptions (e.g., "my-topic/my-subscription")
//
// The method automatically detects the subscription type based on the presence of "/" in the input.
//
// The method includes resilience mechanisms:
//   - Automatic receiver recreation on connection loss
//   - Timeout handling for receive operations
func (r *Receiver) Receive(ctx context.Context, topic string) ([]*message.Message, error) {
	// Check if receiver is closed
	r.closeMu.RLock()
	if r.closed {
		r.closeMu.RUnlock()
		return nil, fmt.Errorf("receiver is closed")
	}
	r.closeMu.RUnlock()

	// Get or create receiver for this queue/topic
	sbReceiver, err := r.getOrCreateReceiver(topic)
	if err != nil {
		return nil, err
	}

	// Attempt to receive messages
	messages, err := r.attemptReceiveMessages(ctx, sbReceiver)
	if err != nil {
		// If context was canceled, return empty result without error to stop gracefully
		if errors.Is(err, context.Canceled) {
			return []*message.Message{}, nil
		}

		// Check if it's a connection lost or closed error (includes idle timeout)
		var sbErr *azservicebus.Error
		if errors.As(err, &sbErr) && (sbErr.Code == azservicebus.CodeConnectionLost || sbErr.Code == azservicebus.CodeClosed) {
			// Recreate the receiver and retry
			newReceiver, recreateErr := r.recreateReceiver(topic)
			if recreateErr != nil {
				return nil, fmt.Errorf("failed to recreate receiver after connection loss: %w", recreateErr)
			}

			// Retry receiving with new receiver
			messages, err = r.attemptReceiveMessages(ctx, newReceiver)
			if err != nil {
				// If context was canceled, return empty result without error
				if errors.Is(err, context.Canceled) {
					return []*message.Message{}, nil
				}
				return nil, fmt.Errorf("failed to receive messages after recreating receiver: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to receive messages: %w", err)
		}
	}

	// Transform Azure Service Bus messages to gopipe messages
	result := make([]*message.Message, 0, len(messages))
	for _, sbMsg := range messages {
		msg := r.transformMessage(sbReceiver, sbMsg)
		result = append(result, msg)
	}

	return result, nil
}

// getOrCreateReceiver gets an existing receiver or creates a new one
func (r *Receiver) getOrCreateReceiver(queueOrTopic string) (*azservicebus.Receiver, error) {
	// Fast path: check if receiver already exists
	r.receiversMu.RLock()
	sbReceiver, exists := r.receivers[queueOrTopic]
	r.receiversMu.RUnlock()
	if exists {
		return sbReceiver, nil
	}

	// Slow path: create new receiver
	r.receiversMu.Lock()
	defer r.receiversMu.Unlock()

	// Double-check after acquiring write lock
	sbReceiver, exists = r.receivers[queueOrTopic]
	if exists {
		return sbReceiver, nil
	}

	// Create Azure Service Bus receiver based on input format
	var err error

	// Check if input contains "/" to determine if it's a topic/subscription or queue
	parts := strings.Split(queueOrTopic, "/")
	if len(parts) == 2 {
		// Topic subscription format: "topic/subscription"
		topicName := parts[0]
		subscriptionName := parts[1]
		sbReceiver, err = r.client.NewReceiverForSubscription(topicName, subscriptionName, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create receiver for topic %s subscription %s: %w", topicName, subscriptionName, err)
		}
	} else {
		// Queue format
		sbReceiver, err = r.client.NewReceiverForQueue(queueOrTopic, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create receiver for queue %s: %w", queueOrTopic, err)
		}
	}

	r.receivers[queueOrTopic] = sbReceiver
	return sbReceiver, nil
}

// attemptReceiveMessages attempts to receive messages from Azure Service Bus with timeout
func (r *Receiver) attemptReceiveMessages(ctx context.Context, sbReceiver *azservicebus.Receiver) ([]*azservicebus.ReceivedMessage, error) {
	receiveCtx, cancel := context.WithTimeout(ctx, r.config.ReceiveTimeout)
	defer cancel()

	messages, err := sbReceiver.ReceiveMessages(receiveCtx, r.config.MaxMessageCount, nil)
	if err != nil {
		return nil, err
	}

	return messages, nil
}

// recreateReceiver closes and recreates a receiver for the configured topic/queue
func (r *Receiver) recreateReceiver(queueOrTopic string) (*azservicebus.Receiver, error) {
	r.receiversMu.Lock()
	defer r.receiversMu.Unlock()

	// Check if we're closing
	r.closeMu.RLock()
	if r.closed {
		r.closeMu.RUnlock()
		return nil, fmt.Errorf("receiver is closing, cannot recreate receiver")
	}
	r.closeMu.RUnlock()

	// Close old receiver if it exists
	if oldReceiver, exists := r.receivers[queueOrTopic]; exists {
		ctx, cancel := context.WithTimeout(context.Background(), r.config.CloseTimeout)
		defer cancel()
		_ = oldReceiver.Close(ctx) // Best effort close, ignore errors
	}

	// Create new receiver based on format (queue vs topic/subscription)
	var newSbReceiver *azservicebus.Receiver
	var err error

	parts := strings.Split(queueOrTopic, "/")
	if len(parts) == 2 {
		// Topic subscription format: "topic/subscription"
		topicName := parts[0]
		subscriptionName := parts[1]
		newSbReceiver, err = r.client.NewReceiverForSubscription(topicName, subscriptionName, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to recreate receiver for topic %s subscription %s: %w", topicName, subscriptionName, err)
		}
	} else {
		// Queue format
		newSbReceiver, err = r.client.NewReceiverForQueue(queueOrTopic, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to recreate receiver for queue %s: %w", queueOrTopic, err)
		}
	}

	// Update the receiver reference
	r.receivers[queueOrTopic] = newSbReceiver

	return newSbReceiver, nil
}

// transformMessage transforms an Azure Service Bus ReceivedMessage to a gopipe Message
func (r *Receiver) transformMessage(sbReceiver *azservicebus.Receiver, sbMsg *azservicebus.ReceivedMessage) *message.Message {
	// Use raw bytes as payload
	payload := sbMsg.Body

	// Create attributes map for Service Bus metadata
	attrs := make(message.Attributes)

	// Map standard Service Bus properties to attributes
	if sbMsg.MessageID != "" {
		attrs[message.AttrID] = sbMsg.MessageID
	}
	if sbMsg.CorrelationID != nil {
		attrs[message.AttrCorrelationID] = *sbMsg.CorrelationID
	}
	if sbMsg.EnqueuedTime != nil {
		attrs[message.AttrTime] = *sbMsg.EnqueuedTime
	}
	if sbMsg.Subject != nil {
		attrs[message.AttrSubject] = *sbMsg.Subject
	}
	if sbMsg.ContentType != nil {
		attrs[message.AttrDataContentType] = *sbMsg.ContentType
	}

	// Map additional Service Bus properties
	attrs["deliveryCount"] = int(sbMsg.DeliveryCount)
	if sbMsg.LockedUntil != nil {
		attrs["lockedUntil"] = *sbMsg.LockedUntil
	}
	if sbMsg.SequenceNumber != nil {
		attrs["sequenceNumber"] = *sbMsg.SequenceNumber
	}
	if sbMsg.PartitionKey != nil {
		attrs["partitionKey"] = *sbMsg.PartitionKey
	}
	if sbMsg.TimeToLive != nil {
		attrs["ttl"] = *sbMsg.TimeToLive
	}
	if sbMsg.ReplyTo != nil {
		attrs["replyTo"] = *sbMsg.ReplyTo
	}

	// Map application properties
	for key, value := range sbMsg.ApplicationProperties {
		attrs[key] = value
	}

	// Create ack/nack callbacks
	ack := func() {
		ackCtx, cancel := context.WithTimeout(context.Background(), r.config.AckTimeout)
		defer cancel()
		if err := sbReceiver.CompleteMessage(ackCtx, sbMsg, nil); err != nil {
			// Log error but don't fail - message will be redelivered
			fmt.Printf("ERROR: failed to complete message: %v\n", err)
		}
	}

	nack := func(err error) {
		nackCtx, cancel := context.WithTimeout(context.Background(), r.config.AckTimeout)
		defer cancel()
		opts := &azservicebus.AbandonMessageOptions{}
		if err != nil {
			opts.PropertiesToModify = map[string]any{
				"abandon_error": err.Error(),
			}
		}
		if err := sbReceiver.AbandonMessage(nackCtx, sbMsg, opts); err != nil {
			// Log error but don't fail
			fmt.Printf("ERROR: failed to abandon message: %v\n", err)
		}
	}

	// Create gopipe message with payload, attributes, and ack callbacks
	return message.NewWithAcking(payload, attrs, ack, nack)
}

// Close gracefully shuts down the receiver
func (r *Receiver) Close() error {
	r.closeMu.Lock()
	if r.closed {
		r.closeMu.Unlock()
		return nil
	}
	r.closed = true
	r.closeMu.Unlock()

	// Close all receivers
	r.receiversMu.Lock()
	defer r.receiversMu.Unlock()

	var wg sync.WaitGroup
	wg.Add(len(r.receivers))

	mu := sync.Mutex{}
	var errs []error

	for key, sbReceiver := range r.receivers {
		go func(key string, sbReceiver *azservicebus.Receiver) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), r.config.CloseTimeout)
			defer cancel()
			if err := sbReceiver.Close(ctx); err != nil {
				mu.Lock()
				defer mu.Unlock()
				errs = append(errs, fmt.Errorf("failed to close receiver for %s: %w", key, err))
			}
		}(key, sbReceiver)
	}

	wg.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("errors closing receivers: %v", errs)
	}

	return nil
}
