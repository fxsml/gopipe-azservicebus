package azservicebus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fxsml/gopipe-azservicebus/internal/semaphore"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe/message"
)

// DefaultLockRenewalInterval is the fallback renewal interval when LockedUntil is unavailable.
const DefaultLockRenewalInterval = 30 * time.Second

// SubscriberConfig contains configuration for the Subscriber.
type SubscriberConfig struct {
	// Logger for the subscriber. Defaults to slog.Default().
	Logger *slog.Logger

	// MaxInFlight is the maximum number of messages being processed concurrently.
	// This provides backpressure - new messages won't be received until in-flight
	// messages are acked/nacked. Defaults to 100.
	MaxInFlight int

	// PrefetchCount is the number of messages to fetch from ServiceBus at once.
	// Higher values improve throughput but increase memory usage.
	// Defaults to MaxInFlight if not set.
	PrefetchCount int

	// ShutdownTimeout is the maximum time allowed for graceful shutdown.
	// This is used when draining in-flight messages during context cancellation.
	// Defaults to 30 seconds.
	ShutdownTimeout time.Duration

	// OperationTimeout is the timeout for ServiceBus operations.
	// Defaults to 5 seconds.
	OperationTimeout time.Duration

	// ReconnectBackoff is the delay before retrying after a failed reconnect.
	// Defaults to 5 seconds.
	ReconnectBackoff time.Duration

	// LockRenewalInterval overrides the auto-detected renewal interval.
	// If zero (default), the interval is calculated per-message from LockedUntil:
	//   renewalInterval = time.Until(msg.LockedUntil) / 2
	// If non-zero, this value is used for all messages.
	LockRenewalInterval time.Duration

	// DisableLockRenewal disables automatic lock renewal.
	// Use only for short-processing scenarios where lock never expires.
	DisableLockRenewal bool

	// Properties maps Azure Service Bus broker properties onto the received message.
	// Each field is a function receiving the broker property value and the message to mutate.
	// Nil functions and nil broker property fields are skipped.
	Properties SubscriberProperties
}

// SubscriberProperties holds per-field mapping functions from Azure Service Bus broker
// properties to the received gopipe message. Functions receive the dereferenced property
// value and a mutable *message.RawMessage and may write to msg.Attributes freely.
type SubscriberProperties struct {
	SessionID            func(string, *message.RawMessage)
	ReplyTo              func(string, *message.RawMessage)
	ReplyToSessionID     func(string, *message.RawMessage)
	To                   func(string, *message.RawMessage)
	Subject              func(string, *message.RawMessage)
	PartitionKey         func(string, *message.RawMessage)
	TimeToLive           func(time.Duration, *message.RawMessage)
	ScheduledEnqueueTime func(time.Time, *message.RawMessage)
}

func (sp SubscriberProperties) apply(sbMsg *azservicebus.ReceivedMessage, msg *message.RawMessage) {
	if sp.SessionID != nil && sbMsg.SessionID != nil {
		sp.SessionID(*sbMsg.SessionID, msg)
	}
	if sp.ReplyTo != nil && sbMsg.ReplyTo != nil {
		sp.ReplyTo(*sbMsg.ReplyTo, msg)
	}
	if sp.ReplyToSessionID != nil && sbMsg.ReplyToSessionID != nil {
		sp.ReplyToSessionID(*sbMsg.ReplyToSessionID, msg)
	}
	if sp.To != nil && sbMsg.To != nil {
		sp.To(*sbMsg.To, msg)
	}
	if sp.Subject != nil && sbMsg.Subject != nil {
		sp.Subject(*sbMsg.Subject, msg)
	}
	if sp.PartitionKey != nil && sbMsg.PartitionKey != nil {
		sp.PartitionKey(*sbMsg.PartitionKey, msg)
	}
	if sp.TimeToLive != nil && sbMsg.TimeToLive != nil {
		sp.TimeToLive(*sbMsg.TimeToLive, msg)
	}
	if sp.ScheduledEnqueueTime != nil && sbMsg.ScheduledEnqueueTime != nil {
		sp.ScheduledEnqueueTime(*sbMsg.ScheduledEnqueueTime, msg)
	}
}

