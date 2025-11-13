package azservicebus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe"
)

// Subscriber implements the gopipe Subscriber interface for Azure Service Bus
type Subscriber[T any] struct {
	client           *azservicebus.Client
	config           SubscriberConfig
	receivers        map[string]*azservicebus.Receiver
	receiverLock     sync.RWMutex
	inFlightMessages sync.WaitGroup
	closed           bool
	closedLock       sync.RWMutex
}

// NewSubscriber creates a new Azure Service Bus subscriber
func NewSubscriber[T any](client *azservicebus.Client, config SubscriberConfig) *Subscriber[T] {
	return &Subscriber[T]{
		client:    client,
		config:    config,
		receivers: make(map[string]*azservicebus.Receiver),
	}
}

// Subscribe implements the gopipe Subscriber interface.
// The topic parameter can be either a queue name or a topic/subscription pair
// in the format "topic/subscription".
//
// This method returns a channel that will receive messages from Azure Service Bus.
// Messages are automatically mapped to gopipe.Message[T] with Ack/Nack functionality.
func (s *Subscriber[T]) Subscribe(ctx context.Context, topic string) (<-chan *gopipe.Message[T], error) {
	s.closedLock.RLock()
	if s.closed {
		s.closedLock.RUnlock()
		return nil, fmt.Errorf("subscriber is closed")
	}
	s.closedLock.RUnlock()

	// Get or create receiver for this topic/queue
	receiver, err := s.getOrCreateReceiver(topic)
	if err != nil {
		return nil, fmt.Errorf("failed to create receiver: %w", err)
	}

	// Create output channel
	msgChan := make(chan *gopipe.Message[T])

	// Start receiving messages
	go s.receiveLoop(ctx, receiver, topic, msgChan)

	return msgChan, nil
}

// getOrCreateReceiver gets an existing receiver or creates a new one
func (s *Subscriber[T]) getOrCreateReceiver(queueOrTopic string) (*azservicebus.Receiver, error) {
	// Fast path: check if receiver already exists
	s.receiverLock.RLock()
	receiver, exists := s.receivers[queueOrTopic]
	s.receiverLock.RUnlock()
	if exists {
		return receiver, nil
	}

	// Slow path: create new receiver
	s.receiverLock.Lock()
	defer s.receiverLock.Unlock()

	// Double-check after acquiring write lock
	receiver, exists = s.receivers[queueOrTopic]
	if exists {
		return receiver, nil
	}

	// Check if this is a topic subscription (format: "topic/subscription")
	if split := strings.Split(queueOrTopic, "/"); len(split) == 2 {
		var err error
		receiver, err = s.client.NewReceiverForSubscription(split[0], split[1], nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create receiver for topic subscription %s: %w", queueOrTopic, err)
		}
	} else {
		// Queue
		var err error
		receiver, err = s.client.NewReceiverForQueue(queueOrTopic, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create receiver for queue %s: %w", queueOrTopic, err)
		}
	}

	s.receivers[queueOrTopic] = receiver
	return receiver, nil
}

// receiveLoop continuously receives messages from Azure Service Bus
func (s *Subscriber[T]) receiveLoop(ctx context.Context, receiver *azservicebus.Receiver, topic string, msgChan chan<- *gopipe.Message[T]) {
	defer close(msgChan)

	// Semaphore for concurrent message processing
	sem := make(chan struct{}, s.config.ConcurrentMessages)

	for {
		// Check if context is cancelled or subscriber is closed
		select {
		case <-ctx.Done():
			return
		default:
		}

		s.closedLock.RLock()
		if s.closed {
			s.closedLock.RUnlock()
			return
		}
		s.closedLock.RUnlock()

		// Receive batch of messages
		sbMsgs, err := receiver.ReceiveMessages(ctx, s.config.BatchSize, nil)
		if err != nil {
			// Ignore context cancellation errors
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			// For other errors, continue trying
			continue
		}

		// Process each message in the batch
		for _, sbMsg := range sbMsgs {
			// Acquire semaphore slot
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}

			// Process message concurrently
			go func(msg *azservicebus.ReceivedMessage) {
				defer func() { <-sem }() // Release semaphore slot
				s.processMessage(ctx, receiver, msg, msgChan)
			}(sbMsg)
		}
	}
}

