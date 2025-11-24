package azservicebus_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/fxsml/gopipe"
	azservicebus_lib "github.com/fxsml/gopipe-azservicebus"
	"github.com/fxsml/gopipe/channel"
)

const (
	// Azure Service Bus Emulator connection string
	// This is the default connection string for the local emulator
	emulatorConnectionString = "Endpoint=sb://localhost;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=PLACEHOLDER_KEY;UseDevelopmentEmulator=true"

	// Test queue from config.json
	testQueue = "test-queue-001"
)

// getEmulatorConnectionString returns the connection string for the emulator
// It can be overridden via SERVICEBUS_CONNECTION_STRING environment variable
func getEmulatorConnectionString() string {
	if connStr := os.Getenv("SERVICEBUS_CONNECTION_STRING"); connStr != "" {
		return connStr
	}
	return emulatorConnectionString
}

// TestMessage is a simple test message structure
type TestMessage struct {
	ID        int       `json:"id"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// TestEmulatorConnection tests basic connectivity to the Service Bus emulator
func TestEmulatorConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	connStr := getEmulatorConnectionString()
	client, err := azservicebus_lib.NewClient(connStr)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(context.Background())

	t.Log("Successfully connected to Service Bus emulator")
}

// TestPublishToQueue tests publishing messages to a queue
func TestPublishToQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := getEmulatorConnectionString()

	// Create client
	client, err := azservicebus_lib.NewClient(connStr)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create publisher
	publisher := azservicebus_lib.NewPublisher(client, azservicebus_lib.PublisherConfig{
		PublishTimeout: 10 * time.Second,
	})
	defer publisher.Close()

	// Create test message
	testMsg := TestMessage{
		ID:        1,
		Content:   "Hello from emulator test!",
		Timestamp: time.Now(),
	}

	// Create gopipe message
	msg := &gopipe.Message[any]{
		Payload: testMsg,
	}
	msg.Properties().Set("test-property", "test-value")
	msg.Properties().Set("message_id", fmt.Sprintf("test-msg-%d", testMsg.ID))

	// Publish message
	err = publisher.Publish(testQueue, channel.FromValues(msg))
	if err != nil {
		t.Fatalf("Failed to publish message: %v", err)
	}

	t.Logf("Successfully published message to queue %s", testQueue)
}

// TestPublishAndReceive tests the full round-trip of publishing and receiving messages
func TestPublishAndReceive(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := getEmulatorConnectionString()

	// Create client
	client, err := azservicebus_lib.NewClient(connStr)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// Create publisher
	publisher := azservicebus_lib.NewPublisher(client, azservicebus_lib.PublisherConfig{
		PublishTimeout: 10 * time.Second,
	})
	defer publisher.Close()

	// Create test messages
	messageCount := 3
	for i := 1; i <= messageCount; i++ {
		testMsg := TestMessage{
			ID:        i,
			Content:   fmt.Sprintf("Test message #%d", i),
			Timestamp: time.Now(),
		}

		msg := &gopipe.Message[any]{
			Payload: testMsg,
		}
		msg.Properties().Set("message_id", fmt.Sprintf("test-msg-%d", i))
		msg.Properties().Set("test-run", t.Name())

		// Publish message
		err = publisher.Publish(testQueue, channel.FromValues(msg))
		if err != nil {
			t.Fatalf("Failed to publish message %d: %v", i, err)
		}
	}

	t.Logf("Published %d messages to queue %s", messageCount, testQueue)

	// Create receiver to verify messages were sent
	receiver, err := client.NewReceiverForQueue(testQueue, nil)
	if err != nil {
		t.Fatalf("Failed to create receiver: %v", err)
	}
	defer receiver.Close(ctx)

	// Receive messages with timeout
	receiveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	receivedCount := 0
	for i := 0; i < messageCount; i++ {
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

// TestEmulatorHealthCheck verifies the emulator is running and healthy
func TestEmulatorHealthCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	connStr := getEmulatorConnectionString()

	// Try to create a client with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := azservicebus_lib.NewClient(connStr)
	if err != nil {
		t.Fatalf("Emulator health check failed - could not create client: %v\n"+
			"Make sure the emulator is running with: docker compose up -d", err)
	}
	defer client.Close(ctx)

	// Try to create a sender to verify the queue exists
	sender, err := client.NewSender(testQueue, nil)
	if err != nil {
		t.Fatalf("Emulator health check failed - could not create sender for queue %s: %v\n"+
			"Make sure the queue is configured in config/config.json", testQueue, err)
	}
	defer sender.Close(ctx)

	t.Log("Emulator health check passed - emulator is running and configured correctly")
}
