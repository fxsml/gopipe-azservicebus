package azservicebus_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	azservicebuspkg "github.com/fxsml/gopipe-azservicebus"
	"github.com/fxsml/gopipe/channel"
	"github.com/fxsml/gopipe/message"
)

// TestPublisher_EndToEnd tests the full publisher workflow with real Azure Service Bus
func TestPublisher_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create a test queue
	queueName := GenerateTestName(t, "test-publisher")
	helper.CreateQueue(ctx, queueName)

	// Create client and publisher
	client, err := azservicebuspkg.NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	publisher := azservicebuspkg.NewPublisher(client, azservicebuspkg.PublisherConfig{
		PublishTimeout:   30 * time.Second,
		BatchMaxSize:     1,
		BatchMaxDuration: 1 * time.Millisecond,
	})
	defer publisher.Close()

	// Publish test messages
	messageCount := 5
	messages := make([]*message.Message, 0, messageCount)
	for i := 1; i <= messageCount; i++ {
		msg := &message.Message{
			Payload: map[string]any{
				"id":      i,
				"content": fmt.Sprintf("Test message #%d", i),
			},
		}
		msg.Properties().Set("test_run", t.Name())
		msg.Properties().Set("message_index", fmt.Sprintf("%d", i))
		messages = append(messages, msg)
	}

	msgChan := channel.FromValues(messages...)
	done, err := publisher.Publish(queueName, msgChan)
	if err != nil {
		t.Fatalf("Failed to publish messages: %v", err)
	}

	// Wait for publishing to complete
	select {
	case <-done:
		t.Logf("Successfully published %d messages", messageCount)
	case <-time.After(30 * time.Second):
		t.Fatal("Timeout waiting for messages to be published")
	}

	// Verify messages were published by receiving them
	receiver, err := client.NewReceiverForQueue(queueName, nil)
	if err != nil {
		t.Fatalf("Failed to create receiver: %v", err)
	}
	defer receiver.Close(ctx)

	receivedCount := 0
	receiveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for receivedCount < messageCount {
		msgs, err := receiver.ReceiveMessages(receiveCtx, 1, nil)
		if err != nil {
			t.Fatalf("Failed to receive messages: %v", err)
		}

		for _, msg := range msgs {
			receivedCount++

			// Verify message content
			var payload map[string]any
			if err := json.Unmarshal(msg.Body, &payload); err != nil {
				t.Errorf("Failed to unmarshal message: %v", err)
				continue
			}

			t.Logf("Received message %d: %v", receivedCount, payload)

			// Complete the message
			if err := receiver.CompleteMessage(ctx, msg, nil); err != nil {
				t.Errorf("Failed to complete message: %v", err)
			}
		}
	}

	if receivedCount != messageCount {
		t.Errorf("Expected to receive %d messages, got %d", messageCount, receivedCount)
	}
}

// TestPublisher_WithTopic tests publishing to a topic with subscription
func TestPublisher_WithTopic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create a test topic and subscription
	topicName := GenerateTestName(t, "test-topic")
	subscriptionName := "test-sub"
	helper.CreateTopic(ctx, topicName)
	helper.CreateSubscription(ctx, topicName, subscriptionName)

	// Give Azure some time to propagate the topic/subscription
	time.Sleep(2 * time.Second)

	// Create client and publisher
	client, err := azservicebuspkg.NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	publisher := azservicebuspkg.NewPublisher(client, azservicebuspkg.PublisherConfig{
		PublishTimeout:   30 * time.Second,
		BatchMaxSize:     1,
		BatchMaxDuration: 1 * time.Millisecond,
	})
	defer publisher.Close()

	// Publish test message to topic
	msg := &message.Message{
		Payload: map[string]any{
			"message": "Hello from topic test!",
		},
	}
	msg.Properties().Set("test_run", t.Name())

	msgChan := channel.FromValues(msg)
	done, err := publisher.Publish(topicName, msgChan)
	if err != nil {
		t.Fatalf("Failed to publish message: %v", err)
	}

	// Wait for publishing to complete
	select {
	case <-done:
		t.Log("Successfully published message to topic")
	case <-time.After(30 * time.Second):
		t.Fatal("Timeout waiting for message to be published")
	}

	// Verify message was published by receiving from subscription
	receiver, err := client.NewReceiverForSubscription(topicName, subscriptionName, nil)
	if err != nil {
		t.Fatalf("Failed to create receiver: %v", err)
	}
	defer receiver.Close(ctx)

	receiveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	msgs, err := receiver.ReceiveMessages(receiveCtx, 1, nil)
	if err != nil {
		t.Fatalf("Failed to receive message: %v", err)
	}

	if len(msgs) == 0 {
		t.Fatal("No messages received from subscription")
	}

	// Verify message content
	var payload map[string]any
	if err := json.Unmarshal(msgs[0].Body, &payload); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	t.Logf("Received message from topic: %v", payload)

	// Complete the message
	if err := receiver.CompleteMessage(ctx, msgs[0], nil); err != nil {
		t.Errorf("Failed to complete message: %v", err)
	}
}

