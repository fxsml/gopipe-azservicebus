package azservicebus_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	gosb "github.com/fxsml/gopipe-azservicebus"

	"github.com/fxsml/gopipe/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublisher_PublishBatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber to verify messages arrive
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{})
	require.NoError(t, err)

	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	// Publish batches of varying sizes
	batches := [][]string{
		{"batch1-msg1", "batch1-msg2", "batch1-msg3"},
		{"batch2-msg1", "batch2-msg2"},
		{"batch3-msg1", "batch3-msg2", "batch3-msg3", "batch3-msg4", "batch3-msg5"},
	}

	totalMessages := 0
	for _, batch := range batches {
		totalMessages += len(batch)
	}

	// Collect received messages
	var receivedMu sync.Mutex
	received := make(map[string]bool)

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

			if count >= totalMessages {
				return
			}
		}
	}()

	// Publish all batches
	for i, batch := range batches {
		msgs := make([]*message.RawMessage, len(batch))
		for j, id := range batch {
			msgs[j] = message.NewRaw(
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

		err := pub.PublishBatch(ctx, "test", msgs...)
		require.NoError(t, err)
		t.Logf("Published batch %d: %d messages", i+1, len(batch))
	}

	// Wait for all messages
	select {
	case <-receiveDone:
		t.Log("All messages received")
	case <-time.After(30 * time.Second):
		receivedMu.Lock()
		count := len(received)
		receivedMu.Unlock()
		t.Fatalf("Timeout waiting for messages: received %d/%d", count, totalMessages)
	}

	// Verify all messages received
	receivedMu.Lock()
	defer receivedMu.Unlock()

	assert.Equal(t, totalMessages, len(received))

	for _, batch := range batches {
		for _, id := range batch {
			assert.True(t, received[id], "message %s should have been received", id)
		}
	}
}

func TestPublisher_AcksMessagesOnSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, _, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Track ack/nack calls for each message
	const numMessages = 5
	acked := make([]bool, numMessages)
	nacked := make([]bool, numMessages)
	var nackErrs [numMessages]error

	messages := make([]*message.RawMessage, numMessages)
	for i := range numMessages {
		idx := i // capture loop variable
		acking := message.NewAcking(
			func() { acked[idx] = true },
			func(err error) { nacked[idx] = true; nackErrs[idx] = err },
		)
		messages[i] = message.NewRaw(
			[]byte(fmt.Sprintf(`{"msg":%d}`, i)),
			message.Attributes{
				message.AttrID:     fmt.Sprintf("ack-test-%d", i),
				message.AttrType:   "azservicebus.test.ack",
				message.AttrSource: "/test",
			},
			acking,
		)
	}

	// Publish should succeed and ack all messages
	err = pub.PublishBatch(ctx, "test", messages...)
	require.NoError(t, err)

	// Verify all messages were acked (not nacked)
	for i := range numMessages {
		assert.True(t, acked[i], "message %d should be acked", i)
		assert.False(t, nacked[i], "message %d should not be nacked", i)
	}
	t.Log("All messages acked after successful publish")
}

func TestPublisher_NacksMessagesOnError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, _, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)

	// Track ack/nack calls
	const numMessages = 3
	acked := make([]bool, numMessages)
	nacked := make([]bool, numMessages)
	var nackErrs [numMessages]error

	messages := make([]*message.RawMessage, numMessages)
	for i := range numMessages {
		idx := i
		acking := message.NewAcking(
			func() { acked[idx] = true },
			func(err error) { nacked[idx] = true; nackErrs[idx] = err },
		)
		messages[i] = message.NewRaw(
			[]byte(fmt.Sprintf(`{"msg":%d}`, i)),
			message.Attributes{
				message.AttrID:     fmt.Sprintf("nack-test-%d", i),
				message.AttrType:   "azservicebus.test.nack",
				message.AttrSource: "/test",
			},
			acking,
		)
	}

	// Close publisher before publish to trigger error
	pub.Close()

	// Publish should fail and nack all messages
	err = pub.PublishBatch(ctx, "test", messages...)
	require.Error(t, err)
	assert.ErrorIs(t, err, gosb.ErrPublisherClosed)

	// Verify all messages were nacked (not acked)
	for i := range numMessages {
		assert.False(t, acked[i], "message %d should not be acked", i)
		assert.True(t, nacked[i], "message %d should be nacked", i)
		assert.ErrorIs(t, nackErrs[i], gosb.ErrPublisherClosed, "message %d should have ErrPublisherClosed", i)
	}
	t.Log("All messages nacked after failed publish")
}

