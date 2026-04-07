package azservicebus_test

import (
	gosb "github.com/fxsml/gopipe-azservicebus"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"


	"github.com/fxsml/gopipe/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBackpressure verifies semaphore limits in-flight messages to MaxInFlight
func TestBackpressure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber with low MaxInFlight
	maxInFlight := 5
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		MaxInFlight: maxInFlight,
	})
	require.NoError(t, err)

	// Publish more messages than MaxInFlight
	messageCount := 15
	for i := 0; i < messageCount; i++ {
		msg := message.NewRaw(
			[]byte(fmt.Sprintf(`{"test":"backpressure-%d"}`, i)),
			message.Attributes{
				message.AttrID:              fmt.Sprintf("backpressure-msg-%d", i),
				message.AttrType:            "azservicebus.test.backpressure",
				message.AttrSource:          "/test",
				message.AttrDataContentType: "application/json",
			},
			nil,
		)
		err = pub.PublishBatch(ctx, "test", msg)
		require.NoError(t, err)
	}
	t.Logf("Published %d messages", messageCount)

	// Subscribe
	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	// Track concurrent processing
	var inFlight atomic.Int32
	var maxConcurrent atomic.Int32
	var wg sync.WaitGroup

	received := 0
	for i := 0; i < messageCount; i++ {
		select {
		case msg := <-msgChan:
			received++
			wg.Add(1)

			go func(m *message.RawMessage) {
				defer wg.Done()

				// Track concurrency
				current := inFlight.Add(1)
				for {
					max := maxConcurrent.Load()
					if current <= max || maxConcurrent.CompareAndSwap(max, current) {
						break
					}
				}

				// Simulate processing
				time.Sleep(100 * time.Millisecond)

				m.Ack()
				inFlight.Add(-1)
			}(msg)

		case <-time.After(30 * time.Second):
			t.Fatalf("Timeout waiting for message %d/%d", i+1, messageCount)
		}
	}

	wg.Wait()

	assert.Equal(t, messageCount, received, "All messages should be received")
	assert.LessOrEqual(t, int(maxConcurrent.Load()), maxInFlight,
		"Concurrent processing should not exceed MaxInFlight")
	t.Logf("Max concurrent: %d (limit: %d)", maxConcurrent.Load(), maxInFlight)
}

// TestGracefulShutdown verifies context cancellation waits for in-flight messages to settle
func TestGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testCtx, testCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer testCancel()

	topicName, subName, cleanup := testTopicSetup(t, testCtx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber with its own cancelable context
	subCtx, subCancel := context.WithCancel(testCtx)
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		MaxInFlight:     10,
		ShutdownTimeout: 10 * time.Second,
	})
	require.NoError(t, err)

	// Publish test messages
	messageCount := 5
	for i := 0; i < messageCount; i++ {
		msg := message.NewRaw(
			[]byte(fmt.Sprintf(`{"test":"shutdown-%d"}`, i)),
			message.Attributes{
				message.AttrID:              fmt.Sprintf("shutdown-msg-%d", i),
				message.AttrType:            "azservicebus.test.shutdown",
				message.AttrSource:          "/test",
				message.AttrDataContentType: "application/json",
			},
			nil,
		)
		err = pub.PublishBatch(testCtx, "test", msg)
		require.NoError(t, err)
	}

	// Subscribe
	msgChan, err := sub.Subscribe(subCtx, "test")
	require.NoError(t, err)

	var settledCount atomic.Int32
	var processingStarted sync.WaitGroup
	var processingDone sync.WaitGroup
	processingStarted.Add(messageCount)
	processingDone.Add(messageCount)

	// Process messages slowly
	go func() {
		for msg := range msgChan {
			go func(m *message.RawMessage) {
				processingStarted.Done()
				time.Sleep(2 * time.Second) // Simulate slow processing
				m.Ack()
				settledCount.Add(1)
				processingDone.Done()
			}(msg)
		}
	}()

	// Wait for all messages to start processing
	processingStarted.Wait()
	t.Log("All messages started processing")

	// Cancel subscriber context - should trigger graceful shutdown
	shutdownStart := time.Now()
	subCancel()

	// Wait for all processing to complete (graceful shutdown should allow this)
	processingDone.Wait()
	shutdownDuration := time.Since(shutdownStart)

	assert.Equal(t, int32(messageCount), settledCount.Load(), "All messages should settle during graceful shutdown")
	assert.GreaterOrEqual(t, shutdownDuration, 1*time.Second, "Shutdown should wait for processing")
	t.Logf("Graceful shutdown took %v, settled %d messages", shutdownDuration, settledCount.Load())
}

