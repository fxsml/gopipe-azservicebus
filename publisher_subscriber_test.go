package azservicebus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fxsml/gopipe/message"
	_ "github.com/joho/godotenv/autoload"
)

// TestPublisher_BasicPublish tests basic publisher functionality
func TestPublisher_BasicPublish(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create a test queue
	queueName := GenerateTestName(t, "test-pub-basic")
	helper.CreateQueue(ctx, queueName)

	// Create client
	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create publisher
	publisher := NewPublisher(client, PublisherConfig{
		BatchSize:    5,
		BatchTimeout: 100 * time.Millisecond,
		SendTimeout:  30 * time.Second,
	})
	defer publisher.Close()

	// Create message channel
	msgs := make(chan *message.Message[[]byte])

	// Start publishing
	done, err := publisher.Publish(ctx, queueName, msgs)
	if err != nil {
		t.Fatalf("Failed to start publishing: %v", err)
	}

	// Send test messages
	messageCount := 10
	go func() {
		defer close(msgs)
		for i := 1; i <= messageCount; i++ {
			payload := map[string]any{
				"id":      i,
				"content": fmt.Sprintf("Test message #%d", i),
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Errorf("Failed to marshal payload: %v", err)
				return
			}
			msg := message.New(body, message.WithProperty[[]byte]("index", i))
			msgs <- msg
		}
	}()

	// Wait for publishing to complete
	<-done
	t.Logf("Published %d messages", messageCount)

	// Verify messages were sent by receiving them
	time.Sleep(1 * time.Second)

	receiver := NewReceiver(client, ReceiverConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: messageCount,
	})
	defer receiver.Close()

	received, err := receiver.Receive(ctx, queueName)
	if err != nil {
		t.Fatalf("Failed to receive messages: %v", err)
	}

	if len(received) != messageCount {
		t.Errorf("Expected %d messages, got %d", messageCount, len(received))
	}

	// Acknowledge messages
	for _, msg := range received {
		msg.Ack()
	}
}

// TestPublisher_SyncPublish tests synchronous publish functionality
func TestPublisher_SyncPublish(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	queueName := GenerateTestName(t, "test-pub-sync")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	publisher := NewPublisher(client, PublisherConfig{})
	defer publisher.Close()

	// Create messages
	messages := make([]*message.Message[[]byte], 5)
	for i := range messages {
		body, _ := json.Marshal(map[string]int{"id": i})
		messages[i] = message.New(body)
	}

	// Publish synchronously
	err = publisher.PublishSync(ctx, queueName, messages)
	if err != nil {
		t.Fatalf("Failed to publish synchronously: %v", err)
	}

	t.Log("Published 5 messages synchronously")
}

// TestSubscriber_BasicSubscribe tests basic subscriber functionality
func TestSubscriber_BasicSubscribe(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	queueName := GenerateTestName(t, "test-sub-basic")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// First, send some messages
	sender := NewSender(client, SenderConfig{})
	defer sender.Close()

	messageCount := 5
	messages := make([]*message.Message[[]byte], messageCount)
	for i := range messages {
		body, _ := json.Marshal(map[string]any{"id": i + 1, "text": fmt.Sprintf("message-%d", i+1)})
		messages[i] = message.New(body)
	}

	if err := sender.Send(ctx, queueName, messages); err != nil {
		t.Fatalf("Failed to send messages: %v", err)
	}
	time.Sleep(1 * time.Second)

	// Now subscribe
	subscriber := NewSubscriber(client, SubscriberConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: messageCount,
		BufferSize:      100,
		PollInterval:    500 * time.Millisecond,
	})
	defer subscriber.Close()

	msgs, err := subscriber.Subscribe(ctx, queueName)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Receive messages
	received := 0
	timeout := time.After(10 * time.Second)

loop:
	for {
		select {
		case msg, ok := <-msgs:
			if !ok {
				break loop
			}
			received++
			t.Logf("Received message %d: %s", received, string(msg.Payload()))
			msg.Ack()
			if received >= messageCount {
				break loop
			}
		case <-timeout:
			t.Logf("Timeout waiting for messages, received %d/%d", received, messageCount)
			break loop
		}
	}

	if received != messageCount {
		t.Errorf("Expected %d messages, got %d", messageCount, received)
	}
}