func (c SubscriberConfig) withDefaults() SubscriberConfig {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.MaxInFlight <= 0 {
		c.MaxInFlight = 100
	}
	if c.PrefetchCount <= 0 {
		c.PrefetchCount = c.MaxInFlight
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 30 * time.Second
	}
	if c.OperationTimeout <= 0 {
		c.OperationTimeout = 5 * time.Second
	}
	if c.ReconnectBackoff <= 0 {
		c.ReconnectBackoff = 5 * time.Second
	}
	return c
}

// Subscriber subscribes to a single Azure Service Bus topic/subscription or queue.
// One instance = one topic. Create multiple instances for multiple topics.
//
// Lifecycle:
//   - Subscribe(ctx) starts a subscription. The subscription ends when ctx is cancelled.
//   - After context cancellation, the subscriber can be reused with a new Subscribe() call.
//   - Close() permanently closes the subscriber and releases the receiver connection.
//   - Only one active subscription is allowed at a time.
type Subscriber struct {
	logger   *slog.Logger
	client   *azservicebus.Client
	receiver *azservicebus.Receiver
	topic    string
	config   SubscriberConfig

	// Subscriber-level state
	closed atomic.Bool // permanent closure flag

	// Settlement tracking with metrics - limits unsettled messages to MaxInFlight
	inFlight *semaphore.Semaphore

	// Protects receiver during reconnect
	receiverMu sync.Mutex

	// Subscription coordination (protected by subMu)
	subMu     sync.Mutex
	activeSub *activeSubscription // nil if no active subscription
	subWg     sync.WaitGroup      // tracks subscription goroutine
	closeDone chan struct{}       // closed when Close() completes shutdown
	closeOnce sync.Once
	closeErr  error
}

// activeSubscription tracks the state of a running subscription.
type activeSubscription struct {
	abort     chan struct{} // closed to signal handlers to abandon
	abortOnce sync.Once
	cancel    context.CancelFunc // cancels the subscription context (for Close())
}

// ErrSubscriberClosed is returned when operations are attempted on a closed subscriber.
var ErrSubscriberClosed = errors.New("subscriber is closed")

// ErrSubscriptionActive is returned when Subscribe() is called while another subscription is active.
var ErrSubscriptionActive = errors.New("subscription already active")

// NewSubscriber creates a subscriber for a single topic/subscription.
// Topic format: "topic/subscription" for subscriptions, or "queue" for queues.
// The pipeline parameter is used for telemetry labels (e.g., "cache", "derived").
func NewSubscriber(client *azservicebus.Client, topic, pipeline string, config SubscriberConfig) (*Subscriber, error) {
	config = config.withDefaults()

	// Create receiver immediately (fail fast)
	receiver, err := createReceiver(client, topic)
	if err != nil {
		return nil, fmt.Errorf("create receiver for %s: %w", topic, err)
	}

	config.Logger.Info("Created subscriber", "topic", topic, "pipeline", pipeline, "max_in_flight", config.MaxInFlight)

	return &Subscriber{
		logger:    config.Logger,
		client:    client,
		receiver:  receiver,
		topic:     topic,
		config:    config,
		closeDone: make(chan struct{}),
		inFlight: semaphore.New(int64(config.MaxInFlight), semaphore.Config{
			Name:    "servicebus.settlement",
			Metrics: true,
			Labels: map[string]string{
				"subscription": topic,
				"pipeline":     pipeline,
			},
		}),
	}, nil
}

func createReceiver(client *azservicebus.Client, topic string) (*azservicebus.Receiver, error) {
	if parts := strings.Split(topic, "/"); len(parts) == 2 {
		return client.NewReceiverForSubscription(parts[0], parts[1], nil)
	}
	return client.NewReceiverForQueue(topic, nil)
}

