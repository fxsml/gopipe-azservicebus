// Package semaphore provides a weighted semaphore with optional OpenTelemetry metrics.
package semaphore

import (
	"context"
	"sync/atomic"
	"time"

	xsync "golang.org/x/sync/semaphore"
)

// Semaphore is a weighted semaphore that tracks current utilization
// and optionally records metrics via OpenTelemetry.
type Semaphore struct {
	sem      *xsync.Weighted
	capacity int64
	current  atomic.Int64
	name     string
	labels   map[string]string
	tel      *telemetry // nil if metrics disabled
}

// Config configures a Semaphore.
type Config struct {
	// Name identifies this semaphore in metrics. Required if Metrics is true.
	Name string
	// Metrics enables OpenTelemetry metrics recording.
	Metrics bool
	// Labels are additional metric labels for customization (e.g., pipeline, subscription).
	Labels map[string]string
}

// New creates a new Semaphore with the given capacity.
// If cfg.Metrics is true, OpenTelemetry metrics are recorded for this semaphore.
func New(capacity int64, cfg Config) *Semaphore {
	s := &Semaphore{
		sem:      xsync.NewWeighted(capacity),
		capacity: capacity,
		name:     cfg.Name,
		labels:   cfg.Labels,
	}
	if cfg.Metrics && cfg.Name != "" {
		s.tel = getTelemetry()
	}
	return s
}

// Acquire acquires n resources, blocking until available or ctx is done.
// Returns ctx.Err() if the context is cancelled before acquisition.
// API compatible with golang.org/x/sync/semaphore.
func (s *Semaphore) Acquire(ctx context.Context, n int64) error {
	start := time.Now()

	if err := s.sem.Acquire(ctx, n); err != nil {
		return err
	}

	s.current.Add(n)

	if s.tel != nil {
		waitDuration := time.Since(start)
		s.tel.recordAcquire(ctx, s.name, s.labels, s.current.Load(), s.capacity, waitDuration)
	}

	return nil
}

// AcquireOne acquires a single resource. Convenience wrapper for Acquire(ctx, 1).
func (s *Semaphore) AcquireOne(ctx context.Context) error {
	return s.Acquire(ctx, 1)
}

// TryAcquire attempts to acquire n resources without blocking.
// Returns true if successful, false if not enough resources available.
// API compatible with golang.org/x/sync/semaphore.
func (s *Semaphore) TryAcquire(n int64) bool {
	if s.sem.TryAcquire(n) {
		s.current.Add(n)
		if s.tel != nil {
			s.tel.recordAcquire(context.Background(), s.name, s.labels, s.current.Load(), s.capacity, 0)
		}
		return true
	}
	return false
}

// TryAcquireOne attempts to acquire a single resource without blocking.
// Convenience wrapper for TryAcquire(1).
func (s *Semaphore) TryAcquireOne() bool {
	return s.TryAcquire(1)
}

// Release releases n resources back to the semaphore.
// API compatible with golang.org/x/sync/semaphore.
func (s *Semaphore) Release(n int64) {
	s.current.Add(-n)
	s.sem.Release(n)

	if s.tel != nil {
		s.tel.recordRelease(context.Background(), s.name, s.labels, s.current.Load(), s.capacity)
	}
}

// ReleaseOne releases a single resource. Convenience wrapper for Release(1).
func (s *Semaphore) ReleaseOne() {
	s.Release(1)
}

// Current returns the current number of acquired resources.
func (s *Semaphore) Current() int64 {
	return s.current.Load()
}

// Capacity returns the maximum capacity of the semaphore.
func (s *Semaphore) Capacity() int64 {
	return s.capacity
}

// Available returns the number of resources currently available.
func (s *Semaphore) Available() int64 {
	return s.capacity - s.current.Load()
}
