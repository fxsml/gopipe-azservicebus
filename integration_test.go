package azservicebus_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	azservicebuspkg "github.com/fxsml/gopipe-azservicebus"
)

// TestMessage is a simple test message structure
type TestMessage struct {
	ID        int       `json:"id"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// TestConnection tests basic connectivity to Azure Service Bus
func TestConnection(t *testing.T) {
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

	t.Log("Successfully connected to Azure Service Bus")
}

// TestPublishToQueue tests publishing messages to a queue using Azure SDK directly
func TestPublishToQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create test queue
	queueName := GenerateTestName(t, "test-queue")
	helper.CreateQueue(ctx, queueName)

	// Create client using Azure SDK directly
	client, err := azservicebus.NewClientFromConnectionString(helper.ConnectionString(), nil)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create sender using Azure SDK directly
	sender, err := client.NewSender(queueName, nil)
	if err != nil {
		t.Fatalf("Failed to create sender: %v", err)
	}
	defer sender.Close(ctx)

	// Create test message
	testMsg := TestMessage{
		ID:        1,
		Content:   "Hello from integration test!",
		Timestamp: time.Now(),
	}

	// Marshal message to JSON
	body, err := json.Marshal(testMsg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	// Create Azure Service Bus message
	messageID := fmt.Sprintf("test-msg-%d", testMsg.ID)
	asbMessage := &azservicebus.Message{
		Body:      body,
		MessageID: &messageID,
		ApplicationProperties: map[string]any{
			"test-property": "test-value",
		},
	}

	// Send message using Azure SDK
	err = sender.SendMessage(ctx, asbMessage, nil)
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	t.Logf("Successfully published message to queue %s", queueName)

	// Verify by receiving the message
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

	// Complete the message
	if err := receiver.CompleteMessage(ctx, msgs[0], nil); err != nil {
		t.Errorf("Failed to complete message: %v", err)
	}

	t.Log("Successfully received and completed message")
}

// TestPublishAndReceive tests the full round-trip of publishing and receiving messages
func TestPublishAndReceive(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create test queue
	queueName := GenerateTestName(t, "test-roundtrip")
	helper.CreateQueue(ctx, queueName)

	// Create client using Azure SDK directly
	client, err := azservicebus.NewClientFromConnectionString(helper.ConnectionString(), nil)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create sender using Azure SDK directly
	sender, err := client.NewSender(queueName, nil)
	if err != nil {
		t.Fatalf("Failed to create sender: %v", err)
	}
	defer sender.Close(ctx)

	// Create test messages
	messageCount := 5
	for i := 1; i <= messageCount; i++ {
		testMsg := TestMessage{
			ID:        i,
			Content:   fmt.Sprintf("Test message #%d", i),
			Timestamp: time.Now(),
		}

		// Marshal message to JSON
		body, err := json.Marshal(testMsg)
		if err != nil {
			t.Fatalf("Failed to marshal message %d: %v", i, err)
		}

		// Create Azure Service Bus message
		messageID := fmt.Sprintf("test-msg-%d", i)
		asbMessage := &azservicebus.Message{
			Body:      body,
			MessageID: &messageID,
			ApplicationProperties: map[string]any{
				"test-run": t.Name(),
			},
		}

		// Send message using Azure SDK
		err = sender.SendMessage(ctx, asbMessage, nil)
		if err != nil {
			t.Fatalf("Failed to send message %d: %v", i, err)
		}
		t.Logf("Message %d published", i)
	}

	t.Logf("Published %d messages to queue %s", messageCount, queueName)

	// Create receiver using Azure SDK directly
	receiver, err := client.NewReceiverForQueue(queueName, nil)
	if err != nil {
		t.Fatalf("Failed to create receiver: %v", err)
	}
	defer receiver.Close(ctx)

	// Receive messages with timeout
	receiveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	receivedCount := 0
	for receivedCount < messageCount {
		messages, err := receiver.ReceiveMessages(receiveCtx, 1, nil)
		if err != nil {
			t.Fatalf("Failed to receive message: %v", err)
		}

		if len(messages) == 0 {
			t.Fatal("No messages received")
		}

		for _, msg := range messages {
			receivedCount++

			// Unmarshal the message
			var testMsg TestMessage
			if err := json.Unmarshal(msg.Body, &testMsg); err != nil {
				t.Errorf("Failed to unmarshal message: %v", err)
				continue
			}

			t.Logf("Received message #%d: ID=%d, Content=%s", receivedCount, testMsg.ID, testMsg.Content)

			// Complete the message
			if err := receiver.CompleteMessage(ctx, msg, nil); err != nil {
				t.Errorf("Failed to complete message: %v", err)
			}
		}
	}

	if receivedCount != messageCount {
		t.Errorf("Expected to receive %d messages, got %d", messageCount, receivedCount)
	}

	t.Logf("Successfully received and processed %d messages", receivedCount)
}

// TestTopicAndSubscription tests publishing to a topic and receiving from a subscription
func TestTopicAndSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create test topic and subscription
	topicName := GenerateTestName(t, "test-topic-integration")
	subscriptionName := "test-subscription"
	helper.CreateTopic(ctx, topicName)
	helper.CreateSubscription(ctx, topicName, subscriptionName)

	// Give Azure some time to propagate
	time.Sleep(2 * time.Second)

	// Create client
	client, err := azservicebus.NewClientFromConnectionString(helper.ConnectionString(), nil)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create sender for topic
	sender, err := client.NewSender(topicName, nil)
	if err != nil {
		t.Fatalf("Failed to create sender: %v", err)
	}
	defer sender.Close(ctx)

	// Publish test message
	testMsg := TestMessage{
		ID:        1,
		Content:   "Hello from topic!",
		Timestamp: time.Now(),
	}

	body, err := json.Marshal(testMsg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	messageID := "topic-msg-1"
	asbMessage := &azservicebus.Message{
		Body:      body,
		MessageID: &messageID,
		ApplicationProperties: map[string]any{
			"test-run": t.Name(),
		},
	}

	err = sender.SendMessage(ctx, asbMessage, nil)
	if err != nil {
		t.Fatalf("Failed to send message to topic: %v", err)
	}

	t.Logf("Published message to topic %s", topicName)

	// Create receiver for subscription
	receiver, err := client.NewReceiverForSubscription(topicName, subscriptionName, nil)
	if err != nil {
		t.Fatalf("Failed to create receiver: %v", err)
	}
	defer receiver.Close(ctx)

	// Receive message
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
	var receivedMsg TestMessage
	if err := json.Unmarshal(msgs[0].Body, &receivedMsg); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	t.Logf("Received message from subscription: ID=%d, Content=%s", receivedMsg.ID, receivedMsg.Content)

	if receivedMsg.ID != testMsg.ID {
		t.Errorf("Expected message ID %d, got %d", testMsg.ID, receivedMsg.ID)
	}

	if receivedMsg.Content != testMsg.Content {
		t.Errorf("Expected content %q, got %q", testMsg.Content, receivedMsg.Content)
	}

	// Complete the message
	if err := receiver.CompleteMessage(ctx, msgs[0], nil); err != nil {
		t.Errorf("Failed to complete message: %v", err)
	}

	t.Log("Successfully completed topic/subscription test")
}

// TestMessageProperties tests sending and receiving messages with various properties
func TestMessageProperties(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create test queue
	queueName := GenerateTestName(t, "test-properties")
	helper.CreateQueue(ctx, queueName)

	// Create client
	client, err := azservicebus.NewClientFromConnectionString(helper.ConnectionString(), nil)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create sender
	sender, err := client.NewSender(queueName, nil)
	if err != nil {
		t.Fatalf("Failed to create sender: %v", err)
	}
	defer sender.Close(ctx)

	// Create message with various properties
	messageID := "msg-with-props"
	subject := "test-subject"
	contentType := "application/json"
	correlationID := "corr-123"

	testMsg := TestMessage{
		ID:        1,
		Content:   "Test message with properties",
		Timestamp: time.Now(),
	}

	body, err := json.Marshal(testMsg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	asbMessage := &azservicebus.Message{
		Body:          body,
		MessageID:     &messageID,
		Subject:       &subject,
		ContentType:   &contentType,
		CorrelationID: &correlationID,
		ApplicationProperties: map[string]any{
			"custom-prop-1": "value1",
			"custom-prop-2": 42,
			"custom-prop-3": true,
		},
	}

	// Send message
	err = sender.SendMessage(ctx, asbMessage, nil)
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	t.Log("Published message with properties")

	// Receive and verify properties
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

	// Verify all properties
	if receivedMsg.MessageID != messageID {
		t.Errorf("Expected MessageID %q, got %v", messageID, receivedMsg.MessageID)
	}

	if receivedMsg.Subject == nil || *receivedMsg.Subject != subject {
		t.Errorf("Expected Subject %q, got %v", subject, receivedMsg.Subject)
	}

	if receivedMsg.ContentType == nil || *receivedMsg.ContentType != contentType {
		t.Errorf("Expected ContentType %q, got %v", contentType, receivedMsg.ContentType)
	}

	if receivedMsg.CorrelationID == nil || *receivedMsg.CorrelationID != correlationID {
		t.Errorf("Expected CorrelationID %q, got %v", correlationID, receivedMsg.CorrelationID)
	}

	if val, ok := receivedMsg.ApplicationProperties["custom-prop-1"]; !ok || val != "value1" {
		t.Errorf("Expected custom-prop-1 'value1', got %v", val)
	}

	if _, ok := receivedMsg.ApplicationProperties["custom-prop-2"]; !ok {
		t.Error("Expected custom-prop-2 to exist")
	}

	if val, ok := receivedMsg.ApplicationProperties["custom-prop-3"]; !ok || val != true {
		t.Errorf("Expected custom-prop-3 true, got %v", val)
	}

	// Complete the message
	if err := receiver.CompleteMessage(ctx, receivedMsg, nil); err != nil {
		t.Errorf("Failed to complete message: %v", err)
	}

	t.Log("Successfully verified all message properties")
}

// TestMessageBatch tests sending messages in a batch
func TestMessageBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	helper := NewTestHelper(t)
	defer helper.Cleanup()

	// Create test queue
	queueName := GenerateTestName(t, "test-batch-integration")
	helper.CreateQueue(ctx, queueName)

	// Create client
	client, err := azservicebus.NewClientFromConnectionString(helper.ConnectionString(), nil)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create sender
	sender, err := client.NewSender(queueName, nil)
	if err != nil {
		t.Fatalf("Failed to create sender: %v", err)
	}
	defer sender.Close(ctx)

	// Create a batch
	batch, err := sender.NewMessageBatch(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to create message batch: %v", err)
	}

	// Add multiple messages to the batch
	messageCount := 10
	for i := 1; i <= messageCount; i++ {
		testMsg := TestMessage{
			ID:        i,
			Content:   fmt.Sprintf("Batch message #%d", i),
			Timestamp: time.Now(),
		}

		body, err := json.Marshal(testMsg)
		if err != nil {
			t.Fatalf("Failed to marshal message %d: %v", i, err)
		}

		messageID := fmt.Sprintf("batch-msg-%d", i)
		asbMessage := &azservicebus.Message{
			Body:      body,
			MessageID: &messageID,
		}

		err = batch.AddMessage(asbMessage, nil)
		if err != nil {
			t.Fatalf("Failed to add message %d to batch: %v", i, err)
		}
	}

	// Send the batch
	err = sender.SendMessageBatch(ctx, batch, nil)
	if err != nil {
		t.Fatalf("Failed to send message batch: %v", err)
	}

	t.Logf("Sent batch of %d messages", messageCount)

	// Receive and verify all messages
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

	t.Logf("Successfully received all %d messages from batch", receivedCount)
}
