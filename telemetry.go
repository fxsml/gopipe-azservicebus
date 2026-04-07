package azservicebus

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var tel *telemetry

type telemetry struct {
	// Publisher metrics
	publishedTotal                metric.Int64Counter
	publishDuration               metric.Float64Histogram
	publishInputBatchSize         metric.Int64Histogram
	publishServiceBusBatchSize    metric.Int64Histogram
	publishBatchSplits            metric.Int64Histogram
	publishServiceBusSendDuration metric.Float64Histogram
	publishBatchCreateDuration    metric.Float64Histogram
	publishMessageAddTotal        metric.Int64Counter
	publishMessageAddFailures     metric.Int64Counter
	publishErrorsTotal            metric.Int64Counter

	// Subscriber metrics
	receivedTotal      metric.Int64Counter
	receiveBatchSize   metric.Int64Histogram
	receiveDuration    metric.Float64Histogram
	ackedTotal         metric.Int64Counter
	nackedTotal        metric.Int64Counter
	processingDuration metric.Float64Histogram
	lockRenewalsTotal  metric.Int64Counter
	lockLostTotal      metric.Int64Counter

	// Channel backpressure metrics
	channelSendDuration metric.Float64Histogram
	channelDepth        metric.Int64Gauge
	channelCapacity     metric.Int64Gauge
	channelUtilization  metric.Float64Gauge
}

func init() {
	meter := otel.Meter("servicebus")

	// Publisher metrics
	publishedTotal, _ := meter.Int64Counter("servicebus_messages_published_total",
		metric.WithDescription("Total number of messages published to ServiceBus"))

	publishDuration, _ := meter.Float64Histogram("servicebus_publish_duration_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of entire PublishBatch operation (may include multiple ServiceBus sends)"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60))

	publishInputBatchSize, _ := meter.Int64Histogram("servicebus_publish_input_batch_size",
		metric.WithDescription("Size of input batch from BatchPipe"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000))

	publishServiceBusBatchSize, _ := meter.Int64Histogram("servicebus_publish_servicebus_batch_size",
		metric.WithDescription("Actual batch size sent to ServiceBus (may be split from input batch)"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000))

	publishBatchSplits, _ := meter.Int64Histogram("servicebus_publish_batch_splits",
		metric.WithDescription("Number of ServiceBus batches needed per input batch (1=no split, 2+=split occurred)"),
		metric.WithExplicitBucketBoundaries(1, 2, 3, 5, 10, 20, 50))

	publishServiceBusSendDuration, _ := meter.Float64Histogram("servicebus_publish_servicebus_send_duration_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of individual SendMessageBatch call to Azure Service Bus"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60))

	publishBatchCreateDuration, _ := meter.Float64Histogram("servicebus_publish_batch_create_duration_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Duration to create ServiceBus MessageBatch (memory allocation)"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1))

	publishMessageAddTotal, _ := meter.Int64Counter("servicebus_publish_message_add_total",
		metric.WithDescription("Total number of message add attempts to ServiceBus batch"))

	publishMessageAddFailures, _ := meter.Int64Counter("servicebus_publish_message_add_failures_total",
		metric.WithDescription("Number of failed message add attempts (too large, etc.)"))

	publishErrorsTotal, _ := meter.Int64Counter("servicebus_publish_errors_total",
		metric.WithDescription("Total number of publish errors by error class"))

	// Subscriber metrics
	receivedTotal, _ := meter.Int64Counter("servicebus_messages_received_total",
		metric.WithDescription("Total number of messages received from ServiceBus"))

	receiveBatchSize, _ := meter.Int64Histogram("servicebus_receive_batch_size",
		metric.WithDescription("Number of messages per ServiceBus receive call"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000))

	receiveDuration, _ := meter.Float64Histogram("servicebus_receive_duration_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of ServiceBus ReceiveMessages call"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60))

	ackedTotal, _ := meter.Int64Counter("servicebus_messages_acked_total",
		metric.WithDescription("Total number of messages acknowledged"))

	nackedTotal, _ := meter.Int64Counter("servicebus_messages_nacked_total",
		metric.WithDescription("Total number of messages rejected (nacked)"))

	processingDuration, _ := meter.Float64Histogram("servicebus_message_processing_duration_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Duration from message receive to settlement"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300))

	lockRenewalsTotal, _ := meter.Int64Counter("servicebus_message_lock_renewals_total",
		metric.WithDescription("Total number of successful message lock renewals"))

	lockLostTotal, _ := meter.Int64Counter("servicebus_message_lock_lost_total",
		metric.WithDescription("Total number of messages where lock was lost before settlement"))

	// Channel backpressure metrics
	channelSendDuration, _ := meter.Float64Histogram("servicebus_channel_send_duration_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Time spent waiting to send message to output channel (backpressure indicator)"),
		metric.WithExplicitBucketBoundaries(0, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60, 120))

	channelDepth, _ := meter.Int64Gauge("servicebus_channel_depth",
		metric.WithDescription("Current number of messages in output channel"))

	channelCapacity, _ := meter.Int64Gauge("servicebus_channel_capacity",
		metric.WithDescription("Maximum capacity of output channel"))

	channelUtilization, _ := meter.Float64Gauge("servicebus_channel_utilization_ratio",
		metric.WithDescription("Output channel utilization ratio (depth/capacity)"))

	tel = &telemetry{
		publishedTotal:                publishedTotal,
		publishDuration:               publishDuration,
		publishInputBatchSize:         publishInputBatchSize,
		publishServiceBusBatchSize:    publishServiceBusBatchSize,
		publishBatchSplits:            publishBatchSplits,
		publishServiceBusSendDuration: publishServiceBusSendDuration,
		publishBatchCreateDuration:    publishBatchCreateDuration,
		publishMessageAddTotal:        publishMessageAddTotal,
		publishMessageAddFailures:     publishMessageAddFailures,
		publishErrorsTotal:            publishErrorsTotal,
		receivedTotal:                 receivedTotal,
		receiveBatchSize:    receiveBatchSize,
		receiveDuration:     receiveDuration,
		ackedTotal:          ackedTotal,
		nackedTotal:         nackedTotal,
		processingDuration:  processingDuration,
		lockRenewalsTotal:   lockRenewalsTotal,
		lockLostTotal:       lockLostTotal,
		channelSendDuration: channelSendDuration,
		channelDepth:        channelDepth,
		channelCapacity:     channelCapacity,
		channelUtilization:  channelUtilization,
	}
}