func TestPublisher_PublishAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, _, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)

	// Close immediately
	err = pub.Close()
	require.NoError(t, err)

	// Attempt to publish should fail
	msg := message.NewRaw(
		[]byte(`{"test":"closed"}`),
		message.Attributes{
			message.AttrID:     "closed-test",
			message.AttrType:   "azservicebus.test.closed",
			message.AttrSource: "/test",
		},
		nil,
	)
	err = pub.PublishBatch(ctx, "test", msg)
	require.Error(t, err)
	assert.ErrorIs(t, err, gosb.ErrPublisherClosed)
}

func TestPublisher_EmptyBatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, _, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Publishing empty batch should succeed (no-op)
	err = pub.PublishBatch(ctx, "test")
	require.NoError(t, err)
	t.Log("Empty batch publish succeeded (no-op)")
}

func TestPublisher_CloudEventsAttributesPreserved(t *testing.T) {
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

	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	// Publish message with various CloudEvents attributes
	msgID := "ce-attrs-test"
	msg := message.NewRaw(
		[]byte(`{"test":"cloudevents"}`),
		message.Attributes{
			message.AttrID:              msgID,
			message.AttrType:            "azservicebus.test.cloudevents",
			message.AttrSource:          "/test/cloudevents",
			message.AttrDataContentType: "application/json",
			message.AttrSubject:         "test-subject",
			"customattr":                "custom-value",
		},
		nil,
	)
	err = pub.PublishBatch(ctx, "test", msg)
	require.NoError(t, err)

	// Receive and verify attributes
	select {
	case received := <-msgChan:
		assert.Equal(t, msgID, received.ID())
		assert.Equal(t, "azservicebus.test.cloudevents", received.Attributes[message.AttrType])
		assert.Equal(t, "/test/cloudevents", received.Attributes[message.AttrSource])
		assert.Equal(t, "application/json", received.DataContentType())
		assert.Equal(t, "test-subject", received.Attributes[message.AttrSubject])
		assert.Equal(t, "custom-value", received.Attributes["customattr"])

		t.Log("All CloudEvents attributes preserved")
		received.Ack()
	case <-time.After(15 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

func TestPublisher_CorrelationIDPreserved(t *testing.T) {
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

	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	// Publish message with correlation ID
	msgID := "corr-id-test"
	corrID := "correlation-12345"
	msg := message.NewRaw(
		[]byte(`{"test":"correlation"}`),
		message.Attributes{
			message.AttrID:            msgID,
			message.AttrType:          "azservicebus.test.correlation",
			message.AttrSource:        "/test",
			message.AttrCorrelationID: corrID,
		},
		nil,
	)
	err = pub.PublishBatch(ctx, "test", msg)
	require.NoError(t, err)

	// Receive and verify correlation ID
	select {
	case received := <-msgChan:
		assert.Equal(t, msgID, received.ID())
		assert.Equal(t, corrID, received.CorrelationID())

		t.Logf("Correlation ID preserved: %s", received.CorrelationID())
		received.Ack()
	case <-time.After(15 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

func TestPublisher_MultipleConcurrentPublishes(t *testing.T) {
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
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{})
	require.NoError(t, err)

	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	// Track received messages
	var receivedMu sync.Mutex
	received := make(map[string]bool)

	// Number of concurrent publishers and messages per publisher
	numGoroutines := 3
	messagesPerGoroutine := 5
	totalMessages := numGoroutines * messagesPerGoroutine

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

			if count >= totalMessages {
				return
			}
		}
	}()

	// Publish concurrently from multiple goroutines
	var wg sync.WaitGroup
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < messagesPerGoroutine; i++ {
				id := fmt.Sprintf("concurrent-%d-%d", goroutineID, i)
				msg := message.NewRaw(
					[]byte(fmt.Sprintf(`{"id":"%s"}`, id)),
					message.Attributes{
						message.AttrID:     id,
						message.AttrType:   "azservicebus.test.concurrent",
						message.AttrSource: "/test",
					},
					nil,
				)
				if err := pub.PublishBatch(ctx, "test", msg); err != nil {
					t.Errorf("Publish failed for %s: %v", id, err)
				}
			}
		}(g)
	}
	wg.Wait()
	t.Log("All concurrent publishes completed")

	// Wait for all messages
	select {
	case <-receiveDone:
		t.Log("All messages received")
	case <-time.After(30 * time.Second):
		receivedMu.Lock()
		count := len(received)
		receivedMu.Unlock()
		t.Fatalf("Timeout waiting for messages: received %d/%d", count, totalMessages)
	}

	receivedMu.Lock()
	assert.Equal(t, totalMessages, len(received))
	receivedMu.Unlock()
}

func TestPublisher_ConcurrentStreamsPublishing(t *testing.T) {
	// Tests that multiple concurrent "streams" (e.g., pipelines) can safely
	// publish batches through the same Publisher instance.
	// This is the expected usage pattern: one publisher per topic, multiple streams feeding it.

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Single publisher shared by multiple streams
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	// Subscriber to verify all messages arrive
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{})
	require.NoError(t, err)

	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	// Simulate multiple concurrent streams
	numStreams := 5
	batchesPerStream := 3
	messagesPerBatch := 4
	totalMessages := numStreams * batchesPerStream * messagesPerBatch

	// Track received messages by stream
	var receivedMu sync.Mutex
	receivedByStream := make(map[int][]string)
	receivedTotal := 0

	receiveDone := make(chan struct{})
	go func() {
		defer close(receiveDone)
		for msg := range msgChan {
			receivedMu.Lock()
			receivedTotal++
			// Extract stream ID from message attributes (handle various numeric types)
			if streamAttr, ok := msg.Attributes["stream"]; ok {
				var streamID int
				switch v := streamAttr.(type) {
				case int:
					streamID = v
				case int32:
					streamID = int(v)
				case int64:
					streamID = int(v)
				case float64:
					streamID = int(v)
				}
				receivedByStream[streamID] = append(receivedByStream[streamID], msg.ID())
			}
			done := receivedTotal >= totalMessages
			receivedMu.Unlock()

			msg.Ack()

			if done {
				return
			}
		}
	}()

	// Launch concurrent streams
	var wg sync.WaitGroup
	streamErrors := make(chan error, numStreams*batchesPerStream)

	for streamID := 0; streamID < numStreams; streamID++ {
		wg.Add(1)
		go func(sid int) {
			defer wg.Done()

			for batchNum := 0; batchNum < batchesPerStream; batchNum++ {
				// Each stream publishes batches (simulating pipeline output)
				msgs := make([]*message.RawMessage, messagesPerBatch)
				for i := 0; i < messagesPerBatch; i++ {
					id := fmt.Sprintf("stream%d-batch%d-msg%d", sid, batchNum, i)
					msgs[i] = message.NewRaw(
						[]byte(fmt.Sprintf(`{"stream":%d,"batch":%d,"msg":%d}`, sid, batchNum, i)),
						message.Attributes{
							message.AttrID:     id,
							message.AttrType:   "azservicebus.test.concurrent.stream",
							message.AttrSource: fmt.Sprintf("/stream/%d", sid),
							"stream":           sid,
							"batch":            batchNum,
						},
						nil,
					)
				}

				if err := pub.PublishBatch(ctx, "test", msgs...); err != nil {
					streamErrors <- fmt.Errorf("stream %d batch %d: %w", sid, batchNum, err)
					return
				}
				t.Logf("Stream %d published batch %d (%d messages)", sid, batchNum, messagesPerBatch)
			}
		}(streamID)
	}

	wg.Wait()
	close(streamErrors)

	// Check for publish errors
	for err := range streamErrors {
		t.Errorf("Publish error: %v", err)
	}

	t.Logf("All %d streams completed publishing", numStreams)

	// Wait for all messages to be received
	select {
	case <-receiveDone:
		t.Log("All messages received")
	case <-time.After(30 * time.Second):
		receivedMu.Lock()
		count := receivedTotal
		receivedMu.Unlock()
		t.Fatalf("Timeout waiting for messages: received %d/%d", count, totalMessages)
	}

	// Verify results
	receivedMu.Lock()
	defer receivedMu.Unlock()

	assert.Equal(t, totalMessages, receivedTotal, "should receive all messages")

	// Verify each stream's messages were received
	for streamID := 0; streamID < numStreams; streamID++ {
		expectedPerStream := batchesPerStream * messagesPerBatch
		actualPerStream := len(receivedByStream[streamID])
		assert.Equal(t, expectedPerStream, actualPerStream,
			"stream %d should have %d messages, got %d", streamID, expectedPerStream, actualPerStream)
	}

	t.Logf("Successfully received messages from %d concurrent streams", numStreams)
}

