package azservicebus

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fxsml/gopipe/channel"
	"github.com/fxsml/gopipe/message"
)

// TestSubscriber_EndToEnd tests the full subscriber workflow with real Azure Service Bus
func TestSubscriber_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create a test queue
	queueName := GenerateTestName(t, "test-subscriber")
	helper.CreateQueue(ctx, queueName)

	// Create client
	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// First, publish some test messages
	publisher := NewPublisher(client, PublisherConfig{
		PublishTimeout:   30 * time.Second,
		BatchMaxSize:     1,
		BatchMaxDuration: 1 * time.Millisecond,
	})
	defer publisher.Close()

	messageCount := 5
	messages := make([]*message.Message, 0, messageCount)
	for i := 1; i <= messageCount; i++ {
		props := &message.Properties{}
		props.Set("message_index", fmt.Sprintf("%d", i))

		msg := message.NewMessage(
			props,
			map[string]any{
				"id":      i,
				"content": fmt.Sprintf("Test message #%d", i),
			},
			time.Time{},
			nil,
			nil,
		)
		messages = append(messages, msg)
	}

	msgChan := channel.FromValues(messages...)
	done, err := publisher.Publish(queueName, msgChan)
	if err != nil {
		t.Fatalf("Failed to publish messages: %v", err)
	}

	// Wait for publishing to complete
	<-done
	t.Logf("Successfully published %d messages", messageCount)

	// Now subscribe and receive messages
	subscriber := NewSubscriber(client, SubscriberConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 5,
		ErrorHandler: func(err error) {
			t.Errorf("Unexpected error in subscriber: %v", err)
		},
	})
	defer subscriber.Close()

	subCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	receiveChan, err := subscriber.Subscribe(subCtx, queueName)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	receivedCount := 0
	for msg := range receiveChan {
		receivedCount++

		// Verify message content
		payload, ok := msg.Payload.(map[string]any)
		if !ok {
			t.Errorf("Unexpected payload type: %T", msg.Payload)
			msg.Nack(fmt.Errorf("unexpected payload type"))
			continue
		}

		t.Logf("Received message %d: %v", receivedCount, payload)

		// Acknowledge the message
		if !msg.Ack() {
			t.Errorf("Failed to ack message")
		}

		if receivedCount >= messageCount {
			cancel()
			break
		}
	}

	if receivedCount != messageCount {
		t.Errorf("Expected to receive %d messages, got %d", messageCount, receivedCount)
	}
}

