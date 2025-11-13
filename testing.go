package azservicebus

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
)

// getTestConnectionString returns the connection string for testing.
// Priority order:
// 1. AZURE_SERVICEBUS_CONNECTION_STRING environment variable
// 2. AZURE_SERVICEBUS_NAMESPACE environment variable
// 3. Default to Azure Service Bus Emulator
func getTestConnectionString(t *testing.T) string {
	t.Helper()

	// Check for explicit connection string
	if connString := os.Getenv("AZURE_SERVICEBUS_CONNECTION_STRING"); connString != "" {
		t.Logf("Using connection string from AZURE_SERVICEBUS_CONNECTION_STRING")
		return connString
	}

	// Check for namespace (uses DefaultAzureCredential)
	if namespace := os.Getenv("AZURE_SERVICEBUS_NAMESPACE"); namespace != "" {
		t.Logf("Using namespace from AZURE_SERVICEBUS_NAMESPACE: %s", namespace)
		return namespace
	}

	// Default to emulator
	emulatorConnString := "Endpoint=sb://localhost;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true;"
	t.Logf("Using Azure Service Bus Emulator")
	return emulatorConnString
}

// createTestClient creates a test Service Bus client
func createTestClient(t *testing.T) *azservicebus.Client {
	t.Helper()

	connString := getTestConnectionString(t)
	client, err := NewClient(connString)
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Close(ctx); err != nil {
			t.Logf("Warning: failed to close test client: %v", err)
		}
	})

	return client
}

// createTestSubscriber creates a test subscriber with default config
func createTestSubscriber[T any](t *testing.T, client *azservicebus.Client) *Subscriber[T] {
	t.Helper()

	config := DefaultSubscriberConfig()
	config.MessageTimeout = 30 * time.Second
	config.CompleteTimeout = 10 * time.Second
	config.AbandonTimeout = 10 * time.Second

	subscriber := NewSubscriber[T](client, config)

	t.Cleanup(func() {
		if err := subscriber.Close(); err != nil {
			t.Logf("Warning: failed to close test subscriber: %v", err)
		}
	})

	return subscriber
}

// createTestPublisher creates a test publisher with default config
func createTestPublisher[T any](t *testing.T, client *azservicebus.Client) *Publisher[T] {
	t.Helper()

	config := DefaultPublisherConfig()
	config.PublishTimeout = 30 * time.Second

	publisher := NewPublisher[T](client, config)

	t.Cleanup(func() {
		if err := publisher.Close(); err != nil {
			t.Logf("Warning: failed to close test publisher: %v", err)
		}
	})

	return publisher
}

// waitForMessages waits for expected number of messages with timeout
func waitForMessages[T any](t *testing.T, msgChan <-chan *T, expectedCount int, timeout time.Duration) []*T {
	t.Helper()

	var messages []*T
	timeoutCh := time.After(timeout)

	for len(messages) < expectedCount {
		select {
		case msg := <-msgChan:
			if msg == nil {
				t.Fatal("Received nil message")
			}
			messages = append(messages, msg)
		case <-timeoutCh:
			t.Fatalf("Timeout waiting for messages. Expected %d, got %d", expectedCount, len(messages))
		}
	}

	return messages
}