// TestSubscriber_TopicSubscription tests subscribing to a topic subscription
func TestSubscriber_TopicSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	topicName := GenerateTestName(t, "test-sub-topic")
	subscriptionName := "test-sub"

	helper.CreateTopic(ctx, topicName)
	helper.CreateSubscription(ctx, topicName, subscriptionName)
	time.Sleep(2 * time.Second)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Send messages to topic
	sender := NewSender(client, SenderConfig{})
	defer sender.Close()

	body, _ := json.Marshal(map[string]string{"message": "hello from topic"})
	msg := message.New(body)

	if err := sender.Send(ctx, topicName, []*message.Message[[]byte]{msg}); err != nil {
		t.Fatalf("Failed to send to topic: %v", err)
	}
	time.Sleep(1 * time.Second)

	// Subscribe to topic/subscription
	subscriber := NewSubscriber(client, SubscriberConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 1,
	})
	defer subscriber.Close()

	topicPath := topicName + "/" + subscriptionName
	msgs, err := subscriber.Subscribe(ctx, topicPath)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	select {
	case received := <-msgs:
		t.Logf("Received from topic: %s", string(received.Payload()))
		received.Ack()
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for message from topic")
	}
}

// TestPublisherSubscriber_EndToEnd tests full publish/subscribe flow
func TestPublisherSubscriber_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	queueName := GenerateTestName(t, "test-e2e")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create publisher and subscriber
	publisher := NewPublisher(client, PublisherConfig{
		BatchSize:    5,
		BatchTimeout: 50 * time.Millisecond,
	})
	defer publisher.Close()

	subscriber := NewSubscriber(client, SubscriberConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 10,
		PollInterval:    500 * time.Millisecond,
	})
	defer subscriber.Close()

	// Start subscriber
	msgs, err := subscriber.Subscribe(ctx, queueName)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Track received messages
	var receivedCount atomic.Int32
	messageCount := 20

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for msg := range msgs {
			count := receivedCount.Add(1)
			t.Logf("Received message %d", count)
			msg.Ack()
			if count >= int32(messageCount) {
				return
			}
		}
	}()

	// Give subscriber time to start
	time.Sleep(500 * time.Millisecond)

	// Start publishing
	pubMsgs := make(chan *message.Message[[]byte])
	pubDone, err := publisher.Publish(ctx, queueName, pubMsgs)
	if err != nil {
		t.Fatalf("Failed to start publishing: %v", err)
	}

	// Send messages
	go func() {
		defer close(pubMsgs)
		for i := 1; i <= messageCount; i++ {
			body, _ := json.Marshal(map[string]int{"id": i})
			pubMsgs <- message.New(body)
			time.Sleep(10 * time.Millisecond) // Slight delay between messages
		}
	}()

	// Wait for publishing to complete
	<-pubDone
	t.Log("Publishing complete")

	// Wait for all messages to be received (with timeout)
	done := make(chan struct{})
	go func() {
		for receivedCount.Load() < int32(messageCount) {
			time.Sleep(100 * time.Millisecond)
		}
		close(done)
	}()

	select {
	case <-done:
		t.Logf("Successfully received all %d messages", messageCount)
	case <-time.After(30 * time.Second):
		t.Errorf("Timeout: received %d/%d messages", receivedCount.Load(), messageCount)
	}
}

// TestMultiPublisher_MultipleDestinations tests publishing to multiple destinations
func TestMultiPublisher_MultipleDestinations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create multiple queues
	queue1 := GenerateTestName(t, "test-multi-pub-1")
	queue2 := GenerateTestName(t, "test-multi-pub-2")
	helper.CreateQueue(ctx, queue1)
	helper.CreateQueue(ctx, queue2)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	multiPub := NewMultiPublisher(client, MultiPublisherConfig{
		PublisherConfig: PublisherConfig{
			BatchSize:    5,
			BatchTimeout: 100 * time.Millisecond,
		},
	})
	defer multiPub.Close()

	// Create message channel
	msgs := make(chan *message.Message[[]byte])

	// Router function - route based on message property
	router := func(msg *message.Message[[]byte]) string {
		if dest, ok := msg.Properties().Get("destination"); ok {
			return dest.(string)
		}
		return queue1 // Default
	}

	// Start publishing
	done, err := multiPub.Publish(ctx, msgs, router)
	if err != nil {
		t.Fatalf("Failed to start multi-publishing: %v", err)
	}

	// Send messages to different destinations
	go func() {
		defer close(msgs)
		for i := 0; i < 10; i++ {
			dest := queue1
			if i%2 == 0 {
				dest = queue2
			}
			body, _ := json.Marshal(map[string]any{"id": i, "dest": dest})
			msgs <- message.New(body, message.WithProperty[[]byte]("destination", dest))
		}
	}()

	<-done
	t.Log("Multi-publishing complete")

	// Verify messages in each queue
	time.Sleep(1 * time.Second)

	receiver := NewReceiver(client, ReceiverConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 10,
	})
	defer receiver.Close()

	received1, _ := receiver.Receive(ctx, queue1)
	received2, _ := receiver.Receive(ctx, queue2)

	t.Logf("Queue 1: %d messages, Queue 2: %d messages", len(received1), len(received2))

	// Ack all messages
	for _, msg := range received1 {
		msg.Ack()
	}
	for _, msg := range received2 {
		msg.Ack()
	}
}