// TestSubscriber_MessageMetadata tests that metadata is properly mapped from Service Bus properties
func TestSubscriber_MessageMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create a test queue
	queueName := GenerateTestName(t, "test-sub-metadata")
	helper.CreateQueue(ctx, queueName)

	// Create client
	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Publish a message with metadata
	publisher := NewPublisher(client, PublisherConfig{
		PublishTimeout:   30 * time.Second,
		BatchMaxSize:     1,
		BatchMaxDuration: 1 * time.Millisecond,
	})
	defer publisher.Close()

	props := &message.Properties{}
	messageID := "test-msg-456"
	props.Set("message_id", messageID)
	props.Set("subject", "test-subject")
	props.Set("content_type", "application/json")
	props.Set("correlation_id", "corr-456")
	props.Set("custom_prop", "custom_value")

	msg := message.NewMessage(
		props,
		map[string]string{"data": "test"},
		time.Time{},
		nil,
		nil,
	)

	msgChan := channel.FromValues(msg)
	done, err := publisher.Publish(queueName, msgChan)
	if err != nil {
		t.Fatalf("Failed to publish message: %v", err)
	}
	<-done

	// Subscribe and verify metadata
	subscriber := NewSubscriber(client, SubscriberConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 1,
		ErrorHandler: func(err error) {
			t.Errorf("Unexpected error in subscriber: %v", err)
		},
	})
	defer subscriber.Close()

	subCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	receiveChan, err := subscriber.Subscribe(subCtx, queueName)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	select {
	case receivedMsg := <-receiveChan:
		// Verify metadata was mapped correctly
		if val, ok := receivedMsg.Properties().Get("message_id"); !ok || val != messageID {
			t.Errorf("Expected message_id %s, got %v", messageID, val)
		}

		if val, ok := receivedMsg.Properties().Get("subject"); !ok || val != "test-subject" {
			t.Errorf("Expected subject 'test-subject', got %v", val)
		}

		if val, ok := receivedMsg.Properties().Get("content_type"); !ok || val != "application/json" {
			t.Errorf("Expected content_type 'application/json', got %v", val)
		}

		if val, ok := receivedMsg.Properties().Get("correlation_id"); !ok || val != "corr-456" {
			t.Errorf("Expected correlation_id 'corr-456', got %v", val)
		}

		if val, ok := receivedMsg.Properties().Get("custom_prop"); !ok || val != "custom_value" {
			t.Errorf("Expected custom_prop 'custom_value', got %v", val)
		}

		// Acknowledge the message
		receivedMsg.Ack()

	case <-time.After(30 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

// TestSubscriber_ClosedSubscriberReturnsError tests that subscribing with a closed subscriber returns an error
func TestSubscriber_ClosedSubscriberReturnsError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	ctx := context.Background()

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	subscriber := NewSubscriber(client, SubscriberConfig{
		ErrorHandler: func(err error) {
			t.Errorf("Unexpected error in subscriber: %v", err)
		},
	})

	// Close the subscriber
	if err := subscriber.Close(); err != nil {
		t.Fatalf("Failed to close subscriber: %v", err)
	}

	// Try to subscribe after closing
	msgChan, err := subscriber.Subscribe(ctx, "test-queue")
	if err == nil {
		t.Error("Expected error when subscribing with closed subscriber, got nil")
	}
	if msgChan != nil {
		t.Error("Expected nil message channel when subscriber is closed, got non-nil")
	}

	expectedErrMsg := "subscriber is closed"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message %q, got %q", expectedErrMsg, err.Error())
	}
}

// TestSubscriber_CloseIdempotent tests that closing a subscriber multiple times is safe
func TestSubscriber_CloseIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	ctx := context.Background()

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	subscriber := NewSubscriber(client, SubscriberConfig{
		ErrorHandler: func(err error) {
			t.Errorf("Unexpected error in subscriber: %v", err)
		},
	})

	// Close multiple times
	if err := subscriber.Close(); err != nil {
		t.Fatalf("First close failed: %v", err)
	}

	if err := subscriber.Close(); err != nil {
		t.Fatalf("Second close failed: %v", err)
	}

	if err := subscriber.Close(); err != nil {
		t.Fatalf("Third close failed: %v", err)
	}
}

// TestSubscriber_PublishSubscribeRoundtrip tests publishing and subscribing in a roundtrip
func TestSubscriber_PublishSubscribeRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create a test queue
	queueName := GenerateTestName(t, "test-roundtrip")
	helper.CreateQueue(ctx, queueName)

	// Create client
	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Start subscriber first
	subscriber := NewSubscriber(client, SubscriberConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 10,
		ErrorHandler: func(err error) {
			t.Errorf("Unexpected error in subscriber: %v", err)
		},
	})
	defer subscriber.Close()

	subCtx, cancelSub := context.WithTimeout(ctx, 30*time.Second)
	defer cancelSub()

	receiveChan, err := subscriber.Subscribe(subCtx, queueName)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Publish messages
	publisher := NewPublisher(client, PublisherConfig{
		PublishTimeout:   30 * time.Second,
		BatchMaxSize:     5,
		BatchMaxDuration: 10 * time.Millisecond,
	})
	defer publisher.Close()

	messageCount := 10
	messages := make([]*message.Message, 0, messageCount)
	for i := 1; i <= messageCount; i++ {
		props := &message.Properties{}
		props.Set("index", fmt.Sprintf("%d", i))

		msg := message.NewMessage(
			props,
			map[string]any{"id": i, "data": fmt.Sprintf("Message %d", i)},
			time.Time{},
			nil,
			nil,
		)
		messages = append(messages, msg)
	}

	msgChan := channel.FromValues(messages...)
	done, err := publisher.Publish(queueName, msgChan)
	if err != nil {
		t.Fatalf("Failed to publish messages: %v", err)
	}

	// Wait for publishing to complete
	<-done
	t.Logf("Published %d messages", messageCount)

	// Receive all messages
	receivedCount := 0
	for msg := range receiveChan {
		receivedCount++
		t.Logf("Received message %d", receivedCount)

		// Acknowledge
		if !msg.Ack() {
			t.Errorf("Failed to ack message %d", receivedCount)
		}

		if receivedCount >= messageCount {
			cancelSub()
			break
		}
	}

	if receivedCount != messageCount {
		t.Errorf("Expected %d messages, received %d", messageCount, receivedCount)
	}
}

