package azservicebus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe/message"
	"github.com/fxsml/gopipe/pipe"
	"github.com/google/uuid"
)

// PublisherConfig contains configuration for the Publisher.
type PublisherConfig struct {
	// Logger for the publisher. Defaults to slog.Default().
	Logger *slog.Logger

	// ErrorHandler is called when batch publishing fails in streaming Publish().
	// The batch contains the messages that failed to publish.
	// Defaults to logging via Logger.
	ErrorHandler func(batch []*message.RawMessage, err error)

	// PublishTimeout is the timeout for each publish operation.
	// Defaults to 60 seconds.
	PublishTimeout time.Duration

	// ShutdownTimeout is the maximum time allowed for graceful shutdown.
	// Defaults to 30 seconds.
	ShutdownTimeout time.Duration

	// OperationTimeout is the timeout for ServiceBus operations.
	// Defaults to 5 seconds.
	OperationTimeout time.Duration

	// BatchSize is the maximum number of messages per batch for streaming Publish.
	// Defaults to 100.
	BatchSize int

	// BatchTimeout is the maximum duration to wait before sending a partial batch.
	// Defaults to 1 second.
	BatchTimeout time.Duration

	// Worker is the number of concurrent workers for streaming Publish.
	// Defaults to 10.
	Worker int

	// Properties controls Azure Service Bus broker-level message properties.
	// Each field is a function receiving the outgoing message and returning the
	// property value; a nil function or empty/zero return value leaves the field unset.
	Properties PublisherProperties
}

// PublisherProperties holds per-field resolver functions for Azure Service Bus broker properties.
// Resolvers receive the outgoing message so values can be derived statically or dynamically.
// Nil functions and empty/zero return values are ignored.
type PublisherProperties struct {
	SessionID            func(*message.RawMessage) string
	ReplyTo              func(*message.RawMessage) string
	ReplyToSessionID     func(*message.RawMessage) string
	To                   func(*message.RawMessage) string
	Subject              func(*message.RawMessage) string
	PartitionKey         func(*message.RawMessage) string
	TimeToLive           func(*message.RawMessage) time.Duration
	ScheduledEnqueueTime func(*message.RawMessage) time.Time
}