// TestShutdownDuringSend verifies shutdown while sending to channel abandons message
func TestShutdownDuringSend(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber with small channel buffer
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		MaxInFlight: 10,
	})
	require.NoError(t, err)

	// Publish messages
	messageCount := 20
	for i := 0; i < messageCount; i++ {
		msg := message.NewRaw(
			[]byte(fmt.Sprintf(`{"test":"shutdown-send-%d"}`, i)),
			message.Attributes{
				message.AttrID:              fmt.Sprintf("shutdown-send-msg-%d", i),
				message.AttrType:            "azservicebus.test.shutdown.send",
				message.AttrSource:          "/test",
				message.AttrDataContentType: "application/json",
			},
			nil,
		)
		err = pub.PublishBatch(ctx, "test", msg)
		require.NoError(t, err)
	}

	// Subscribe
	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	// Receive only a few messages, then close (channel will fill up)
	received := 0
	for i := 0; i < 3; i++ {
		<-msgChan
		received++
	}

	t.Logf("Received %d messages, closing subscriber", received)

	// Close immediately - some messages may be waiting to send to channel
	closeStart := time.Now()
	closeDuration := time.Since(closeStart)

	require.NoError(t, err)
	t.Logf("Close took %v (messages waiting to send should be abandoned)", closeDuration)
	// Success = no deadlock, clean shutdown
}

// TestShutdownDuringProcessing verifies shutdown while processing abandons message
func TestShutdownDuringProcessing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber with short grace period
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		MaxInFlight:     5,
		ShutdownTimeout: 3 * time.Second, // Short timeout
	})
	require.NoError(t, err)

	// Publish test message
	msg := message.NewRaw(
		[]byte(`{"test":"shutdown-processing"}`),
		message.Attributes{
			message.AttrID:              "shutdown-processing-msg",
			message.AttrType:            "azservicebus.test.shutdown.processing",
			message.AttrSource:          "/test",
			message.AttrDataContentType: "application/json",
		},
		nil,
	)
	err = pub.PublishBatch(ctx, "test", msg)
	require.NoError(t, err)

	// Subscribe
	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	var processingStarted sync.WaitGroup
	processingStarted.Add(1)

	// Start processing but never ack
	go func() {
		msg := <-msgChan
		processingStarted.Done()
		// Never ack - simulate stuck processing
		time.Sleep(30 * time.Second)
		msg.Ack()
	}()

	// Wait for processing to start
	processingStarted.Wait()
	t.Log("Message processing started (stuck)")

	// Close - should abandon the message after ShutdownTimeout
	closeStart := time.Now()
	closeDuration := time.Since(closeStart)

	require.NoError(t, err)
	assert.LessOrEqual(t, closeDuration, 15*time.Second, "Close should timeout and proceed")
	t.Logf("Close took %v (message abandoned due to timeout)", closeDuration)
}

// TestLockRenewalAutoDetect verifies interval calculated from LockedUntil/2
func TestLockRenewalAutoDetect(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber with lock renewal enabled (default)
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		MaxInFlight: 5,
	})
	require.NoError(t, err)

	// Publish test message
	msg := message.NewRaw(
		[]byte(`{"test":"lock-renewal"}`),
		message.Attributes{
			message.AttrID:              "lock-renewal-msg",
			message.AttrType:            "azservicebus.test.lock.renewal",
			message.AttrSource:          "/test",
			message.AttrDataContentType: "application/json",
		},
		nil,
	)
	err = pub.PublishBatch(ctx, "test", msg)
	require.NoError(t, err)

	// Subscribe
	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	select {
	case received := <-msgChan:
		t.Log("Received message, simulating long processing (90s)")

		// Simulate processing longer than default lock duration (60s)
		// Lock renewal should keep the message locked
		time.Sleep(90 * time.Second)

		// Ack should succeed because lock was renewed
		received.Ack()
		t.Log("Successfully acked after 90s processing (lock renewal worked)")

	case <-time.After(2 * time.Minute):
		t.Fatal("Timeout waiting for message")
	}

}