// processMessage processes a single Azure Service Bus message
func (s *Subscriber[T]) processMessage(
	ctx context.Context,
	receiver *azservicebus.Receiver,
	sbMsg *azservicebus.ReceivedMessage,
	msgChan chan<- *gopipe.Message[T],
) {
	s.inFlightMessages.Add(1)
	defer s.inFlightMessages.Done()

	// Unmarshal message body to type T
	var payload T
	if err := json.Unmarshal(sbMsg.Body, &payload); err != nil {
		// If unmarshaling fails, abandon the message
		s.abandonMessage(ctx, receiver, sbMsg, fmt.Errorf("failed to unmarshal message: %w", err))
		return
	}

	// Create gopipe message with metadata
	metadata := gopipe.Metadata{}

	// Map Service Bus properties to metadata
	if sbMsg.MessageID != "" {
		metadata["message_id"] = sbMsg.MessageID
	}
	if sbMsg.Subject != nil {
		metadata["subject"] = *sbMsg.Subject
	}
	if sbMsg.CorrelationID != nil {
		metadata["correlation_id"] = *sbMsg.CorrelationID
	}
	if sbMsg.ContentType != nil {
		metadata["content_type"] = *sbMsg.ContentType
	}
	if sbMsg.To != nil {
		metadata["to"] = *sbMsg.To
	}
	if sbMsg.ReplyTo != nil {
		metadata["reply_to"] = *sbMsg.ReplyTo
	}
	if sbMsg.SessionID != nil {
		metadata["session_id"] = *sbMsg.SessionID
	}

	// Map application properties
	for key, value := range sbMsg.ApplicationProperties {
		metadata[key] = fmt.Sprintf("%v", value)
	}

	// Create message context with timeout
	msgCtx, msgCancel := context.WithTimeout(context.Background(), s.config.MessageTimeout)
	defer msgCancel()

	// Create Ack and Nack functions
	ackCalled := make(chan struct{})
	nackCalled := make(chan error)

	ackFunc := func() {
		select {
		case ackCalled <- struct{}{}:
		default:
		}
	}

	nackFunc := func(err error) {
		select {
		case nackCalled <- err:
		default:
		}
	}

	// Create gopipe message using NewMessage constructor
	deadline, _ := msgCtx.Deadline()
	msg := gopipe.NewMessage(
		metadata,
		payload,
		deadline,
		ackFunc,
		nackFunc,
	)

	// Send message to channel
	select {
	case <-ctx.Done():
		s.abandonMessage(ctx, receiver, sbMsg, ctx.Err())
		return
	case msgChan <- msg:
	}

	// Wait for acknowledgment
	select {
	case <-ctx.Done():
		s.abandonMessage(ctx, receiver, sbMsg, ctx.Err())
	case <-msgCtx.Done():
		s.abandonMessage(ctx, receiver, sbMsg, msgCtx.Err())
	case err := <-nackCalled:
		s.abandonMessage(ctx, receiver, sbMsg, err)
	case <-ackCalled:
		s.completeMessage(ctx, receiver, sbMsg)
	}
}

// completeMessage marks a message as successfully processed
func (s *Subscriber[T]) completeMessage(
	ctx context.Context,
	receiver *azservicebus.Receiver,
	sbMsg *azservicebus.ReceivedMessage,
) {
	completeCtx, cancel := context.WithTimeout(context.Background(), s.config.CompleteTimeout)
	defer cancel()

	if err := receiver.CompleteMessage(completeCtx, sbMsg, nil); err != nil {
		// Log error but don't fail - message will be redelivered if timeout occurs
		_ = err
	}
}

// abandonMessage marks a message as failed and allows it to be redelivered
func (s *Subscriber[T]) abandonMessage(
	ctx context.Context,
	receiver *azservicebus.Receiver,
	sbMsg *azservicebus.ReceivedMessage,
	reason error,
) {
	abandonCtx, cancel := context.WithTimeout(context.Background(), s.config.AbandonTimeout)
	defer cancel()

	if err := receiver.AbandonMessage(abandonCtx, sbMsg, nil); err != nil {
		// Log error but don't fail
		_ = err
	}
}

// Close gracefully shuts down the subscriber, waiting for in-flight messages to complete
func (s *Subscriber[T]) Close() error {
	s.closedLock.Lock()
	if s.closed {
		s.closedLock.Unlock()
		return nil
	}
	s.closed = true
	s.closedLock.Unlock()

	// Wait for all in-flight messages to complete
	done := make(chan struct{})
	go func() {
		s.inFlightMessages.Wait()
		close(done)
	}()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), s.config.CloseTimeout)
	defer waitCancel()

	select {
	case <-done:
		// All messages completed
	case <-waitCtx.Done():
		// Timeout reached, proceed with closing anyway
	}

	// Close all receivers
	s.receiverLock.Lock()
	defer s.receiverLock.Unlock()

	var errs []error
	closeCtx, cancel := context.WithTimeout(context.Background(), s.config.CloseTimeout)
	defer cancel()

	for topic, receiver := range s.receivers {
		if err := receiver.Close(closeCtx); err != nil {
			errs = append(errs, fmt.Errorf("failed to close receiver for %s: %w", topic, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing receivers: %v", errs)
	}

	return nil
}