// TestSubscriber_AutoDetectQueueAndTopic verifies that Subscribe() automatically detects
// whether the input is a queue or topic based on the presence of "/"
func TestSubscriber_AutoDetectQueueAndTopic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Test 1: Queue (no "/" in name)
	queueName := GenerateTestName(t, "test-autodetect-queue")
	helper.CreateQueue(ctx, queueName)
	t.Logf("Testing queue auto-detection with: %s", queueName)

	// Create client
	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Create subscriber
	subscriber := NewSubscriber(client, SubscriberConfig{
		ErrorHandler: func(err error) {
			t.Errorf("Unexpected error in subscriber: %v", err)
		},
	})
	defer func() {
		if err := subscriber.Close(); err != nil {
			t.Errorf("Failed to close subscriber: %v", err)
		}
	}()

	// Publish test messages to queue
	messages := []*message.Message{
		message.NewMessage(
			&message.Properties{},
			map[string]any{"type": "queue", "content": "Queue message"},
			time.Time{},
			nil,
			nil,
		),
	}
	msgChan := channel.FromValues(messages...)

	publisher := NewPublisher(client, PublisherConfig{})
	defer publisher.Close()

	publishDone, err := publisher.Publish(queueName, msgChan)
	if err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}
	<-publishDone

	// Subscribe using auto-detection (no "/" should be treated as queue)
	subCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	receiveMsgChan, err := subscriber.Subscribe(subCtx, queueName)
	if err != nil {
		t.Fatalf("Failed to subscribe to queue: %v", err)
	}

	// Receive message
	select {
	case msg := <-receiveMsgChan:
		payloadMap, ok := msg.Payload.(map[string]any)
		if !ok {
			t.Fatalf("Expected map[string]any, got %T", msg.Payload)
		}
		if payloadMap["type"] != "queue" {
			t.Errorf("Expected type=queue, got %v", payloadMap["type"])
		}
		if !msg.Ack() {
			t.Errorf("Failed to ack message")
		}
		t.Log("Successfully received message from queue using auto-detection")
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for message from queue")
	}

	// Test 2: Topic (with "/" in format: topic/subscription)
	topicName := GenerateTestName(t, "test-autodetect-topic")
	subscriptionName := "test-sub"
	helper.CreateTopic(ctx, topicName)
	helper.CreateSubscription(ctx, topicName, subscriptionName)
	topicPath := topicName + "/" + subscriptionName
	t.Logf("Testing topic auto-detection with: %s", topicPath)

	// Create a new subscriber for the topic
	topicSubscriber := NewSubscriber(client, SubscriberConfig{
		ErrorHandler: func(err error) {
			t.Errorf("Unexpected error in topic subscriber: %v", err)
		},
	})
	defer func() {
		if err := topicSubscriber.Close(); err != nil {
			t.Errorf("Failed to close topic subscriber: %v", err)
		}
	}()

	// Publish test messages to topic
	topicMessages := []*message.Message{
		message.NewMessage(
			&message.Properties{},
			map[string]any{"type": "topic", "content": "Topic message"},
			time.Time{},
			nil,
			nil,
		),
	}
	topicMsgChan := channel.FromValues(topicMessages...)

	publishDone2, err := publisher.Publish(topicName, topicMsgChan)
	if err != nil {
		t.Fatalf("Failed to publish to topic: %v", err)
	}
	<-publishDone2

	// Subscribe using auto-detection (with "/" should be treated as topic/subscription)
	topicSubCtx, topicCancel := context.WithTimeout(ctx, 30*time.Second)
	defer topicCancel()

	topicReceiveMsgChan, err := topicSubscriber.Subscribe(topicSubCtx, topicPath)
	if err != nil {
		t.Fatalf("Failed to subscribe to topic: %v", err)
	}

	// Receive message
	select {
	case msg := <-topicReceiveMsgChan:
		payloadMap, ok := msg.Payload.(map[string]any)
		if !ok {
			t.Fatalf("Expected map[string]any, got %T", msg.Payload)
		}
		if payloadMap["type"] != "topic" {
			t.Errorf("Expected type=topic, got %v", payloadMap["type"])
		}
		if !msg.Ack() {
			t.Errorf("Failed to ack message")
		}
		t.Log("Successfully received message from topic using auto-detection")
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for message from topic")
	}

	t.Log("Auto-detection test completed successfully")
}