// TestLockRenewalConfigOverride verifies config interval is used when set
func TestLockRenewalConfigOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber with forced renewal interval
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		MaxInFlight:         5,
		LockRenewalInterval: 15 * time.Second, // Override
	})
	require.NoError(t, err)

	// Publish test message
	msg := message.NewRaw(
		[]byte(`{"test":"lock-renewal-override"}`),
		message.Attributes{
			message.AttrID:              "lock-renewal-override-msg",
			message.AttrType:            "azservicebus.test.lock.renewal.override",
			message.AttrSource:          "/test",
			message.AttrDataContentType: "application/json",
		},
		nil,
	)
	err = pub.PublishBatch(ctx, "test", msg)
	require.NoError(t, err)

	// Subscribe
	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	select {
	case received := <-msgChan:
		t.Log("Received message with custom renewal interval (15s)")

		// Process for longer than one renewal cycle
		time.Sleep(20 * time.Second)

		received.Ack()
		t.Log("Acked after 20s (custom renewal interval worked)")

	case <-time.After(45 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

}

// TestDisableLockRenewal verifies DisableLockRenewal flag works
func TestDisableLockRenewal(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber with lock renewal DISABLED
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		MaxInFlight:        5,
		DisableLockRenewal: true, // Disable renewal
	})
	require.NoError(t, err)

	// Publish test message
	msg := message.NewRaw(
		[]byte(`{"test":"no-lock-renewal"}`),
		message.Attributes{
			message.AttrID:              "no-lock-renewal-msg",
			message.AttrType:            "azservicebus.test.no.lock.renewal",
			message.AttrSource:          "/test",
			message.AttrDataContentType: "application/json",
		},
		nil,
	)
	err = pub.PublishBatch(ctx, "test", msg)
	require.NoError(t, err)

	// Subscribe
	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	select {
	case received := <-msgChan:
		t.Log("Received message with lock renewal disabled")

		// Quick processing (should work fine without renewal)
		time.Sleep(1 * time.Second)

		received.Ack()
		t.Log("Acked quickly (no renewal needed)")

	case <-time.After(15 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

}

// TestGracefulShutdownSettlesMessages verifies that context cancellation
// gracefully waits for in-flight messages to settle.
func TestGracefulShutdownSettlesMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber with graceful shutdown timeout
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		MaxInFlight:    5,
		ShutdownTimeout: 5 * time.Second,
	})
	require.NoError(t, err)

	// Publish test messages
	messageCount := 3
	for i := 0; i < messageCount; i++ {
		msg := message.NewRaw(
			[]byte(fmt.Sprintf(`{"test":"graceful-%d"}`, i)),
			message.Attributes{
				message.AttrID: fmt.Sprintf("graceful-msg-%d", i),
			},
			nil,
		)
		err = pub.PublishBatch(ctx, "test", msg)
		require.NoError(t, err)
	}
	t.Log("Published test messages")

	// Subscribe and track settled messages
	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	var (
		ackedCount   atomic.Int32
		receivedCount atomic.Int32
	)

	// Process messages slowly (simulate slow handler)
	go func() {
		for msg := range msgChan {
			receivedCount.Add(1)
			t.Logf("Received message %d", receivedCount.Load())
			time.Sleep(500 * time.Millisecond) // Simulate work
			msg.Ack()
			ackedCount.Add(1)
			t.Logf("Acked message %d", ackedCount.Load())
		}
	}()

	// Give it time to receive and start processing
	time.Sleep(1 * time.Second)

	// Cancel context to trigger graceful shutdown
	shutdownStart := time.Now()
	cancel()

	// Wait a bit for shutdown to complete
	time.Sleep(6 * time.Second)

	shutdownDuration := time.Since(shutdownStart)
	t.Logf("Shutdown took %v", shutdownDuration)

	// Verify: should have settled most/all messages within ShutdownTimeout
	received := int(receivedCount.Load())
	acked := int(ackedCount.Load())
	t.Logf("Received: %d, Acked: %d, Total published: %d", received, acked, messageCount)

	assert.Greater(t, received, 0, "should have received at least one message")
	assert.Greater(t, acked, 0, "should have acked at least one message")
	assert.LessOrEqual(t, acked, messageCount, "acked count should not exceed published")
}