// TestPublisher_BatchMessages tests batch publishing
func TestPublisher_BatchMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create a test queue
	queueName := GenerateTestName(t, "test-batch")
	helper.CreateQueue(ctx, queueName)

	// Create client and publisher with batching
	client, err := azservicebuspkg.NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	publisher := azservicebuspkg.NewPublisher(client, azservicebuspkg.PublisherConfig{
		PublishTimeout:   30 * time.Second,
		BatchMaxSize:     10, // Batch up to 10 messages
		BatchMaxDuration: 100 * time.Millisecond,
	})
	defer publisher.Close()

	// Publish many messages
	messageCount := 25
	messages := make([]*message.Message, 0, messageCount)
	for i := 1; i <= messageCount; i++ {
		msg := &message.Message{
			Payload: map[string]any{
				"id": i,
			},
		}
		messages = append(messages, msg)
	}

	msgChan := channel.FromValues(messages...)
	done, err := publisher.Publish(queueName, msgChan)
	if err != nil {
		t.Fatalf("Failed to publish messages: %v", err)
	}

	// Wait for publishing to complete
	select {
	case <-done:
		t.Logf("Successfully published %d messages in batches", messageCount)
	case <-time.After(30 * time.Second):
		t.Fatal("Timeout waiting for messages to be published")
	}

	// Verify messages were published
	receiver, err := client.NewReceiverForQueue(queueName, nil)
	if err != nil {
		t.Fatalf("Failed to create receiver: %v", err)
	}
	defer receiver.Close(ctx)

	receivedCount := 0
	receiveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for receivedCount < messageCount {
		msgs, err := receiver.ReceiveMessages(receiveCtx, 10, nil)
		if err != nil {
			t.Fatalf("Failed to receive messages: %v", err)
		}

		for _, msg := range msgs {
			receivedCount++
			if err := receiver.CompleteMessage(ctx, msg, nil); err != nil {
				t.Errorf("Failed to complete message: %v", err)
			}
		}
	}

	if receivedCount != messageCount {
		t.Errorf("Expected to receive %d messages, got %d", messageCount, receivedCount)
	}
}

// TestPublisher_MessageMetadata tests that metadata is properly mapped to Service Bus properties
func TestPublisher_MessageMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create a test queue
	queueName := GenerateTestName(t, "test-metadata")
	helper.CreateQueue(ctx, queueName)

	// Create client and publisher
	client, err := azservicebuspkg.NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	publisher := azservicebuspkg.NewPublisher(client, azservicebuspkg.PublisherConfig{
		PublishTimeout:   30 * time.Second,
		BatchMaxSize:     1,
		BatchMaxDuration: 1 * time.Millisecond,
	})
	defer publisher.Close()

	// Create message with metadata
	msg := &message.Message{
		Payload: map[string]string{"data": "test"},
	}
	messageID := "test-msg-123"
	msg.Properties().Set("message_id", messageID)
	msg.Properties().Set("subject", "test-subject")
	msg.Properties().Set("content_type", "application/json")
	msg.Properties().Set("correlation_id", "corr-123")
	msg.Properties().Set("custom_prop", "custom_value")

	msgChan := channel.FromValues(msg)
	done, err := publisher.Publish(queueName, msgChan)
	if err != nil {
		t.Fatalf("Failed to publish message: %v", err)
	}

	// Wait for publishing to complete
	<-done

	// Receive and verify metadata
	receiver, err := client.NewReceiverForQueue(queueName, nil)
	if err != nil {
		t.Fatalf("Failed to create receiver: %v", err)
	}
	defer receiver.Close(ctx)

	receiveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	msgs, err := receiver.ReceiveMessages(receiveCtx, 1, nil)
	if err != nil {
		t.Fatalf("Failed to receive message: %v", err)
	}

	if len(msgs) == 0 {
		t.Fatal("No messages received")
	}

	receivedMsg := msgs[0]

	// Verify metadata was mapped correctly
	if receivedMsg.MessageID != messageID {
		t.Errorf("Expected MessageID %s, got %v", messageID, receivedMsg.MessageID)
	}

	if receivedMsg.Subject == nil || *receivedMsg.Subject != "test-subject" {
		t.Errorf("Expected Subject 'test-subject', got %v", receivedMsg.Subject)
	}

	if receivedMsg.ContentType == nil || *receivedMsg.ContentType != "application/json" {
		t.Errorf("Expected ContentType 'application/json', got %v", receivedMsg.ContentType)
	}

	if receivedMsg.CorrelationID == nil || *receivedMsg.CorrelationID != "corr-123" {
		t.Errorf("Expected CorrelationID 'corr-123', got %v", receivedMsg.CorrelationID)
	}

	if customProp, ok := receivedMsg.ApplicationProperties["custom_prop"]; !ok || customProp != "custom_value" {
		t.Errorf("Expected custom_prop 'custom_value', got %v", customProp)
	}

	// Complete the message
	if err := receiver.CompleteMessage(ctx, receivedMsg, nil); err != nil {
		t.Errorf("Failed to complete message: %v", err)
	}
}