func (c PublisherConfig) withDefaults() PublisherConfig {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.ErrorHandler == nil {
		// Default is no-op since PublishBatch logs errors with classification via logPublishError.
		// Users can provide custom ErrorHandler for additional handling (alerting, custom metrics, etc.).
		c.ErrorHandler = func(batch []*message.RawMessage, err error) {}
	}
	if c.PublishTimeout <= 0 {
		c.PublishTimeout = 60 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 30 * time.Second
	}
	if c.OperationTimeout <= 0 {
		c.OperationTimeout = 5 * time.Second
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.BatchTimeout <= 0 {
		c.BatchTimeout = time.Second
	}
	if c.Worker <= 0 {
		c.Worker = 10
	}
	return c
}

// Publisher publishes messages to a single Azure Service Bus topic or queue.
// One instance = one topic. Create multiple instances for multiple topics.
type Publisher struct {
	logger *slog.Logger
	client *azservicebus.Client
	sender *azservicebus.Sender
	topic  string
	config PublisherConfig

	done     chan struct{}
	doneOnce sync.Once
	wg       sync.WaitGroup // tracks in-flight operations
	mu       sync.Mutex     // protects sender during reconnect
}

// ErrPublisherClosed is returned when operations are attempted on a closed publisher.
var ErrPublisherClosed = errors.New("publisher is closed")

// NewPublisher creates a publisher for a single topic.
func NewPublisher(client *azservicebus.Client, topic string, config PublisherConfig) (*Publisher, error) {
	config = config.withDefaults()

	// Create sender immediately (fail fast)
	sender, err := client.NewSender(topic, nil)
	if err != nil {
		return nil, fmt.Errorf("create sender for %s: %w", topic, err)
	}

	config.Logger.Info("Created publisher", "topic", topic)

	return &Publisher{
		logger: config.Logger,
		client: client,
		sender: sender,
		topic:  topic,
		config: config,
		done:   make(chan struct{}),
	}, nil
}

// PublishBatch publishes messages to the topic.
// The pipeline parameter is used for telemetry correlation (e.g., "cache", "derived").
// Successfully published messages are Acked; on error, unpublished messages are Nacked.
func (p *Publisher) PublishBatch(ctx context.Context, pipeline string, messages ...*message.RawMessage) error {
	if len(messages) == 0 {
		return nil
	}

	select {
	case <-p.done:
		nackAll(messages, ErrPublisherClosed)
		return ErrPublisherClosed
	default:
	}

	p.wg.Add(1)
	defer p.wg.Done()

	startTime := time.Now()

	// Use detached context with timeout for graceful shutdown
	publishCtx, cancel := context.WithTimeout(context.Background(), p.config.PublishTimeout)
	defer cancel()

	published := 0
	splitCount := 0
	for published < len(messages) {
		// Create batch with retry on connection error
		createStart := time.Now()
		batch, err := p.createBatchWithRetry(publishCtx)
		createDuration := time.Since(createStart)

		if err != nil {
			errClass := classifyError(err)
			tel.RecordPublishError(ctx, p.topic, pipeline, errClass)
			logPublishError(p.logger, err, errClass, p.topic, len(messages)-published)
			nackAll(messages[published:], err)
			return err
		}

		// Add messages to batch
		added := 0
		for i := published; i < len(messages); i++ {
			sbMsg := p.toServiceBusMessage(messages[i])
			if err := batch.AddMessage(sbMsg, nil); err != nil {
				tel.RecordMessageAdd(ctx, p.topic, pipeline, false)
				if errors.Is(err, azservicebus.ErrMessageTooLarge) {
					if added == 0 {
						wrappedErr := fmt.Errorf("message %s too large: %w", messages[i].ID(), err)
						errClass := classifyError(wrappedErr)
						tel.RecordPublishError(ctx, p.topic, pipeline, errClass)
						logPublishError(p.logger, wrappedErr, errClass, p.topic, 1)
						nackAll(messages[published:], wrappedErr)
						return wrappedErr
					}
					break // Batch full, send what we have
				}
				wrappedErr := fmt.Errorf("add to batch: %w", err)
				errClass := classifyError(wrappedErr)
				tel.RecordPublishError(ctx, p.topic, pipeline, errClass)
				logPublishError(p.logger, wrappedErr, errClass, p.topic, len(messages)-published)
				nackAll(messages[published:], wrappedErr)
				return wrappedErr
			}
			tel.RecordMessageAdd(ctx, p.topic, pipeline, true)
			added++
		}

		// Send batch with retry on connection error
		sendStart := time.Now()
		if err := p.sendBatchWithRetry(publishCtx, batch, messages[published:published+added]); err != nil {
			errClass := classifyError(err)
			tel.RecordPublishError(ctx, p.topic, pipeline, errClass)
			logPublishError(p.logger, err, errClass, p.topic, added)
			nackAll(messages[published:], err)
			return err
		}
		sendDuration := time.Since(sendStart)

		// Record ServiceBus send metrics
		tel.RecordServiceBusSend(ctx, p.topic, pipeline, added, createDuration, sendDuration)
		splitCount++

		// Ack published messages
		for i := published; i < published+added; i++ {
			messages[i].Ack()
		}
		published += added
		p.logger.Debug("Published batch", "topic", p.topic, "count", added)
	}

	tel.RecordPublish(ctx, p.topic, pipeline, len(messages), time.Since(startTime), splitCount)
	return nil
}

// Publish consumes messages from the input channel and publishes them to the topic.
// The pipeline parameter is used for telemetry correlation (e.g., "cache", "derived").
// Messages are automatically batched for efficient network usage (see BatchSize, BatchTimeout).
// Returns a done channel that closes when all messages are published or the publisher is closed.
func (p *Publisher) Publish(ctx context.Context, pipeline string, in <-chan *message.RawMessage) (<-chan struct{}, error) {
	select {
	case <-p.done:
		return nil, ErrPublisherClosed
	default:
	}

	// Context that cancels when either parent ctx or publisher closes
	pipeCtx, cancel := context.WithCancel(ctx)

	batchPipe := pipe.NewBatchPipe(
		func(ctx context.Context, batch []*message.RawMessage) ([]struct{}, error) {
			return nil, p.PublishBatch(ctx, pipeline, batch...)
		},
		pipe.BatchConfig{
			MaxSize:     p.config.BatchSize,
			MaxDuration: p.config.BatchTimeout,
			Config: pipe.Config{
				Concurrency:     p.config.Worker,
				ShutdownTimeout: p.config.OperationTimeout,
				ErrorHandler: func(in any, err error) {
					batch := in.([]*message.RawMessage)
					p.config.ErrorHandler(batch, err)
				},
			},
		},
	)

	done, err := batchPipe.Pipe(pipeCtx, in)
	if err != nil {
		cancel()
		return nil, err
	}

	// Watch for publisher close to cancel the pipe
	go func() {
		select {
		case <-done:
			// Pipe completed naturally
		case <-p.done:
			// Publisher closing, cancel the pipe
		}
		cancel() // Always cleanup context
	}()

	return done, nil
}

// createBatchWithRetry creates a batch, retrying once on connection error.
func (p *Publisher) createBatchWithRetry(ctx context.Context) (*azservicebus.MessageBatch, error) {
	p.mu.Lock()
	sender := p.sender
	p.mu.Unlock()

	batch, err := sender.NewMessageBatch(ctx, nil)
	if err == nil {
		return batch, nil
	}

	if !isConnectionError(err) {
		return nil, fmt.Errorf("create batch: %w", err)
	}

	// Reconnect and retry
	if err := p.reconnect(ctx); err != nil {
		return nil, err
	}

	p.mu.Lock()
	sender = p.sender
	p.mu.Unlock()

	batch, err = sender.NewMessageBatch(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("create batch after reconnect: %w", err)
	}
	return batch, nil
}

// sendBatchWithRetry sends the batch, retrying once on connection error.
// On retry, it rebuilds the batch with the new sender since batches are sender-specific.
func (p *Publisher) sendBatchWithRetry(ctx context.Context, batch *azservicebus.MessageBatch, messages []*message.RawMessage) error {
	p.mu.Lock()
	sender := p.sender
	p.mu.Unlock()

	// Send the pre-built batch
	err := sender.SendMessageBatch(ctx, batch, nil)
	if err == nil {
		return nil
	}

	if !isConnectionError(err) {
		return fmt.Errorf("send batch: %w", err)
	}

	// Reconnect and retry
	if err := p.reconnect(ctx); err != nil {
		return err
	}

	p.mu.Lock()
	sender = p.sender
	p.mu.Unlock()

	// Rebuild batch with new sender (batches are tied to their sender)
	retryBatch, err := sender.NewMessageBatch(ctx, nil)
	if err != nil {
		return fmt.Errorf("create retry batch: %w", err)
	}
	for _, msg := range messages {
		if err := retryBatch.AddMessage(p.toServiceBusMessage(msg), nil); err != nil {
			return fmt.Errorf("add to retry batch: %w", err)
		}
	}
	if err := sender.SendMessageBatch(ctx, retryBatch, nil); err != nil {
		return fmt.Errorf("send batch after reconnect: %w", err)
	}
	return nil
}

func (p *Publisher) reconnect(ctx context.Context) error {
	// Check shutdown
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return ErrPublisherClosed
	default:
	}

	p.logger.Info("Connection lost, reconnecting", "topic", p.topic)

	p.mu.Lock()
	defer p.mu.Unlock()

	// Close old sender
	closeCtx, cancel := context.WithTimeout(context.Background(), p.config.OperationTimeout)
	p.sender.Close(closeCtx)
	cancel()

	// Create new sender
	sender, err := p.client.NewSender(p.topic, nil)
	if err != nil {
		p.logger.Error("Reconnect failed", "error", err, "topic", p.topic)
		return fmt.Errorf("reconnect: %w", err)
	}

	p.sender = sender
	p.logger.Info("Reconnected", "topic", p.topic)
	return nil
}