// RecordPublish records metrics for a successful publish batch.
// This tracks batch-level operations: how many messages were published,
// how long the batch took, and the input batch size distribution.
func (t *telemetry) RecordPublish(ctx context.Context, topic, pipeline string, count int, duration time.Duration, splitCount int) {
	attrs := metric.WithAttributes(
		attribute.String("topic", topic),
		attribute.String("pipeline", pipeline),
		attribute.String("stage", "publish"),
	)
	t.publishedTotal.Add(ctx, int64(count), attrs)
	t.publishDuration.Record(ctx, duration.Seconds(), attrs)
	t.publishInputBatchSize.Record(ctx, int64(count), attrs)
	t.publishBatchSplits.Record(ctx, int64(splitCount), attrs)
}

// RecordServiceBusSend records metrics for an individual ServiceBus batch send.
func (t *telemetry) RecordServiceBusSend(ctx context.Context, topic, pipeline string, batchSize int, createDuration, sendDuration time.Duration) {
	attrs := metric.WithAttributes(
		attribute.String("topic", topic),
		attribute.String("pipeline", pipeline),
		attribute.String("stage", "publish"),
	)
	t.publishServiceBusBatchSize.Record(ctx, int64(batchSize), attrs)
	t.publishBatchCreateDuration.Record(ctx, createDuration.Seconds(), attrs)
	t.publishServiceBusSendDuration.Record(ctx, sendDuration.Seconds(), attrs)
}