// TestSubscriber_RecreateReceiver tests the recreateReceiver functionality
func TestSubscriber_RecreateReceiver(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	ctx := context.Background()

	// Create a test queue
	queueName := GenerateTestName(t, "test-recreate-receiver")
	helper.CreateQueue(ctx, queueName)

	// Create client
	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create subscriber
	subscriber := NewSubscriber(client, SubscriberConfig{
		ErrorHandler: func(err error) {
			t.Errorf("Unexpected error in subscriber: %v", err)
		},
	})
	defer subscriber.Close()

	// First, subscribe to create the receiver
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	receiveChan, err := subscriber.Subscribe(subCtx, queueName)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	t.Log("Initial subscription created successfully")

	// Publish a message
	publisher := NewPublisher(client, PublisherConfig{})
	defer publisher.Close()

	msg1 := &message.Message{
		Payload: map[string]any{"id": 1, "content": "First message"},
	}
	msgChan1 := channel.FromValues(msg1)
	done1, err := publisher.Publish(queueName, msgChan1)
	if err != nil {
		t.Fatalf("Failed to publish first message: %v", err)
	}
	<-done1

	// Receive the first message
	select {
	case msg := <-receiveChan:
		t.Logf("Received first message: %v", msg.Payload)
		msg.Ack()
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for first message")
	}

	// Now call recreateReceiver to simulate connection recovery
	newReceiver, err := subscriber.recreateReceiver(queueName)
	if err != nil {
		t.Fatalf("Failed to recreate receiver: %v", err)
	}

	if newReceiver == nil {
		t.Fatal("recreateReceiver returned nil receiver")
	}

	t.Log("Receiver recreated successfully")

	// Publish another message
	msg2 := &message.Message{
		Payload: map[string]any{"id": 2, "content": "Second message after recreation"},
	}
	msgChan2 := channel.FromValues(msg2)
	done2, err := publisher.Publish(queueName, msgChan2)
	if err != nil {
		t.Fatalf("Failed to publish second message: %v", err)
	}
	<-done2

	// Receive the second message with recreated receiver
	select {
	case msg := <-receiveChan:
		t.Logf("Received second message after recreation: %v", msg.Payload)
		msg.Ack()
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for second message after recreation")
	}

	t.Log("Receiver successfully processed message after recreation")
}

// TestSubscriber_RecreateReceiverWhenClosing tests that recreateReceiver fails gracefully when subscriber is closing
func TestSubscriber_RecreateReceiverWhenClosing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	ctx := context.Background()

	// Create a test queue
	queueName := GenerateTestName(t, "test-recreate-closing")
	helper.CreateQueue(ctx, queueName)

	// Create client
	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create subscriber
	subscriber := NewSubscriber(client, SubscriberConfig{
		ErrorHandler: func(err error) {
			t.Errorf("Unexpected error in subscriber: %v", err)
		},
	})

	// Subscribe to create the receiver
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	_, err = subscriber.Subscribe(subCtx, queueName)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Close the subscriber
	if err := subscriber.Close(); err != nil {
		t.Fatalf("Failed to close subscriber: %v", err)
	}

	// Try to recreate receiver after closing
	_, err = subscriber.recreateReceiver(queueName)
	if err == nil {
		t.Error("Expected error when recreating receiver after close, got nil")
	}

	if err != nil && !contains(err.Error(), "closing") {
		t.Errorf("Expected error message to contain 'closing', got: %v", err)
	}

	t.Log("recreateReceiver correctly failed when subscriber is closed")
}

