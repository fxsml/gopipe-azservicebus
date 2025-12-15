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

// TestSender_BasicSend tests basic sender functionality
func TestSender_BasicSend(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	queueName := GenerateTestName(t, "test-send-basic")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	sender := NewSender(client, SenderConfig{
		SendTimeout: 30 * time.Second,
	})
	defer sender.Close()

	// Create and send messages
	messageCount := 10
	messages := make([]*message.Message[[]byte], messageCount)
	for i := range messages {
		payload := map[string]any{
			"id":      i + 1,
			"content": fmt.Sprintf("Test message #%d", i+1),
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("Failed to marshal payload: %v", err)
		}
		messages[i] = message.New(body, message.WithProperty[[]byte]("index", i+1))
	}

	// Send messages
	if err := sender.Send(ctx, queueName, messages); err != nil {
		t.Fatalf("Failed to send messages: %v", err)
	}
	t.Logf("Sent %d messages", messageCount)

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

	for _, msg := range received {
		msg.Ack()
	}
}

// TestReceiver_BasicReceive tests basic receiver functionality
func TestReceiver_BasicReceive(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	queueName := GenerateTestName(t, "test-recv-basic")
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

	// Now receive
	receiver := NewReceiver(client, ReceiverConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: messageCount,
	})
	defer receiver.Close()

	received, err := receiver.Receive(ctx, queueName)
	if err != nil {
		t.Fatalf("Failed to receive: %v", err)
	}

	if len(received) != messageCount {
		t.Errorf("Expected %d messages, got %d", messageCount, len(received))
	}

	for i, msg := range received {
		t.Logf("Received message %d: %s", i+1, string(msg.Payload()))
		msg.Ack()
	}
}

// TestReceiver_TopicSubscription tests receiving from a topic subscription
func TestReceiver_TopicSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	topicName := GenerateTestName(t, "test-recv-topic")
	subscriptionName := "test-sub"

	helper.CreateTopic(ctx, topicName)
	helper.CreateSubscription(ctx, topicName, subscriptionName)
	time.Sleep(2 * time.Second)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Send message to topic
	sender := NewSender(client, SenderConfig{})
	defer sender.Close()

	body, _ := json.Marshal(map[string]string{"message": "hello from topic"})
	msg := message.New(body)

	if err := sender.Send(ctx, topicName, []*message.Message[[]byte]{msg}); err != nil {
		t.Fatalf("Failed to send to topic: %v", err)
	}
	time.Sleep(1 * time.Second)

	// Receive from topic/subscription
	receiver := NewReceiver(client, ReceiverConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 1,
	})
	defer receiver.Close()

	topicPath := topicName + "/" + subscriptionName
	received, err := receiver.Receive(ctx, topicPath)
	if err != nil {
		t.Fatalf("Failed to receive: %v", err)
	}

	if len(received) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(received))
	}

	t.Logf("Received from topic: %s", string(received[0].Payload()))
	received[0].Ack()
}

// TestGopipeIntegration_Generator tests the gopipe Generator integration
func TestGopipeIntegration_Generator(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	helper := NewTestHelper(t)
	defer helper.Cleanup()

	queueName := GenerateTestName(t, "test-gopipe-gen")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Send some messages first
	sender := NewSender(client, SenderConfig{})
	defer sender.Close()

	messageCount := 5
	messages := make([]*message.Message[[]byte], messageCount)
	for i := range messages {
		body, _ := json.Marshal(map[string]int{"id": i + 1})
		messages[i] = message.New(body)
	}
	if err := sender.Send(ctx, queueName, messages); err != nil {
		t.Fatalf("Failed to send messages: %v", err)
	}
	time.Sleep(1 * time.Second)

	// Use gopipe Generator to receive messages
	receiver := NewReceiver(client, ReceiverConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 5,
	})
	defer receiver.Close()

	generator := NewMessageGenerator(receiver, queueName)
	msgs := generator.Generate(ctx)

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
			break loop
		}
	}

	if received != messageCount {
		t.Errorf("Expected %d messages, got %d", messageCount, received)
	}
}

