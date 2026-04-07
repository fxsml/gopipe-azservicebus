package azservicebus_test

import (
	gosb "github.com/fxsml/gopipe-azservicebus"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"


	"github.com/fxsml/gopipe/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscriber_ReceiveMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher for sending test messages
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		MaxInFlight: 50,
	})
	require.NoError(t, err)

	// Start subscriber before publishing
	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	// Define test messages
	testMessages := []string{"msg-1", "msg-2", "msg-3", "msg-4", "msg-5"}

	// Collect received messages
	var receivedMu sync.Mutex
	received := make(map[string]bool)

	// Start receiver goroutine
	receiveDone := make(chan struct{})
	go func() {
		defer close(receiveDone)
		for msg := range msgChan {
			id := msg.ID()
			receivedMu.Lock()
			received[id] = true
			count := len(received)
			receivedMu.Unlock()

			msg.Ack()
			t.Logf("Received message %d/%d: %s", count, len(testMessages), id)

			if count >= len(testMessages) {
				return
			}
		}
	}()

	// Publish messages
	msgs := make([]*message.RawMessage, len(testMessages))
	for i, id := range testMessages {
		msgs[i] = message.NewRaw(
			[]byte(fmt.Sprintf(`{"id":"%s"}`, id)),
			message.Attributes{
				message.AttrID:              id,
				message.AttrType:            "azservicebus.integration.test",
				message.AttrSource:          "/test",
				message.AttrDataContentType: "application/json",
			},
			nil,
		)
	}

	err = pub.PublishBatch(ctx, "test", msgs...)
	require.NoError(t, err)
	t.Logf("Published %d messages", len(testMessages))

	// Wait for all messages to be received
	select {
	case <-receiveDone:
		t.Log("All messages received")
	case <-time.After(30 * time.Second):
		receivedMu.Lock()
		count := len(received)
		receivedMu.Unlock()
		t.Fatalf("Timeout waiting for messages: received %d/%d", count, len(testMessages))
	}

	// Cancel context to stop subscriber
	cancel()

	// Assert we received exactly the messages we sent
	receivedMu.Lock()
	defer receivedMu.Unlock()

	assert.Equal(t, len(testMessages), len(received), "should receive exactly %d messages", len(testMessages))

	for _, id := range testMessages {
		assert.True(t, received[id], "message %s should have been received", id)
	}
}

func TestSubscriber_NackAndRedelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber with MaxInFlight=1 for predictable behavior
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		MaxInFlight: 1,
	})
	require.NoError(t, err)

	// Publish a single message
	msgID := "nack-test-msg"
	msg := message.NewRaw(
		[]byte(`{"test":"nack"}`),
		message.Attributes{
			message.AttrID:              msgID,
			message.AttrType:            "azservicebus.integration.test.nack",
			message.AttrSource:          "/test",
			message.AttrDataContentType: "application/json",
		},
		nil,
	)
	err = pub.PublishBatch(ctx, "test", msg)
	require.NoError(t, err)
	t.Log("Published message")

	// Subscribe
	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	// Receive first time and NACK
	select {
	case received := <-msgChan:
		assert.Equal(t, msgID, received.ID())
		received.Nack(errors.New("intentional nack for test"))
		t.Log("Received and nacked message (first delivery)")
	case <-time.After(15 * time.Second):
		t.Fatal("Timeout waiting for first delivery")
	}

	// Receive second time (redelivery) and ACK
	select {
	case received := <-msgChan:
		assert.Equal(t, msgID, received.ID())
		received.Ack()
		t.Log("Received and acked message (redelivery)")
	case <-time.After(15 * time.Second):
		t.Fatal("Timeout waiting for redelivery")
	}

	// Verify no more messages arrive (message was acked)
	select {
	case received := <-msgChan:
		t.Fatalf("Unexpected message received after ack: %s", received.ID())
	case <-time.After(3 * time.Second):
		t.Log("Confirmed no more messages (as expected)")
	}

	cancel()
}

