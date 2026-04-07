package semaphore

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSemaphore_AcquireRelease(t *testing.T) {
	sem := New(3, Config{Name: "test"})

	if sem.Capacity() != 3 {
		t.Errorf("expected capacity 3, got %d", sem.Capacity())
	}
	if sem.Current() != 0 {
		t.Errorf("expected current 0, got %d", sem.Current())
	}
	if sem.Available() != 3 {
		t.Errorf("expected available 3, got %d", sem.Available())
	}

	// Acquire one
	if err := sem.Acquire(context.Background(), 1); err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if sem.Current() != 1 {
		t.Errorf("expected current 1, got %d", sem.Current())
	}
	if sem.Available() != 2 {
		t.Errorf("expected available 2, got %d", sem.Available())
	}

	// Acquire two more
	if err := sem.Acquire(context.Background(), 2); err != nil {
		t.Fatalf("AcquireN failed: %v", err)
	}
	if sem.Current() != 3 {
		t.Errorf("expected current 3, got %d", sem.Current())
	}
	if sem.Available() != 0 {
		t.Errorf("expected available 0, got %d", sem.Available())
	}

	// Release one
	sem.Release(1)
	if sem.Current() != 2 {
		t.Errorf("expected current 2, got %d", sem.Current())
	}

	// Release two
	sem.Release(2)
	if sem.Current() != 0 {
		t.Errorf("expected current 0, got %d", sem.Current())
	}
}

func TestSemaphore_TryAcquire(t *testing.T) {
	sem := New(2, Config{Name: "test"})

	// Should succeed
	if !sem.TryAcquire(1) {
		t.Error("TryAcquire should succeed when available")
	}
	if !sem.TryAcquire(1) {
		t.Error("TryAcquire should succeed when available")
	}

	// Should fail - semaphore full
	if sem.TryAcquire(1) {
		t.Error("TryAcquire should fail when full")
	}

	if sem.Current() != 2 {
		t.Errorf("expected current 2, got %d", sem.Current())
	}

	// Release and try again
	sem.Release(1)
	if !sem.TryAcquire(1) {
		t.Error("TryAcquire should succeed after release")
	}

	sem.Release(2)
}

func TestSemaphore_TryAcquireN(t *testing.T) {
	sem := New(5, Config{Name: "test"})

	// Acquire 3
	if !sem.TryAcquire(3) {
		t.Error("TryAcquireN(3) should succeed")
	}
	if sem.Current() != 3 {
		t.Errorf("expected current 3, got %d", sem.Current())
	}

	// Try to acquire 3 more - should fail
	if sem.TryAcquire(3) {
		t.Error("TryAcquireN(3) should fail when only 2 available")
	}

	// Acquire remaining 2
	if !sem.TryAcquire(2) {
		t.Error("TryAcquireN(2) should succeed")
	}
	if sem.Current() != 5 {
		t.Errorf("expected current 5, got %d", sem.Current())
	}

	sem.Release(5)
}

func TestSemaphore_AcquireBlocks(t *testing.T) {
	sem := New(1, Config{Name: "test"})

	// Acquire the only slot
	sem.Acquire(context.Background(), 1)

	// Start goroutine that tries to acquire
	acquired := make(chan struct{})
	go func() {
		sem.Acquire(context.Background(), 1)
		close(acquired)
	}()

	// Should not acquire yet
	select {
	case <-acquired:
		t.Error("should not acquire while semaphore is full")
	case <-time.After(50 * time.Millisecond):
		// Expected - still blocked
	}

	// Release
	sem.Release(1)

	// Now should acquire
	select {
	case <-acquired:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("should acquire after release")
	}

	sem.Release(1)
}

func TestSemaphore_AcquireContextCancel(t *testing.T) {
	sem := New(1, Config{Name: "test"})

	// Fill the semaphore
	sem.Acquire(context.Background(), 1)

	// Try to acquire with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sem.Acquire(ctx, 1)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	// Current should still be 1 (failed acquire doesn't increment)
	if sem.Current() != 1 {
		t.Errorf("expected current 1, got %d", sem.Current())
	}

	sem.Release(1)
}

func TestSemaphore_AcquireContextTimeout(t *testing.T) {
	sem := New(1, Config{Name: "test"})

	// Fill the semaphore
	sem.Acquire(context.Background(), 1)

	// Try to acquire with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := sem.Acquire(ctx, 1)
	elapsed := time.Since(start)

	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}

	if elapsed < 10*time.Millisecond {
		t.Errorf("expected to wait at least 10ms, waited %v", elapsed)
	}

	sem.Release(1)
}