// Subscribe starts receiving messages and returns a channel.
// The channel closes automatically when the context is cancelled, triggering graceful shutdown.
//
// Subscription Lifecycle:
//   - Only one subscription can be active at a time (returns ErrSubscriptionActive otherwise)
//   - Context cancellation triggers graceful shutdown of the subscription
//   - After shutdown completes, Subscribe() can be called again with a new context
//
// Graceful Shutdown (context cancellation):
//  1. Message reception stops immediately
//  2. In-flight handlers continue processing naturally (NOT signaled yet)
//  3. Wait up to ShutdownTimeout for handlers to settle (ack/nack)
//  4. If timeout: signal handlers to abandon, messages will be redelivered
//  5. Receiver stays open (subscriber can be reused)
//
// Each message is handled by a dedicated goroutine that manages lock renewal and settlement.
// Subscribe starts consuming messages from the subscription.
// The pipeline parameter is used for telemetry correlation (e.g., "cache", "derived").
//
// IMPORTANT: This method now updates semaphore metrics with pipeline/subscription labels.
// On first Subscribe() call, the semaphore labels are set and cannot be changed afterward.
func (s *Subscriber) Subscribe(ctx context.Context, pipeline string) (<-chan *message.RawMessage, error) {
	// Check if subscriber is permanently closed
	if s.closed.Load() {
		return nil, ErrSubscriberClosed
	}

	// Check if there's already an active subscription
	s.subMu.Lock()
	if s.activeSub != nil {
		s.subMu.Unlock()
		return nil, ErrSubscriptionActive
	}

	// Create subscription context that can be cancelled by Close()
	subCtx, subCancel := context.WithCancel(ctx)

	// Create new subscription state
	sub := &activeSubscription{
		abort:  make(chan struct{}),
		cancel: subCancel,
	}
	s.activeSub = sub
	s.subWg.Add(1)
	s.subMu.Unlock()

	msgChan := make(chan *message.RawMessage, s.config.MaxInFlight)

	go func() {
		defer s.subWg.Done()
		defer close(msgChan)
		defer s.logger.Debug("Receive loop stopped", "topic", s.topic)
		defer s.clearActiveSubscription()
		defer subCancel()              // Always cleanup
		defer s.drainSubscription(sub) // Always drain handlers on exit

		for {
			// Check for shutdown
			select {
			case <-subCtx.Done():
				return
			default:
			}

			// Get receiver under lock
			s.receiverMu.Lock()
			receiver := s.receiver
			s.receiverMu.Unlock()

			// Receive messages
			receiveStart := time.Now()
			msgs, err := receiver.ReceiveMessages(subCtx, s.config.PrefetchCount, nil)
			receiveDuration := time.Since(receiveStart)

			if err != nil {
				if s.handleReceiveError(subCtx, sub, err) {
					continue // Reconnected, retry
				}
				return // Fatal error or shutdown
			}

			tel.RecordReceive(subCtx, s.topic, pipeline, len(msgs), receiveDuration)
			receiveTime := time.Now()

			for _, sbMsg := range msgs {
				if err := s.inFlight.Acquire(subCtx, 1); err != nil {
					return // Context cancelled
				}

				attrs := s.extractAttrs(sbMsg)
				handler := s.newHandler(receiver, sbMsg, attrs, receiveTime, sub.abort, pipeline, msgChan)
				go handler.handle()
			}
		}
	}()

	return msgChan, nil
}

// clearActiveSubscription clears the active subscription state.
func (s *Subscriber) clearActiveSubscription() {
	s.subMu.Lock()
	s.activeSub = nil
	s.subMu.Unlock()
}

// newHandler creates a message handler with subscriber-level config
// and message-specific fields. Only pass fields that vary per message.
func (s *Subscriber) newHandler(
	receiver *azservicebus.Receiver,
	msg *azservicebus.ReceivedMessage,
	attrs message.Attributes,
	receiveTime time.Time,
	abort <-chan struct{},
	pipeline string,
	msgChan chan<- *message.RawMessage,
) *messageHandler {
	return &messageHandler{
		// Message-specific fields
		receiver:    receiver,
		msg:         msg,
		attrs:       attrs,
		receiveTime: receiveTime,
		msgChan:     msgChan,

		// Subscriber-level config (constant per subscriber)
		logger:               s.logger,
		topic:                s.topic,
		pipeline:             pipeline,
		renewalInterval:      s.config.LockRenewalInterval,
		operationTimeout:     s.config.OperationTimeout,
		disableLockRenewal:   s.config.DisableLockRenewal,
		subscriberProperties: s.config.Properties,
		inFlight:             s.inFlight,
		abort:                abort,
	}
}