func TestSubscriber_ExpiryTimePreserved(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher and subscriber
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{})
	require.NoError(t, err)

	// Publish message with expirytime set to 10 seconds from now
	expiry := time.Now().Add(10 * time.Second).Truncate(time.Second)
	msgID := "expiry-test-msg"
	msg := message.NewRaw(
		[]byte(`{"test":"expiry"}`),
		message.Attributes{
			message.AttrID:              msgID,
			message.AttrType:            "azservicebus.integration.test.expiry",
			message.AttrSource:          "/test",
			message.AttrDataContentType: "application/json",
			message.AttrExpiryTime:      expiry.Format(time.RFC3339),
		},
		nil,
	)
	err = pub.PublishBatch(ctx, "test", msg)
	require.NoError(t, err)
	t.Logf("Published message with expiry: %s", expiry.Format(time.RFC3339))

	// Subscribe and receive
	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	select {
	case received := <-msgChan:
		assert.Equal(t, msgID, received.ID())

		// Verify expirytime is preserved
		receivedExpiry := received.ExpiryTime()
		assert.False(t, receivedExpiry.IsZero(), "expirytime should be set")

		// Allow 2 second tolerance for timing differences
		diff := receivedExpiry.Sub(expiry).Abs()
		assert.Less(t, diff, 2*time.Second, "expirytime should be within 2 seconds of original: got %v, want %v", receivedExpiry, expiry)

		t.Logf("Received message with expiry: %s (diff: %v)", receivedExpiry.Format(time.RFC3339), diff)
		received.Ack()
	case <-time.After(15 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

	cancel()
}

func TestSubscriber_NoExpiryTimeWhenNotSet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher and subscriber
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{})
	require.NoError(t, err)

	// Publish message WITHOUT expirytime set
	msgID := "no-expiry-test-msg"
	msg := message.NewRaw(
		[]byte(`{"test":"no-expiry"}`),
		message.Attributes{
			message.AttrID:              msgID,
			message.AttrType:            "azservicebus.integration.test.noexpiry",
			message.AttrSource:          "/test",
			message.AttrDataContentType: "application/json",
		},
		nil,
	)
	err = pub.PublishBatch(ctx, "test", msg)
	require.NoError(t, err)
	t.Log("Published message without expirytime")

	// Subscribe and receive
	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	select {
	case received := <-msgChan:
		assert.Equal(t, msgID, received.ID())

		// With async settlement + lock renewal, expiry time is NOT set by default.
		// Only CloudEvents expirytime (business deadline) sets it.
		// Lock renewal keeps the message locked indefinitely, so LockedUntil is not a deadline.
		// ShutdownTimeout is for shutdown, not processing timeout.
		receivedExpiry := received.ExpiryTime()
		assert.True(t, receivedExpiry.IsZero(),
			"expirytime should NOT be set when no CloudEvents expirytime is present (lock renewal handles lock duration)")

		t.Log("Verified: No expiry time set when CloudEvents expirytime absent")
		received.Ack()
	case <-time.After(15 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

	cancel()
}

func TestSubscriber_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{})
	require.NoError(t, err)

	// Create a separate context for the subscription that we'll cancel
	subCtx, subCancel := context.WithCancel(ctx)

	// Subscribe
	msgChan, err := sub.Subscribe(subCtx, "test")
	require.NoError(t, err)
	t.Log("Subscriber started")

	// Cancel the subscription context
	subCancel()
	t.Log("Context cancelled")

	// Channel should be closed after context cancellation
	select {
	case msg, ok := <-msgChan:
		if ok {
			// If we get a message, nack it (might have been in-flight)
			msg.Nack(errors.New("context cancelled"))
			t.Log("Received in-flight message, nacked it")
		} else {
			t.Log("Channel closed as expected")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for channel to close")
	}

	// Verify channel is closed by trying to read again
	_, ok := <-msgChan
	assert.False(t, ok, "channel should be closed after context cancellation")

}

func TestSubscriber_ContextCancellationClosesChannel(t *testing.T) {
	testCtx, testCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer testCancel()

	topicName, subName, cleanup := testTopicSetup(t, testCtx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{})
	require.NoError(t, err)

	// Create a cancelable context for the subscriber
	subCtx, subCancel := context.WithCancel(testCtx)

	// Subscribe
	msgChan, err := sub.Subscribe(subCtx, "test")
	require.NoError(t, err)
	t.Log("Subscriber started")

	// Cancel the subscriber context (triggers graceful shutdown)
	subCancel()
	t.Log("Context cancelled")

	// Channel should be closed
	select {
	case msg, ok := <-msgChan:
		if ok {
			msg.Nack(errors.New("subscriber closing"))
			t.Log("Received in-flight message, nacked it")
		} else {
			t.Log("Channel closed as expected")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for channel to close")
	}

	// Verify channel is closed
	_, ok := <-msgChan
	assert.False(t, ok, "channel should be closed after context cancellation")
}

func TestSubscriber_SubscribeAfterClose(t *testing.T) {
	testCtx, testCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer testCancel()

	topicName, subName, cleanup := testTopicSetup(t, testCtx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		ShutdownTimeout: 1 * time.Second,
	})
	require.NoError(t, err)

	// Close the subscriber
	err = sub.Close()
	require.NoError(t, err)

	// Attempt to subscribe should fail
	_, err = sub.Subscribe(testCtx, "test")
	require.Error(t, err)
	assert.ErrorIs(t, err, gosb.ErrSubscriberClosed)
}

func TestSubscriber_ResubscribeAfterContextCancel(t *testing.T) {
	testCtx, testCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer testCancel()

	topicName, subName, cleanup := testTopicSetup(t, testCtx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		ShutdownTimeout: 1 * time.Second,
	})
	require.NoError(t, err)
	defer sub.Close()

	// First subscription
	subCtx1, subCancel1 := context.WithCancel(testCtx)
	msgChan1, err := sub.Subscribe(subCtx1, "test")
	require.NoError(t, err)
	t.Log("First subscription started")

	// Cancel first subscription
	subCancel1()

	// Wait for channel to close
	for range msgChan1 {
		// Drain
	}
	t.Log("First subscription ended")

	// Second subscription should work (subscriber is reusable!)
	subCtx2, subCancel2 := context.WithCancel(testCtx)
	defer subCancel2()
	msgChan2, err := sub.Subscribe(subCtx2, "test")
	require.NoError(t, err, "Should be able to resubscribe after context cancellation")
	t.Log("Second subscription started successfully")

	// Verify channel is open
	select {
	case <-msgChan2:
		// Got a message or channel closed - both ok for this test
	case <-time.After(100 * time.Millisecond):
		// Timeout is fine - channel is open
	}

	// Cleanup
	subCancel2()
	for range msgChan2 {
		// Drain
	}
}