// TestSubscriber_RecreateReceiverForTopic tests recreating a receiver for a topic subscription
func TestSubscriber_RecreateReceiverForTopic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	ctx := context.Background()

	// Create a test topic and subscription
	topicName := GenerateTestName(t, "test-recreate-topic")
	subscriptionName := "test-sub"
	helper.CreateTopic(ctx, topicName)
	helper.CreateSubscription(ctx, topicName, subscriptionName)

	topicPath := topicName + "/" + subscriptionName

	// Create client
	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create subscriber
	subscriber := NewSubscriber(client, SubscriberConfig{
		ErrorHandler: func(err error) {
			t.Errorf("Unexpected error in subscriber: %v", err)
		},
	})
	defer subscriber.Close()

	// Subscribe to the topic
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	receiveChan, err := subscriber.Subscribe(subCtx, topicPath)
	if err != nil {
		t.Fatalf("Failed to subscribe to topic: %v", err)
	}

	t.Log("Initial topic subscription created successfully")

	// Recreate receiver to simulate connection recovery
	newReceiver, err := subscriber.recreateReceiver(topicPath)
	if err != nil {
		t.Fatalf("Failed to recreate receiver for topic: %v", err)
	}

	if newReceiver == nil {
		t.Fatal("recreateReceiver returned nil receiver for topic")
	}

	t.Log("Topic receiver recreated successfully")

	// Publish a message to the topic
	publisher := NewPublisher(client, PublisherConfig{})
	defer publisher.Close()

	msg := &message.Message{
		Payload: map[string]any{"content": "Message after topic recreation"},
	}
	msgChan := channel.FromValues(msg)
	done, err := publisher.Publish(topicName, msgChan)
	if err != nil {
		t.Fatalf("Failed to publish to topic: %v", err)
	}
	<-done

	// Receive the message with recreated receiver
	select {
	case msg := <-receiveChan:
		t.Logf("Received message from recreated topic receiver: %v", msg.Payload)
		msg.Ack()
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for message from recreated topic receiver")
	}

	t.Log("Topic receiver successfully processed message after recreation")
}

// TestSubscriber_ConnectionRecovery tests end-to-end message receiving with simulated recovery
func TestSubscriber_ConnectionRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	ctx := context.Background()

	// Create a test queue
	queueName := GenerateTestName(t, "test-sub-recovery")
	helper.CreateQueue(ctx, queueName)

	// Create client
	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create subscriber
	subscriber := NewSubscriber(client, SubscriberConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 10,
		ErrorHandler: func(err error) {
			t.Errorf("Unexpected error in subscriber: %v", err)
		},
	})
	defer subscriber.Close()

	// Subscribe
	subCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	receiveChan, err := subscriber.Subscribe(subCtx, queueName)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Create publisher
	publisher := NewPublisher(client, PublisherConfig{})
	defer publisher.Close()

	// Publish message before "connection loss"
	msg1 := &message.Message{
		Payload: map[string]any{"id": 1, "phase": "before"},
	}
	msgChan1 := channel.FromValues(msg1)
	done1, err := publisher.Publish(queueName, msgChan1)
	if err != nil {
		t.Fatalf("Failed to publish message before recovery: %v", err)
	}
	<-done1

	// Receive first message
	select {
	case msg := <-receiveChan:
		t.Logf("Received message before recovery: %v", msg.Payload)
		msg.Ack()
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for message before recovery")
	}

	// Simulate connection recovery
	_, err = subscriber.recreateReceiver(queueName)
	if err != nil {
		t.Fatalf("Failed to recreate receiver: %v", err)
	}

	t.Log("Simulated connection recovery")

	// Publish message after "connection recovery"
	msg2 := &message.Message{
		Payload: map[string]any{"id": 2, "phase": "after"},
	}
	msgChan2 := channel.FromValues(msg2)
	done2, err := publisher.Publish(queueName, msgChan2)
	if err != nil {
		t.Fatalf("Failed to publish message after recovery: %v", err)
	}
	<-done2

	// Receive second message after recovery
	select {
	case msg := <-receiveChan:
		t.Logf("Received message after recovery: %v", msg.Payload)
		msg.Ack()
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for message after recovery")
	}

	t.Log("Connection recovery test completed successfully")
}

// Helper function to check if string contains substring