// handleReceiveError handles errors from ReceiveMessages.
// Returns true if reconnected and should retry, false if should exit.
func (s *Subscriber) handleReceiveError(ctx context.Context, sub *activeSubscription, err error) bool {
	// Normal shutdown (context cancelled by user or Close())
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Check if we're closing
	if s.closed.Load() {
		return false
	}

	// Connection error - try to reconnect with backoff
	var sbErr *azservicebus.Error
	if errors.As(err, &sbErr) && (sbErr.Code == azservicebus.CodeConnectionLost || sbErr.Code == azservicebus.CodeClosed) {
		s.logger.Info("Connection lost, reconnecting", "topic", s.topic, "code", string(sbErr.Code))

		for {
			// Check shutdown before reconnect attempt
			if s.closed.Load() {
				s.logger.Info("Subscriber closing, stopping reconnect", "topic", s.topic)
				return false
			}

			if err := s.reconnect(); err != nil {
				s.logger.Error("Reconnect failed, retrying", "error", err, "topic", s.topic, "backoff", s.config.ReconnectBackoff)

				// Backoff before retry
				select {
				case <-ctx.Done():
					return false
				case <-time.After(s.config.ReconnectBackoff):
					continue
				}
			}

			return true // Reconnected successfully
		}
	}

	// Other error - log and exit
	s.logger.Error("Receive error", "error", err, "topic", s.topic)
	return false
}

func (s *Subscriber) reconnect() error {
	s.receiverMu.Lock()
	defer s.receiverMu.Unlock()

	// Close old receiver
	ctx, cancel := context.WithTimeout(context.Background(), s.config.OperationTimeout)
	s.receiver.Close(ctx)
	cancel()

	// Create new receiver
	receiver, err := createReceiver(s.client, s.topic)
	if err != nil {
		return err
	}

	s.receiver = receiver
	s.logger.Info("Reconnected", "topic", s.topic)
	return nil
}

func (s *Subscriber) extractAttrs(sbMsg *azservicebus.ReceivedMessage) message.Attributes {
	attrs := message.Attributes{}

	// Content type
	if sbMsg.ContentType != nil {
		attrs[message.AttrDataContentType] = *sbMsg.ContentType
	}

	// CloudEvents attributes from application properties
	for key, value := range sbMsg.ApplicationProperties {
		if stripped, ok := strings.CutPrefix(key, cloudEventsPrefix); ok {
			attrs[stripped] = value
		} else {
			attrs[key] = value
		}
	}

	// Fallback to native fields
	if _, hasID := attrs[message.AttrID]; !hasID {
		attrs[message.AttrID] = sbMsg.MessageID
	}
	if _, hasCorrID := attrs[message.AttrCorrelationID]; !hasCorrID && sbMsg.CorrelationID != nil {
		attrs[message.AttrCorrelationID] = *sbMsg.CorrelationID
	}

	return attrs
}

// isReceiverInvalidError checks if error indicates receiver is no longer valid.
func isReceiverInvalidError(err error) bool {
	var sbErr *azservicebus.Error
	if errors.As(err, &sbErr) {
		switch sbErr.Code {
		case azservicebus.CodeLockLost, azservicebus.CodeClosed, azservicebus.CodeConnectionLost:
			return true
		}
	}
	return false
}

func getErrorCode(err error) string {
	var sbErr *azservicebus.Error
	if errors.As(err, &sbErr) {
		return string(sbErr.Code)
	}
	return "unknown"
}

// messageHandler handles a single message's lifecycle:
// build → send to channel → renewal loop → settlement
type messageHandler struct {
	receiver             *azservicebus.Receiver
	msg                  *azservicebus.ReceivedMessage
	attrs                message.Attributes
	msgChan              chan<- *message.RawMessage
	logger               *slog.Logger
	topic                string
	pipeline             string
	renewalInterval      time.Duration // 0 = auto-detect
	operationTimeout     time.Duration
	disableLockRenewal   bool
	subscriberProperties SubscriberProperties
	inFlight             *semaphore.Semaphore
	receiveTime          time.Time
	abort                <-chan struct{} // subscription abort signal (closed on timeout or Close())
}