// RecordMessageAdd records metrics for adding a message to a ServiceBus batch.
func (t *telemetry) RecordMessageAdd(ctx context.Context, topic, pipeline string, success bool) {
	attrs := metric.WithAttributes(
		attribute.String("topic", topic),
		attribute.String("pipeline", pipeline),
		attribute.String("stage", "publish"),
	)
	t.publishMessageAddTotal.Add(ctx, 1, attrs)
	if !success {
		t.publishMessageAddFailures.Add(ctx, 1, attrs)
	}
}

// RecordPublishError records a publish error with its classification.
func (t *telemetry) RecordPublishError(ctx context.Context, topic, pipeline string, errClass ErrorClass) {
	t.publishErrorsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("topic", topic),
		attribute.String("pipeline", pipeline),
		attribute.String("stage", "publish"),
		attribute.String("error_class", string(errClass)),
	))
}

// RecordReceive records metrics for received messages.
// This tracks batch-level receives. Individual message types are tracked
// via RecordProcessingDuration, RecordAck, and RecordNack.
func (t *telemetry) RecordReceive(ctx context.Context, subscription, pipeline string, count int, duration time.Duration) {
	attrs := metric.WithAttributes(
		attribute.String("subscription", subscription),
		attribute.String("pipeline", pipeline),
		attribute.String("stage", "receive"),
	)
	t.receivedTotal.Add(ctx, int64(count), attrs)
	t.receiveBatchSize.Record(ctx, int64(count), attrs)
	t.receiveDuration.Record(ctx, duration.Seconds(), attrs)
}

// RecordChannelSend records metrics for sending a message to the output channel.
// This tracks backpressure - longer durations indicate downstream processing is slow.
func (t *telemetry) RecordChannelSend(ctx context.Context, subscription, pipeline string, duration time.Duration, depth, capacity int) {
	attrs := metric.WithAttributes(
		attribute.String("subscription", subscription),
		attribute.String("pipeline", pipeline),
		attribute.String("stage", "receive"),
	)
	t.channelSendDuration.Record(ctx, duration.Seconds(), attrs)
	t.channelDepth.Record(ctx, int64(depth), attrs)
	t.channelCapacity.Record(ctx, int64(capacity), attrs)

	utilization := 0.0
	if capacity > 0 {
		utilization = float64(depth) / float64(capacity)
	}
	t.channelUtilization.Record(ctx, utilization, attrs)
}

// RecordAck records metrics for acknowledged messages.
func (t *telemetry) RecordAck(ctx context.Context, subscription, pipeline, msgType string) {
	t.ackedTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("subscription", subscription),
		attribute.String("pipeline", pipeline),
		attribute.String("stage", "receive"),
		attribute.String("type", msgType),
	))
}

// RecordNack records metrics for rejected messages.
func (t *telemetry) RecordNack(ctx context.Context, subscription, pipeline, msgType string) {
	t.nackedTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("subscription", subscription),
		attribute.String("pipeline", pipeline),
		attribute.String("stage", "receive"),
		attribute.String("type", msgType),
	))
}

// RecordProcessingDuration records the time from message receive to settlement.
func (t *telemetry) RecordProcessingDuration(ctx context.Context, subscription, pipeline, msgType string, duration time.Duration) {
	t.processingDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
		attribute.String("subscription", subscription),
		attribute.String("pipeline", pipeline),
		attribute.String("stage", "receive"),
		attribute.String("type", msgType),
	))
}

// RecordLockRenewal records a successful message lock renewal.
func (t *telemetry) RecordLockRenewal(ctx context.Context, subscription, pipeline, msgType string) {
	t.lockRenewalsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("subscription", subscription),
		attribute.String("pipeline", pipeline),
		attribute.String("stage", "receive"),
		attribute.String("type", msgType),
	))
}

// RecordLockLost records when a message lock was lost before settlement.
func (t *telemetry) RecordLockLost(ctx context.Context, subscription, pipeline, msgType string) {
	t.lockLostTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("subscription", subscription),
		attribute.String("pipeline", pipeline),
		attribute.String("stage", "receive"),
		attribute.String("type", msgType),
	))
}