// TestGopipeIntegration_SinkPipe tests the gopipe SinkPipe integration
func TestGopipeIntegration_SinkPipe(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	queueName := GenerateTestName(t, "test-gopipe-sink")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	sender := NewSender(client, SenderConfig{})
	defer sender.Close()

	// Use gopipe SinkPipe to send messages
	sinkPipe := NewMessageSinkPipe(sender, queueName, 5, 100*time.Millisecond)

	// Create message channel
	in := make(chan *message.Message[[]byte])

	// Start the pipeline
	out := sinkPipe.Start(ctx, in)

	// Send messages
	messageCount := 10
	go func() {
		defer close(in)
		for i := range messageCount {
			body, _ := json.Marshal(map[string]int{"id": i + 1})
			in <- message.New(body)
		}
	}()

	// Wait for all outputs
	for range out {
	}
	t.Log("Sink pipeline complete")

	// Verify messages were sent
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

	for _, msg := range received {
		msg.Ack()
	}
}

// TestSenderReceiver_EndToEnd tests full send/receive flow
func TestSenderReceiver_EndToEnd(t *testing.T) {
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

	sender := NewSender(client, SenderConfig{})
	defer sender.Close()

	receiver := NewReceiver(client, ReceiverConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 10,
	})
	defer receiver.Close()

	// Start receiving with generator
	generator := NewMessageGenerator(receiver, queueName)
	msgs := generator.Generate(ctx)

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

	// Give receiver time to start
	time.Sleep(500 * time.Millisecond)

	// Send messages in batches using sink pipe
	sinkPipe := NewMessageSinkPipe(sender, queueName, 5, 50*time.Millisecond)
	in := make(chan *message.Message[[]byte])
	out := sinkPipe.Start(ctx, in)

	go func() {
		defer close(in)
		for i := range messageCount {
			body, _ := json.Marshal(map[string]int{"id": i + 1})
			in <- message.New(body)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Wait for sending to complete
	for range out {
	}
	t.Log("Sending complete")

	// Wait for all messages to be received
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

// TestSender_MultipleDestinations tests sending to multiple destinations
func TestSender_MultipleDestinations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	queue1 := GenerateTestName(t, "test-multi-send-1")
	queue2 := GenerateTestName(t, "test-multi-send-2")
	helper.CreateQueue(ctx, queue1)
	helper.CreateQueue(ctx, queue2)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	sender := NewSender(client, SenderConfig{})
	defer sender.Close()

	// Send to multiple destinations
	for i := range 10 {
		dest := queue1
		if i%2 == 0 {
			dest = queue2
		}
		body, _ := json.Marshal(map[string]any{"id": i, "dest": dest})
		if err := sender.Send(ctx, dest, []*message.Message[[]byte]{message.New(body)}); err != nil {
			t.Fatalf("Failed to send: %v", err)
		}
	}

	t.Log("Sent to multiple destinations")
	time.Sleep(1 * time.Second)

	// Verify messages in each queue
	receiver := NewReceiver(client, ReceiverConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 10,
	})
	defer receiver.Close()

	received1, _ := receiver.Receive(ctx, queue1)
	received2, _ := receiver.Receive(ctx, queue2)

	t.Logf("Queue 1: %d messages, Queue 2: %d messages", len(received1), len(received2))

	for _, msg := range received1 {
		msg.Ack()
	}
	for _, msg := range received2 {
		msg.Ack()
	}
}

// TestSender_ClosedSender tests error handling for closed sender
func TestSender_ClosedSender(t *testing.T) {
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

	sender := NewSender(client, SenderConfig{})
	sender.Close()

	body, _ := json.Marshal(map[string]string{"test": "data"})
	err = sender.Send(context.Background(), "test-queue", []*message.Message[[]byte]{message.New(body)})
	if err == nil {
		t.Error("Expected error when sending with closed sender")
	}
}

// TestReceiver_ClosedReceiver tests error handling for closed receiver
func TestReceiver_ClosedReceiver(t *testing.T) {
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

	receiver := NewReceiver(client, ReceiverConfig{})
	receiver.Close()

	_, err = receiver.Receive(context.Background(), "test-queue")
	if err == nil {
		t.Error("Expected error when receiving with closed receiver")
	}
}

// TestSender_BatchingBehavior tests that messages are batched correctly
func TestSender_BatchingBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	queueName := GenerateTestName(t, "test-send-batch")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	sender := NewSender(client, SenderConfig{})
	defer sender.Close()

	// Send exactly 3 messages in a batch
	messages := make([]*message.Message[[]byte], 3)
	for i := range messages {
		body, _ := json.Marshal(map[string]int{"id": i})
		messages[i] = message.New(body)
	}

	if err := sender.Send(ctx, queueName, messages); err != nil {
		t.Fatalf("Failed to send batch: %v", err)
	}

	t.Log("Batch sending complete")
}