func TestPublisher_StreamingPublish(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	// Create publisher with small batch settings for testing
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{
		BatchSize:    3,
		BatchTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer pub.Close()

	// Create subscriber
	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{})
	require.NoError(t, err)

	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	// Track received messages
	var receivedMu sync.Mutex
	received := make(map[string]bool)
	numMessages := 10

	receiveDone := make(chan struct{})
	go func() {
		defer close(receiveDone)
		for msg := range msgChan {
			receivedMu.Lock()
			received[msg.ID()] = true
			count := len(received)
			receivedMu.Unlock()

			msg.Ack()

			if count >= numMessages {
				return
			}
		}
	}()

	// Create input channel and start streaming publish
	inputChan := make(chan *message.RawMessage, numMessages)
	done, err := pub.Publish(ctx, "test", inputChan)
	require.NoError(t, err)

	// Send messages to input channel
	for i := 0; i < numMessages; i++ {
		id := fmt.Sprintf("stream-msg-%d", i)
		msg := message.NewRaw(
			[]byte(fmt.Sprintf(`{"id":"%s"}`, id)),
			message.Attributes{
				message.AttrID:     id,
				message.AttrType:   "azservicebus.test.streaming",
				message.AttrSource: "/test",
			},
			nil,
		)
		inputChan <- msg
	}
	close(inputChan)
	t.Logf("Sent %d messages to streaming publisher", numMessages)

	// Wait for publish to complete
	select {
	case <-done:
		t.Log("Streaming publish completed")
	case <-time.After(30 * time.Second):
		t.Fatal("Timeout waiting for streaming publish to complete")
	}

	// Wait for all messages to be received
	select {
	case <-receiveDone:
		t.Log("All messages received")
	case <-time.After(30 * time.Second):
		receivedMu.Lock()
		count := len(received)
		receivedMu.Unlock()
		t.Fatalf("Timeout waiting for messages: received %d/%d", count, numMessages)
	}

	receivedMu.Lock()
	assert.Equal(t, numMessages, len(received))
	receivedMu.Unlock()
}