// TestMultiSubscriber_MultipleSources tests subscribing to multiple sources
func TestMultiSubscriber_MultipleSources(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create multiple queues
	queue1 := GenerateTestName(t, "test-multi-sub-1")
	queue2 := GenerateTestName(t, "test-multi-sub-2")
	helper.CreateQueue(ctx, queue1)
	helper.CreateQueue(ctx, queue2)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Send messages to both queues
	sender := NewSender(client, SenderConfig{})
	defer sender.Close()

	for i := 0; i < 3; i++ {
		body1, _ := json.Marshal(map[string]any{"queue": "q1", "id": i})
		body2, _ := json.Marshal(map[string]any{"queue": "q2", "id": i})
		sender.Send(ctx, queue1, []*message.Message[[]byte]{message.New(body1)})
		sender.Send(ctx, queue2, []*message.Message[[]byte]{message.New(body2)})
	}
	time.Sleep(1 * time.Second)

	// Subscribe to both queues
	multiSub := NewMultiSubscriber(client, MultiSubscriberConfig{
		SubscriberConfig: SubscriberConfig{
			ReceiveTimeout:  5 * time.Second,
			MaxMessageCount: 5,
			PollInterval:    500 * time.Millisecond,
		},
		MergeBufferSize: 100,
	})
	defer multiSub.Close()

	msgs, err := multiSub.Subscribe(ctx, queue1, queue2)
	if err != nil {
		t.Fatalf("Failed to multi-subscribe: %v", err)
	}

	// Receive messages from merged channel
	received := 0
	timeout := time.After(15 * time.Second)

loop:
	for {
		select {
		case msg, ok := <-msgs:
			if !ok {
				break loop
			}
			received++
			t.Logf("Received message %d: %s", received, string(msg.Payload()))
			msg.Ack()
			if received >= 6 {
				break loop
			}
		case <-timeout:
			break loop
		}
	}

	if received < 6 {
		t.Errorf("Expected at least 6 messages, got %d", received)
	}
}

// TestSubscriber_ContextCancellation tests graceful shutdown on context cancellation
func TestSubscriber_ContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithCancel(context.Background())
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	queueName := GenerateTestName(t, "test-sub-cancel")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(context.Background())

	subscriber := NewSubscriber(client, SubscriberConfig{
		ReceiveTimeout: 2 * time.Second,
		PollInterval:   500 * time.Millisecond,
	})
	defer subscriber.Close()

	msgs, err := subscriber.Subscribe(ctx, queueName)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Cancel context after short delay
	go func() {
		time.Sleep(1 * time.Second)
		cancel()
	}()

	// Wait for channel to close
	timeout := time.After(5 * time.Second)
	select {
	case _, ok := <-msgs:
		if !ok {
			t.Log("Channel closed as expected after context cancellation")
		}
	case <-timeout:
		t.Error("Timeout waiting for channel to close")
	}
}

// TestPublisher_ClosedPublisher tests error handling for closed publisher
func TestPublisher_ClosedPublisher(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(context.Background())

	publisher := NewPublisher(client, PublisherConfig{})

	// Close publisher
	publisher.Close()

	// Try to publish - should still work with Publish (it starts a goroutine)
	// but PublishSync should fail
	body, _ := json.Marshal(map[string]string{"test": "data"})
	err = publisher.PublishSync(context.Background(), "test-queue", []*message.Message[[]byte]{message.New(body)})
	if err == nil {
		t.Error("Expected error when publishing with closed publisher")
	}
}

// TestSubscriber_ClosedSubscriber tests error handling for closed subscriber
func TestSubscriber_ClosedSubscriber(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(context.Background())

	subscriber := NewSubscriber(client, SubscriberConfig{})

	// Close subscriber
	subscriber.Close()

	// Try to subscribe
	_, err = subscriber.Subscribe(context.Background(), "test-queue")
	if err == nil {
		t.Error("Expected error when subscribing with closed subscriber")
	}

	if err != ErrSubscriberClosed {
		t.Errorf("Expected ErrSubscriberClosed, got %v", err)
	}
}

// TestPublisher_BatchingBehavior tests that messages are batched correctly
func TestPublisher_BatchingBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	queueName := GenerateTestName(t, "test-pub-batch")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create publisher with specific batch settings
	publisher := NewPublisher(client, PublisherConfig{
		BatchSize:    3, // Small batch size
		BatchTimeout: 2 * time.Second,
	})
	defer publisher.Close()

	msgs := make(chan *message.Message[[]byte])
	done, _ := publisher.Publish(ctx, queueName, msgs)

	// Send exactly 3 messages (one batch) and close
	go func() {
		defer close(msgs)
		for i := 0; i < 3; i++ {
			body, _ := json.Marshal(map[string]int{"id": i})
			msgs <- message.New(body)
		}
	}()

	<-done
	t.Log("Batch publishing complete")
}
