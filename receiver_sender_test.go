package azservicebus

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/fxsml/gopipe/message"
	_ "github.com/joho/godotenv/autoload"
)

// TestSenderAndReceiver_EndToEnd tests the full sender and receiver workflow
func TestSenderAndReceiver_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create a test queue
	queueName := GenerateTestName(t, "test-sender-receiver")
	helper.CreateQueue(ctx, queueName)

	// Create client
	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create sender
	sender := NewSender(client, SenderConfig{
		SendTimeout: 30 * time.Second,
	})
	defer sender.Close()

	// Create receiver
	receiver := NewReceiver(client, ReceiverConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 5,
	})
	defer receiver.Close()

	// Send test messages
	messageCount := 5
	messages := make([]*message.Message[[]byte], 0, messageCount)
	for i := 1; i <= messageCount; i++ {
		// Marshal to JSON
		payload := map[string]any{
			"id":      i,
			"content": fmt.Sprintf("Test message #%d", i),
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("Failed to marshal payload: %v", err)
		}

		msg := message.New(
			body,
			message.WithProperty[[]byte]("message_index", fmt.Sprintf("%d", i)),
		)
		messages = append(messages, msg)
	}

	// Send messages
	err = sender.Send(ctx, queueName, messages)
	if err != nil {
		t.Fatalf("Failed to send messages: %v", err)
	}
	t.Logf("Successfully sent %d messages", messageCount)

	// Give Azure some time to propagate
	time.Sleep(1 * time.Second)

	// Receive messages
	receivedMessages, err := receiver.Receive(ctx, queueName)
	if err != nil {
		t.Fatalf("Failed to receive messages: %v", err)
	}

	if len(receivedMessages) != messageCount {
		t.Errorf("Expected to receive %d messages, got %d", messageCount, len(receivedMessages))
	}

	// Verify and acknowledge messages
	for i, msg := range receivedMessages {
		// Unmarshal payload
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
			t.Errorf("Failed to unmarshal payload: %v", err)
			msg.Nack(fmt.Errorf("unmarshal failed"))
			continue
		}

		t.Logf("Received message %d: %v", i+1, payload)

		// Acknowledge the message
		if !msg.Ack() {
			t.Errorf("Failed to ack message")
		}
	}
}

// TestSender_MultipleQueues tests sending to multiple queues
func TestSender_MultipleQueues(t *testing.T) {
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
		queueName := GenerateTestName(t, fmt.Sprintf("test-multi-queue-%d", i))
		helper.CreateQueue(ctx, queueName)
		queues[i] = queueName
	}

	// Create client and sender
	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	sender := NewSender(client, SenderConfig{
		SendTimeout: 30 * time.Second,
	})
	defer sender.Close()

	// Send messages to all queues
	for i, queueName := range queues {
		payload := map[string]any{
			"queue_index": i,
			"message":     fmt.Sprintf("Message for queue %d", i),
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("Failed to marshal payload: %v", err)
		}
		msg := message.New(body)

		err = sender.Send(ctx, queueName, []*message.Message[[]byte]{msg})
		if err != nil {
			t.Fatalf("Failed to send to queue %s: %v", queueName, err)
		}
	}

	t.Logf("Successfully sent to %d queues", queueCount)
}