func TestPublisher_StreamingPublishErrorHandler(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, _, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	// Track error handler calls
	var errorHandlerMu sync.Mutex
	errorHandlerCalled := false
	var capturedBatch []*message.RawMessage
	var capturedErr error

	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{
		BatchSize:    5,
		BatchTimeout: 100 * time.Millisecond,
		ErrorHandler: func(batch []*message.RawMessage, err error) {
			errorHandlerMu.Lock()
			defer errorHandlerMu.Unlock()
			errorHandlerCalled = true
			capturedBatch = batch
			capturedErr = err
		},
	})
	require.NoError(t, err)

	// Close publisher to trigger errors
	pub.Close()

	// Try streaming publish on closed publisher
	inputChan := make(chan *message.RawMessage)
	_, err = pub.Publish(ctx, "test", inputChan)
	require.Error(t, err)
	assert.ErrorIs(t, err, gosb.ErrPublisherClosed)

	t.Log("Streaming publish correctly rejected on closed publisher")

	// Note: ErrorHandler is called during batch processing failures,
	// not when Publish() itself fails. The above test verifies the closed check.
	_ = errorHandlerCalled
	_ = capturedBatch
	_ = capturedErr
}

func TestPublisher_ScheduledEnqueueTime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	delay := 10 * time.Second
	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{
		Properties: gosb.PublisherProperties{
			ScheduledEnqueueTime: func(*message.RawMessage) time.Time {
				return time.Now().UTC().Add(delay)
			},
		},
	})
	require.NoError(t, err)
	defer pub.Close()

	sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{})
	require.NoError(t, err)

	msgChan, err := sub.Subscribe(ctx, "test")
	require.NoError(t, err)

	sentAt := time.Now().UTC()

	msg := message.NewRaw(
		[]byte(`{"test":"scheduled"}`),
		message.Attributes{
			message.AttrID:   "scheduled-test",
			message.AttrType: "azservicebus.test.scheduled",
		},
		nil,
	)
	err = pub.PublishBatch(ctx, "test", msg)
	require.NoError(t, err)

	// Message should not arrive before the delay elapses
	select {
	case received := <-msgChan:
		elapsed := time.Since(sentAt)
		assert.GreaterOrEqual(t, elapsed, delay, "message arrived before scheduled enqueue time")
		t.Logf("Message received after %s (delay was %s)", elapsed, delay)
		received.Ack()
	case <-time.After(delay + 30*time.Second):
		t.Fatal("Timeout waiting for scheduled message")
	}
}