// msgType extracts the CloudEvents type from the message attributes.
func (h *messageHandler) msgType() string {
	if t, ok := h.attrs[message.AttrType].(string); ok && t != "" {
		return t
	}
	return ""
}

// handle runs the complete message lifecycle in a single goroutine.
func (h *messageHandler) handle() {
	defer h.inFlight.Release(1)
	defer func() {
		tel.RecordProcessingDuration(context.Background(), h.topic, h.pipeline, h.msgType(), time.Since(h.receiveTime))
	}()

	// === Phase 1: Build message and send to pipeline ===
	result := make(chan error, 1)
	acking := message.NewAcking(
		func() { result <- nil },
		func(e error) { result <- e },
	)
	pipelineMsg := message.NewRaw(h.msg.Body, h.attrs, acking)
	h.subscriberProperties.apply(h.msg, pipelineMsg)

	// Measure channel send duration (backpressure indicator)
	sendStart := time.Now()
	select {
	case h.msgChan <- pipelineMsg:
		sendDuration := time.Since(sendStart)

		// Record channel metrics
		depth := len(h.msgChan)
		capacity := cap(h.msgChan)
		tel.RecordChannelSend(context.Background(), h.topic, h.pipeline, sendDuration, depth, capacity)

	case <-h.abort:
		h.settle(ErrSubscriberClosed)
		return
	}

	// === Phase 2: Lock renewal loop until settlement ===

	// Calculate renewal interval
	renewalInterval := h.renewalInterval
	if renewalInterval == 0 && h.msg.LockedUntil != nil {
		if d := time.Until(*h.msg.LockedUntil); d > 0 {
			renewalInterval = d / 2
		}
	}
	if renewalInterval == 0 {
		renewalInterval = DefaultLockRenewalInterval
	}

	// Lock renewal ticker (optional)
	var tickerC <-chan time.Time
	if !h.disableLockRenewal {
		ticker := time.NewTicker(renewalInterval)
		defer ticker.Stop()
		tickerC = ticker.C
	}

	for {
		select {
		case err := <-result:
			h.settle(err)
			return

		case <-tickerC:
			// Use background context for renewal - don't let subscriber context timeout kill it
			renewCtx, renewCancel := context.WithTimeout(context.Background(), h.operationTimeout)
			err := h.receiver.RenewMessageLock(renewCtx, h.msg, nil)
			renewCancel()

			if err != nil {
				if isReceiverInvalidError(err) {
					h.logger.Warn("Lock lost, message will be redelivered",
						"reason", getErrorCode(err), "message_id", h.msg.MessageID, "topic", h.topic)
					tel.RecordLockLost(context.Background(), h.topic, h.pipeline, h.msgType())
					return
				}
				h.logger.Warn("Lock renewal failed", "error", err,
					"message_id", h.msg.MessageID, "topic", h.topic)
			} else {
				tel.RecordLockRenewal(context.Background(), h.topic, h.pipeline, h.msgType())
			}

		case <-h.abort:
			h.settle(ErrSubscriberClosed)
			return
		}
	}
}

// settle completes or abandons the message with Service Bus.
func (h *messageHandler) settle(err error) {
	opCtx, cancel := context.WithTimeout(context.Background(), h.operationTimeout)
	defer cancel()

	if err == nil {
		tel.RecordAck(context.Background(), h.topic, h.pipeline, h.msgType())
		if settleErr := h.receiver.CompleteMessage(opCtx, h.msg, nil); settleErr != nil {
			if h.isExpectedSettlementError(settleErr) {
				h.logger.Warn("Cannot complete message, will be redelivered",
					"reason", settlementErrorReason(settleErr), "message_id", h.msg.MessageID, "topic", h.topic)
				return
			}
			h.logger.Error("Failed to complete message",
				"error", settleErr, "message_id", h.msg.MessageID, "topic", h.topic)
		}
	} else {
		tel.RecordNack(context.Background(), h.topic, h.pipeline, h.msgType())
		if settleErr := h.receiver.AbandonMessage(opCtx, h.msg, nil); settleErr != nil {
			if h.isExpectedSettlementError(settleErr) {
				h.logger.Warn("Cannot abandon message, will be redelivered",
					"reason", settlementErrorReason(settleErr), "original_error", err,
					"message_id", h.msg.MessageID, "topic", h.topic)
				return
			}
			h.logger.Error("Failed to abandon message",
				"error", settleErr, "original_error", err,
				"message_id", h.msg.MessageID, "topic", h.topic)
			return
		}
		h.logger.Warn("Message abandoned", "original_error", err,
			"message_id", h.msg.MessageID, "topic", h.topic)
	}
}