// TestReceiver_MessageMetadata tests that metadata is properly mapped
func TestReceiver_MessageMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create a test queue
	queueName := GenerateTestName(t, "test-metadata")
	helper.CreateQueue(ctx, queueName)

	// Create client
	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create sender and send message with metadata
	sender := NewSender(client, SenderConfig{
		SendTimeout: 30 * time.Second,
	})
	defer sender.Close()

	messageID := "test-msg-789"
	payload := map[string]string{"data": "test"}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}
	msg := message.New(
		body,
		message.WithProperty[[]byte]("message_id", messageID),
		message.WithProperty[[]byte]("subject", "test-subject"),
		message.WithProperty[[]byte]("content_type", "application/json"),
		message.WithProperty[[]byte]("correlation_id", "corr-789"),
		message.WithProperty[[]byte]("custom_prop", "custom_value"),
	)

	err = sender.Send(ctx, queueName, []*message.Message[[]byte]{msg})
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	// Give Azure some time to propagate
	time.Sleep(1 * time.Second)

	// Receive and verify metadata
	receiver := NewReceiver(client, ReceiverConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 1,
	})
	defer receiver.Close()

	receivedMessages, err := receiver.Receive(ctx, queueName)
	if err != nil {
		t.Fatalf("Failed to receive message: %v", err)
	}

	if len(receivedMessages) == 0 {
		t.Fatal("No messages received")
	}

	receivedMsg := receivedMessages[0]

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

	if val, ok := receivedMsg.Properties().Get("correlation_id"); !ok || val != "corr-789" {
		t.Errorf("Expected correlation_id 'corr-789', got %v", val)
	}

	if val, ok := receivedMsg.Properties().Get("custom_prop"); !ok || val != "custom_value" {
		t.Errorf("Expected custom_prop 'custom_value', got %v", val)
	}

	// Acknowledge the message
	receivedMsg.Ack()
}

// TestSender_WithTopic tests sending to a topic with subscription
func TestSender_WithTopic(t *testing.T) {
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

	// Create client
	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create sender and send to topic
	sender := NewSender(client, SenderConfig{
		SendTimeout: 30 * time.Second,
	})
	defer sender.Close()

	sendPayload := map[string]any{
		"message": "Hello from topic test!",
	}
	body, err := json.Marshal(sendPayload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}
	msg := message.New(body)

	err = sender.Send(ctx, topicName, []*message.Message[[]byte]{msg})
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	t.Log("Successfully sent message to topic")

	// Give Azure some time to propagate
	time.Sleep(1 * time.Second)

	// Create receiver for the topic subscription
	receiver := NewReceiver(client, ReceiverConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 1,
	})
	defer receiver.Close()

	topicPath := topicName + "/" + subscriptionName
	receivedMessages, err := receiver.Receive(ctx, topicPath)
	if err != nil {
		t.Fatalf("Failed to receive message from topic: %v", err)
	}

	if len(receivedMessages) == 0 {
		t.Fatal("No messages received from topic")
	}

	var receivedPayload map[string]any
	if err := json.Unmarshal(receivedMessages[0].Payload(), &receivedPayload); err != nil {
		t.Fatalf("Failed to unmarshal payload: %v", err)
	}

	t.Logf("Received message from topic: %v", receivedPayload)
	receivedMessages[0].Ack()
}

// TestSender_ClosedSenderReturnsError tests that sending with a closed sender returns an error
func TestSender_ClosedSenderReturnsError(t *testing.T) {
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

	sender := NewSender(client, SenderConfig{})

	// Close the sender
	if err := sender.Close(); err != nil {
		t.Fatalf("Failed to close sender: %v", err)
	}

	// Try to send after closing
	payload := map[string]string{"test": "data"}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}
	msg := message.New(body)

	err = sender.Send(ctx, "test-queue", []*message.Message[[]byte]{msg})
	if err == nil {
		t.Error("Expected error when sending with closed sender, got nil")
	}

	expectedErrMsg := "sender is closed"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message %q, got %q", expectedErrMsg, err.Error())
	}
}

// TestReceiver_ClosedReceiverReturnsError tests that receiving with a closed receiver returns an error
func TestReceiver_ClosedReceiverReturnsError(t *testing.T) {
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

	receiver := NewReceiver(client, ReceiverConfig{})

	// Close the receiver
	if err := receiver.Close(); err != nil {
		t.Fatalf("Failed to close receiver: %v", err)
	}

	// Try to receive after closing
	_, err = receiver.Receive(ctx, "test-queue")
	if err == nil {
		t.Error("Expected error when receiving with closed receiver, got nil")
	}

	expectedErrMsg := "receiver is closed"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message %q, got %q", expectedErrMsg, err.Error())
	}
}