func isConnectionError(err error) bool {
	var sbErr *azservicebus.Error
	return errors.As(err, &sbErr) &&
		(sbErr.Code == azservicebus.CodeConnectionLost || sbErr.Code == azservicebus.CodeClosed)
}

// ErrorClass categorizes Service Bus errors for telemetry and handling.
type ErrorClass string

const (
	ErrorClassNone       ErrorClass = ""
	ErrorClassThrottling ErrorClass = "throttling"
	ErrorClassConnection ErrorClass = "connection"
	ErrorClassTimeout    ErrorClass = "timeout"
	ErrorClassAuth       ErrorClass = "auth"
	ErrorClassNotFound   ErrorClass = "not_found"
	ErrorClassUnknown    ErrorClass = "unknown"
)

// logPublishError logs the error with appropriate level based on classification.
func logPublishError(logger *slog.Logger, err error, errClass ErrorClass, topic string, batchSize int) {
	attrs := []any{
		"error", err,
		"error_class", string(errClass),
		"topic", topic,
		"batch_size", batchSize,
	}

	switch errClass {
	case ErrorClassThrottling:
		logger.Error("Publishing throttled by Azure Service Bus", attrs...)
	case ErrorClassConnection:
		logger.Error("Connection error during publish", attrs...)
	case ErrorClassTimeout:
		logger.Error("Publish operation timed out", attrs...)
	default:
		logger.Error("Publish failed", attrs...)
	}
}