// TestPublisher_ClosedPublisherReturnsError tests that publishing to a closed publisher returns an error
func TestPublisher_ClosedPublisherReturnsError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	ctx := context.Background()

	client, err := azservicebuspkg.NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	publisher := azservicebuspkg.NewPublisher(client, azservicebuspkg.PublisherConfig{})

	// Close the publisher
	if err := publisher.Close(); err != nil {
		t.Fatalf("Failed to close publisher: %v", err)
	}

	// Try to publish after closing
	msg := &message.Message{
		Payload: map[string]string{"test": "data"},
	}

	done, err := publisher.Publish("test-queue", channel.FromValues(msg))
	if err == nil {
		t.Error("Expected error when publishing to closed publisher, got nil")
	}
	if done != nil {
		t.Error("Expected nil done channel when publisher is closed, got non-nil")
	}

	expectedErrMsg := "publisher is closed"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message %q, got %q", expectedErrMsg, err.Error())
	}
}

// TestPublisher_CloseIdempotent tests that closing a publisher multiple times is safe
func TestPublisher_CloseIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	ctx := context.Background()

	client, err := azservicebuspkg.NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	publisher := azservicebuspkg.NewPublisher(client, azservicebuspkg.PublisherConfig{})

	// Close multiple times
	if err := publisher.Close(); err != nil {
		t.Fatalf("First close failed: %v", err)
	}

	if err := publisher.Close(); err != nil {
		t.Fatalf("Second close failed: %v", err)
	}

	if err := publisher.Close(); err != nil {
		t.Fatalf("Third close failed: %v", err)
	}
}

// TestPublisher_ConcurrentPublish tests concurrent publishing to different queues
func TestPublisher_ConcurrentPublish(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create multiple test queues
	queueCount := 3
	queues := make([]string, queueCount)
	for i := range queues {
		queueName := GenerateTestName(t, fmt.Sprintf("test-concurrent-%d", i))
		helper.CreateQueue(ctx, queueName)
		queues[i] = queueName
	}

	// Create client and publisher
	client, err := azservicebuspkg.NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	publisher := azservicebuspkg.NewPublisher(client, azservicebuspkg.PublisherConfig{
		PublishTimeout:   30 * time.Second,
		BatchMaxSize:     1,
		BatchMaxDuration: 1 * time.Millisecond,
	})
	defer publisher.Close()

	// Publish to all queues concurrently
	doneChans := make([]<-chan struct{}, queueCount)
	for i, queueName := range queues {
		msg := &message.Message{
			Payload: map[string]any{
				"queue_index": i,
				"message":     fmt.Sprintf("Message for queue %d", i),
			},
		}

		msgChan := channel.FromValues(msg)
		done, err := publisher.Publish(queueName, msgChan)
		if err != nil {
			t.Fatalf("Failed to publish to queue %s: %v", queueName, err)
		}
		doneChans[i] = done
	}

	// Wait for all publishing to complete
	for i, done := range doneChans {
		select {
		case <-done:
			t.Logf("Queue %d publishing complete", i)
		case <-time.After(30 * time.Second):
			t.Fatalf("Timeout waiting for queue %d publishing", i)
		}
	}

	t.Logf("Successfully published to %d queues concurrently", queueCount)
}