func TestSemaphore_ConcurrentAccess(t *testing.T) {
	sem := New(10, Config{Name: "test"})
	iterations := 1000
	goroutines := 50

	var wg sync.WaitGroup
	var totalAcquires atomic.Int64

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if err := sem.Acquire(context.Background(), 1); err != nil {
					t.Errorf("Acquire failed: %v", err)
					return
				}
				totalAcquires.Add(1)

				// Verify current never exceeds capacity
				current := sem.Current()
				if current > sem.Capacity() {
					t.Errorf("current %d exceeds capacity %d", current, sem.Capacity())
				}

				sem.Release(1)
			}
		}()
	}

	wg.Wait()

	expected := int64(goroutines * iterations)
	if totalAcquires.Load() != expected {
		t.Errorf("expected %d total acquires, got %d", expected, totalAcquires.Load())
	}

	if sem.Current() != 0 {
		t.Errorf("expected current 0 after all releases, got %d", sem.Current())
	}
}

func TestSemaphore_WithMetrics(t *testing.T) {
	// Test that metrics-enabled semaphore works correctly
	sem := New(5, Config{
		Name:    "test-metrics",
		Metrics: true,
	})

	if err := sem.Acquire(context.Background(), 1); err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if sem.Current() != 1 {
		t.Errorf("expected current 1, got %d", sem.Current())
	}

	sem.Release(1)
	if sem.Current() != 0 {
		t.Errorf("expected current 0, got %d", sem.Current())
	}
}

func TestSemaphore_WithoutMetrics(t *testing.T) {
	// Test that non-metrics semaphore works (no panic from nil telemetry)
	sem := New(5, Config{
		Name:    "test-no-metrics",
		Metrics: false,
	})

	if err := sem.Acquire(context.Background(), 1); err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	sem.Release(1)

	if sem.TryAcquire(1) {
		sem.Release(1)
	}
}

func TestSemaphore_EmptyName(t *testing.T) {
	// Metrics should be disabled if name is empty even if Metrics: true
	sem := New(5, Config{
		Name:    "",
		Metrics: true,
	})

	// Should work without panicking
	sem.Acquire(context.Background(), 1)
	sem.Release(1)

	// tel should be nil
	if sem.tel != nil {
		t.Error("telemetry should be nil when name is empty")
	}
}

func TestSemaphore_ConcurrentAccessWithMetrics(t *testing.T) {
	// Test that metrics recording is safe under high concurrency
	sem := New(10, Config{
		Name:    "concurrent-metrics-test",
		Metrics: true,
	})
	iterations := 1000
	goroutines := 50

	var wg sync.WaitGroup
	var totalAcquires atomic.Int64

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				if err := sem.Acquire(context.Background(), 1); err != nil {
					t.Errorf("Acquire failed: %v", err)
					return
				}
				totalAcquires.Add(1)

				// Verify current never exceeds capacity
				current := sem.Current()
				if current > sem.Capacity() {
					t.Errorf("current %d exceeds capacity %d", current, sem.Capacity())
				}

				sem.Release(1)
			}
		}()
	}

	wg.Wait()

	expected := int64(goroutines * iterations)
	if totalAcquires.Load() != expected {
		t.Errorf("expected %d total acquires, got %d", expected, totalAcquires.Load())
	}

	if sem.Current() != 0 {
		t.Errorf("expected current 0 after all releases, got %d", sem.Current())
	}
}

func TestSemaphore_ConcurrentMixedOperationsWithMetrics(t *testing.T) {
	// Test mixed Acquire/TryAcquire under contention with metrics
	sem := New(5, Config{
		Name:    "mixed-ops-test",
		Metrics: true,
	})
	iterations := 500
	goroutines := 20

	var wg sync.WaitGroup
	var acquires, tryAcquires, tryFails atomic.Int64

	// Half goroutines use Acquire (blocking)
	for range goroutines / 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				if err := sem.Acquire(context.Background(), 1); err != nil {
					return
				}
				acquires.Add(1)
				sem.Release(1)
			}
		}()
	}

	// Half goroutines use TryAcquire (non-blocking)
	for range goroutines / 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				if sem.TryAcquire(1) {
					tryAcquires.Add(1)
					sem.Release(1)
				} else {
					tryFails.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	t.Logf("Acquires: %d, TryAcquires: %d, TryFails: %d",
		acquires.Load(), tryAcquires.Load(), tryFails.Load())

	// All blocking acquires should succeed
	expectedAcquires := int64(goroutines / 2 * iterations)
	if acquires.Load() != expectedAcquires {
		t.Errorf("expected %d acquires, got %d", expectedAcquires, acquires.Load())
	}

	// TryAcquires + TryFails should equal total attempts
	expectedTryAttempts := int64(goroutines / 2 * iterations)
	if tryAcquires.Load()+tryFails.Load() != expectedTryAttempts {
		t.Errorf("expected %d try attempts, got %d", expectedTryAttempts, tryAcquires.Load()+tryFails.Load())
	}

	if sem.Current() != 0 {
		t.Errorf("expected current 0, got %d", sem.Current())
	}
}