// classifyError categorizes an error for telemetry and handling decisions.
func classifyError(err error) ErrorClass {
	if err == nil {
		return ErrorClassNone
	}

	errStr := err.Error()

	// Throttling: Azure returns error code 50009
	if strings.Contains(errStr, "50009") ||
		strings.Contains(errStr, "being throttled") ||
		strings.Contains(errStr, "ServerBusy") {
		return ErrorClassThrottling
	}

	// Check SDK error codes
	var sbErr *azservicebus.Error
	if errors.As(err, &sbErr) {
		switch sbErr.Code {
		case azservicebus.CodeConnectionLost, azservicebus.CodeClosed:
			return ErrorClassConnection
		case azservicebus.CodeTimeout:
			return ErrorClassTimeout
		case azservicebus.CodeUnauthorizedAccess:
			return ErrorClassAuth
		case azservicebus.CodeNotFound:
			return ErrorClassNotFound
		}
	}

	// Context errors
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrorClassTimeout
	}

	return ErrorClassUnknown
}

func (p *Publisher) toServiceBusMessage(msg *message.RawMessage) *azservicebus.Message {
	msgID := msg.ID()
	if msgID == "" {
		msgID = uuid.New().String()
	}

	sbMsg := &azservicebus.Message{
		Body:                  msg.Data,
		MessageID:             &msgID,
		ApplicationProperties: make(map[string]any),
	}

	if ct := msg.DataContentType(); ct != "" {
		sbMsg.ContentType = &ct
	}

	if corrID := msg.CorrelationID(); corrID != "" {
		sbMsg.CorrelationID = &corrID
	}

	pp := p.config.Properties
	if pp.SessionID != nil {
		v := pp.SessionID(msg)
		if v != "" {
			sbMsg.SessionID = &v
		}
	}
	if pp.ReplyTo != nil {
		v := pp.ReplyTo(msg)
		if v != "" {
			sbMsg.ReplyTo = &v
		}
	}
	if pp.ReplyToSessionID != nil {
		v := pp.ReplyToSessionID(msg)
		if v != "" {
			sbMsg.ReplyToSessionID = &v
		}
	}
	if pp.To != nil {
		v := pp.To(msg)
		if v != "" {
			sbMsg.To = &v
		}
	}
	if pp.Subject != nil {
		v := pp.Subject(msg)
		if v != "" {
			sbMsg.Subject = &v
		}
	}
	if pp.PartitionKey != nil {
		v := pp.PartitionKey(msg)
		if v != "" {
			sbMsg.PartitionKey = &v
		}
	}
	if pp.TimeToLive != nil {
		d := pp.TimeToLive(msg)
		if d > 0 {
			sbMsg.TimeToLive = &d
		}
	}
	if pp.ScheduledEnqueueTime != nil {
		t := pp.ScheduledEnqueueTime(msg)
		if !t.IsZero() {
			sbMsg.ScheduledEnqueueTime = &t
		}
	}

	// CloudEvents attributes to application properties
	for key, value := range msg.Attributes {
		if key == message.AttrDataContentType {
			continue
		}
		sbMsg.ApplicationProperties[cloudEventsPrefix+key] = value
	}

	return sbMsg
}

func nackAll(messages []*message.RawMessage, err error) {
	for _, msg := range messages {
		msg.Nack(err)
	}
}

// Close stops the publisher and waits for in-flight operations.
func (p *Publisher) Close() error {
	p.logger.Info("Closing publisher", "topic", p.topic)

	// Signal shutdown
	p.doneOnce.Do(func() { close(p.done) })

	// Wait for in-flight operations
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Debug("All in-flight operations completed", "topic", p.topic)
	case <-time.After(p.config.ShutdownTimeout):
		p.logger.Warn("Timeout waiting for in-flight operations", "topic", p.topic)
	}

	// Close sender
	ctx, cancel := context.WithTimeout(context.Background(), p.config.OperationTimeout)
	defer cancel()

	return p.sender.Close(ctx)
}