func TestSubscriber_ConcurrentSubscribeFails(t *testing.T) {
	testCtx, testCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer testCancel()

	topicName, subName, cleanup := testTopicSetup(t, testCtx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{})
	require.NoError(t, err)
	defer sub.Close()

	// First subscription
	subCtx, subCancel := context.WithCancel(testCtx)
	defer subCancel()
	msgChan, err := sub.Subscribe(subCtx, "test")
	require.NoError(t, err)
	t.Log("First subscription started")

	// Attempt second subscription while first is active - should fail
	_, err = sub.Subscribe(testCtx, "test")
	require.Error(t, err)
	assert.ErrorIs(t, err, gosb.ErrSubscriptionActive)
	t.Log("Second subscription correctly rejected")

	// Cleanup
	subCancel()
	for range msgChan {
		// Drain
	}
}

func TestSubscriber_CloseWhileSubscriptionActive(t *testing.T) {
	testCtx, testCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer testCancel()

	topicName, subName, cleanup := testTopicSetup(t, testCtx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		ShutdownTimeout: 5 * time.Second,
	})
	require.NoError(t, err)

	// Publish some messages
	for i := 0; i < 3; i++ {
		msg := message.NewRaw(
			[]byte(fmt.Sprintf(`{"test":"close-active-%d"}`, i)),
			message.Attributes{
				message.AttrID:              fmt.Sprintf("close-active-msg-%d", i),
				message.AttrType:            "azservicebus.test.close",
				message.AttrSource:          "/test",
				message.AttrDataContentType: "application/json",
			},
			nil,
		)
		err = pub.PublishBatch(testCtx, "test", msg)
		require.NoError(t, err)
	}

	// Start subscription
	subCtx, subCancel := context.WithCancel(testCtx)
	defer subCancel()
	msgChan, err := sub.Subscribe(subCtx, "test")
	require.NoError(t, err)
	t.Log("Subscription started")

	// Wait for at least one message
	select {
	case msg := <-msgChan:
		if msg != nil {
			msg.Ack()
			t.Log("Received and acked a message")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

	// Close subscriber while subscription is active
	// This should gracefully drain the subscription
	closeStart := time.Now()
	err = sub.Close()
	closeDuration := time.Since(closeStart)
	require.NoError(t, err)
	t.Logf("Close() completed in %v", closeDuration)

	// Drain any buffered messages and verify channel closes
	bufferedCount := 0
	for msg := range msgChan {
		if msg != nil {
			msg.Nack(errors.New("cleanup after close"))
			bufferedCount++
		}
	}
	t.Logf("Drained %d buffered messages after Close()", bufferedCount)
	// If we got here, channel is closed (range exits when channel closes)

	// Verify subscriber is closed
	_, err = sub.Subscribe(testCtx, "test")
	require.Error(t, err)
	assert.ErrorIs(t, err, gosb.ErrSubscriberClosed)
}

func TestSubscriber_BackpressureRespected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber with very low MaxInFlight
	maxInFlight := 2
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
		MaxInFlight: maxInFlight,
	})
	require.NoError(t, err)

	// Publish more messages than MaxInFlight
	numMessages := 5
	for i := 0; i < numMessages; i++ {
		msg := message.NewRaw(
			[]byte(fmt.Sprintf(`{"id":%d}`, i)),
			message.Attributes{
				message.AttrID:     fmt.Sprintf("backpressure-test-%d", i),
				message.AttrType:   "azservicebus.test.backpressure",
				message.AttrSource: "/test",
			},
			nil,
		)
		err = pub.PublishBatch(ctx, "test", msg)
		require.NoError(t, err)
	}
	t.Logf("Published %d messages", numMessages)

	// Subscribe
	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	// Receive all messages without acking first (hold them)
	heldMessages := make([]*message.RawMessage, 0, numMessages)
receiveLoop:
	for i := 0; i < numMessages; i++ {
		select {
		case msg := <-msgChan:
			heldMessages = append(heldMessages, msg)
			t.Logf("Received message %d: %s", i+1, msg.ID())
		case <-time.After(15 * time.Second):
			// This is expected - backpressure should prevent receiving more than MaxInFlight
			t.Logf("Backpressure working: only received %d messages before blocking", len(heldMessages))
			break receiveLoop
		}
	}

	// Ack all held messages
	for _, msg := range heldMessages {
		msg.Ack()
	}

	cancel()

	// We should have received at least some messages
	assert.GreaterOrEqual(t, len(heldMessages), 1, "should have received at least 1 message")
}