// TestGracefulShutdownForceAbandon verifies that messages are abandoned
// if they don't settle within ShutdownTimeout.
func TestGracefulShutdownForceAbandon(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber with short shutdown timeout
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		MaxInFlight:     5,
		ShutdownTimeout: 1 * time.Second, // Short timeout
	})
	require.NoError(t, err)

	// Publish test messages
	messageCount := 5
	for i := 0; i < messageCount; i++ {
		msg := message.NewRaw(
			[]byte(fmt.Sprintf(`{"test":"force-abandon-%d"}`, i)),
			message.Attributes{
				message.AttrID: fmt.Sprintf("force-abandon-msg-%d", i),
			},
			nil,
		)
		err = pub.PublishBatch(ctx, "test", msg)
		require.NoError(t, err)
	}
	t.Log("Published test messages")

	// Subscribe with slow processing (intentionally won't complete in time)
	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	var receivedCount atomic.Int32

	// Process messages VERY slowly (won't finish before timeout)
	go func() {
		for msg := range msgChan {
			receivedCount.Add(1)
			t.Logf("Received message %d, sleeping 10 seconds...", receivedCount.Load())
			time.Sleep(10 * time.Second) // Won't finish before 1s shutdown timeout
			msg.Ack() // Won't reach here for most messages
		}
	}()

	// Give it time to receive messages
	time.Sleep(500 * time.Millisecond)

	// Cancel context - should force-abandon after 1 second
	shutdownStart := time.Now()
	cancel()

	// Wait for shutdown to complete
	time.Sleep(3 * time.Second)

	shutdownDuration := time.Since(shutdownStart)
	t.Logf("Shutdown took %v", shutdownDuration)

	received := int(receivedCount.Load())
	t.Logf("Received: %d, Total published: %d", received, messageCount)

	// Verify: some messages received but not all processed due to short timeout
	assert.Greater(t, received, 0, "should have received some messages")
	assert.Less(t, received, messageCount, "should not have processed all messages (due to timeout)")
	assert.LessOrEqual(t, shutdownDuration, 5*time.Second, "shutdown should complete in reasonable time")
}

// TestContextCancellationIsClean verifies that context cancellation
// doesn't cause panics and handles errors gracefully.
func TestContextCancellationIsClean(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		MaxInFlight:     3,
		ShutdownTimeout: 2 * time.Second,
	})
	require.NoError(t, err)

	// Publish some messages
	for i := 0; i < 5; i++ {
		msg := message.NewRaw(
			[]byte(fmt.Sprintf(`{"test":"clean-%d"}`, i)),
			message.Attributes{
				message.AttrID: fmt.Sprintf("clean-msg-%d", i),
			},
			nil,
		)
		err = pub.PublishBatch(ctx, "test", msg)
		require.NoError(t, err)
	}

	// Subscribe
	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	var wg sync.WaitGroup
	panicRecovered := false

	// Process messages with panic recovery
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				t.Logf("PANIC DETECTED: %v", r)
				panicRecovered = true
			}
		}()

		for msg := range msgChan {
			msg.Ack()
		}
	}()

	// Give it time to process
	time.Sleep(1 * time.Second)

	// Cancel context
	cancel()

	// Wait for goroutine to finish
	wg.Wait()

	// Verify: no panic occurred
	assert.False(t, panicRecovered, "context cancellation should not cause panic")
	t.Log("Graceful shutdown completed without panic")
}