// isExpectedSettlementError checks if an error is expected during shutdown or reconnect.
// Returns true for Service Bus errors (lock lost, connection lost) and context errors
// during shutdown. Context errors outside of shutdown are unexpected (e.g., Service Bus slow).
func (h *messageHandler) isExpectedSettlementError(err error) bool {
	if isReceiverInvalidError(err) {
		return true
	}
	// Context errors are only expected during shutdown
	if h.isShuttingDown() && isContextError(err) {
		return true
	}
	return false
}

// isShuttingDown checks if the subscription is being aborted.
func (h *messageHandler) isShuttingDown() bool {
	select {
	case <-h.abort:
		return true
	default:
		return false
	}
}

// isContextError checks if an error is a context cancellation or deadline exceeded.
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// settlementErrorReason returns a human-readable reason for a settlement error.
func settlementErrorReason(err error) string {
	if errors.Is(err, context.Canceled) {
		return "shutdown"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return getErrorCode(err)
}

// drainSubscription waits for in-flight messages to settle gracefully.
// Called when context is cancelled or Close() is invoked.
// Does NOT close the receiver - that's done by Close() only.
func (s *Subscriber) drainSubscription(sub *activeSubscription) {
	s.logger.Info("Draining subscription", "topic", s.topic)

	// Phase 1: Wait for in-flight handlers to settle gracefully (without signaling them)
	// Acquiring all MaxInFlight slots means all messages have been acked/nacked by handlers
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	if err := s.inFlight.Acquire(shutdownCtx, int64(s.config.MaxInFlight)); err != nil {
		// Timeout: handlers didn't finish in time
		unsettledCount := s.config.MaxInFlight - int(s.inFlight.Available())
		s.logger.Warn("Drain timeout, forcing handler abandonment",
			"unsettled", unsettledCount,
			"timeout", s.config.ShutdownTimeout,
			"note", "messages will be redelivered by ServiceBus",
			"topic", s.topic)

		// Phase 2: Force abandonment - signal remaining handlers
		sub.abortOnce.Do(func() { close(sub.abort) })

		// Give handlers a moment to react to abort signal and abandon
		time.Sleep(100 * time.Millisecond)
	} else {
		// Success: all handlers settled gracefully
		s.logger.Debug("All messages settled gracefully", "topic", s.topic)
		s.inFlight.Release(int64(s.config.MaxInFlight))
	}

	s.logger.Info("Subscription drained", "topic", s.topic)
}

// Close permanently closes the subscriber and releases the receiver connection.
// If a subscription is active, it will be gracefully drained first.
// After Close(), Subscribe() returns ErrSubscriberClosed.
// Close is safe to call multiple times.
func (s *Subscriber) Close() error {
	s.closeOnce.Do(func() {
		s.logger.Info("Closing subscriber", "topic", s.topic)

		// Mark as permanently closed
		s.closed.Store(true)

		// If there's an active subscription, cancel it to stop ReceiveMessages
		s.subMu.Lock()
		if s.activeSub != nil {
			s.activeSub.cancel()
		}
		s.subMu.Unlock()

		// Wait for subscription goroutine to exit (it will call drainSubscription)
		s.subWg.Wait()

		// Close receiver
		closeCtx, cancel := context.WithTimeout(context.Background(), s.config.OperationTimeout)
		defer cancel()

		if err := s.receiver.Close(closeCtx); err != nil {
			s.logger.Error("Failed to close receiver", "error", err, "topic", s.topic)
			s.closeErr = err
		}

		s.logger.Info("Subscriber closed", "topic", s.topic)
		close(s.closeDone)
	})

	<-s.closeDone
	return s.closeErr
}
