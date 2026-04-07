package semaphore

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	telOnce     sync.Once
	telInstance *telemetry
)

type telemetry struct {
	acquireWait metric.Float64Histogram
	current     metric.Int64Gauge
	utilization metric.Float64Gauge
}

func getTelemetry() *telemetry {
	telOnce.Do(func() {
		meter := otel.Meter("semaphore")

		acquireWait, _ := meter.Float64Histogram("semaphore_acquire_seconds",
			metric.WithUnit("s"),
			metric.WithDescription("Time spent waiting to acquire semaphore"),
			metric.WithExplicitBucketBoundaries(0, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10))

		current, _ := meter.Int64Gauge("semaphore_current",
			metric.WithDescription("Current number of acquired semaphore slots"))

		utilization, _ := meter.Float64Gauge("semaphore_utilization_ratio",
			metric.WithDescription("Semaphore utilization ratio (current/capacity)"))

		telInstance = &telemetry{
			acquireWait: acquireWait,
			current:     current,
			utilization: utilization,
		}
	})
	return telInstance
}

func (t *telemetry) recordAcquire(ctx context.Context, name string, labels map[string]string, current, capacity int64, waitDuration time.Duration) {
	attrList := []attribute.KeyValue{attribute.String("name", name)}
	for k, v := range labels {
		attrList = append(attrList, attribute.String(k, v))
	}
	attrs := metric.WithAttributes(attrList...)

	t.acquireWait.Record(ctx, waitDuration.Seconds(), attrs)
	t.current.Record(ctx, current, attrs)
	t.utilization.Record(ctx, float64(current)/float64(capacity), attrs)
}

func (t *telemetry) recordRelease(ctx context.Context, name string, labels map[string]string, current, capacity int64) {
	attrList := []attribute.KeyValue{attribute.String("name", name)}
	for k, v := range labels {
		attrList = append(attrList, attribute.String(k, v))
	}
	attrs := metric.WithAttributes(attrList...)

	t.current.Record(ctx, current, attrs)
	t.utilization.Record(ctx, float64(current)/float64(capacity), attrs)
}
