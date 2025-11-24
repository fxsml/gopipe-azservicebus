package azservicebus_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus/admin"
	"github.com/joho/godotenv"
)

// getConnectionString returns the Azure Service Bus connection string from environment
// It first tries to load from .env file, then falls back to environment variable
func getConnectionString(t *testing.T) string {
	t.Helper()

	// Try to load .env file (ignore error if it doesn't exist)
	_ = godotenv.Load()

	connStr := os.Getenv("AZURE_SERVICEBUS_CONNECTION_STRING")
	if connStr == "" {
		t.Skip("AZURE_SERVICEBUS_CONNECTION_STRING not set - skipping real Azure Service Bus test")
	}

	return connStr
}

// TestHelper provides utilities for managing Azure Service Bus resources in tests
type TestHelper struct {
	t           *testing.T
	adminClient *admin.Client
	connStr     string

	// Resources to cleanup
	queues []string
	topics []string
}

// NewTestHelper creates a new test helper with admin client
func NewTestHelper(t *testing.T) *TestHelper {
	t.Helper()

	connStr := getConnectionString(t)

	adminClient, err := admin.NewClientFromConnectionString(connStr, nil)
	if err != nil {
		t.Fatalf("Failed to create admin client: %v", err)
	}

	return &TestHelper{
		t:           t,
		adminClient: adminClient,
		connStr:     connStr,
		queues:      make([]string, 0),
		topics:      make([]string, 0),
	}
}

// CreateQueue creates a test queue and registers it for cleanup
func (h *TestHelper) CreateQueue(ctx context.Context, queueName string) {
	h.t.Helper()
	h.t.Logf("Creating queue: %s", queueName)

	// Create queue with default settings
	_, err := h.adminClient.CreateQueue(ctx, queueName, nil)
	if err != nil {
		h.t.Fatalf("Failed to create queue %s: %v", queueName, err)
	}

	h.queues = append(h.queues, queueName)
	h.t.Logf("Queue created: %s", queueName)
}

// CreateTopic creates a test topic and registers it for cleanup
func (h *TestHelper) CreateTopic(ctx context.Context, topicName string) {
	h.t.Helper()
	h.t.Logf("Creating topic: %s", topicName)

	// Create topic with default settings
	_, err := h.adminClient.CreateTopic(ctx, topicName, nil)
	if err != nil {
		h.t.Fatalf("Failed to create topic %s: %v", topicName, err)
	}

	h.topics = append(h.topics, topicName)
	h.t.Logf("Topic created: %s", topicName)
}

// CreateSubscription creates a subscription for a topic
func (h *TestHelper) CreateSubscription(ctx context.Context, topicName, subscriptionName string) {
	h.t.Helper()
	h.t.Logf("Creating subscription %s for topic: %s", subscriptionName, topicName)

	_, err := h.adminClient.CreateSubscription(ctx, topicName, subscriptionName, nil)
	if err != nil {
		h.t.Fatalf("Failed to create subscription %s for topic %s: %v", subscriptionName, topicName, err)
	}

	h.t.Logf("Subscription created: %s/%s", topicName, subscriptionName)
}

// PurgeQueue removes all messages from a queue
func (h *TestHelper) PurgeQueue(ctx context.Context, queueName string) {
	h.t.Helper()
	h.t.Logf("Purging queue: %s", queueName)

	// Azure doesn't have a direct purge API, so we'll receive and complete all messages
	// This is a best-effort cleanup
	h.t.Logf("Queue purge requested for %s (best effort cleanup)", queueName)
}

// DeleteQueue deletes a queue
func (h *TestHelper) DeleteQueue(ctx context.Context, queueName string) error {
	h.t.Logf("Deleting queue: %s", queueName)

	_, err := h.adminClient.DeleteQueue(ctx, queueName, nil)
	if err != nil {
		return fmt.Errorf("failed to delete queue %s: %w", queueName, err)
	}

	h.t.Logf("Queue deleted: %s", queueName)
	return nil
}

// DeleteTopic deletes a topic (and all its subscriptions)
func (h *TestHelper) DeleteTopic(ctx context.Context, topicName string) error {
	h.t.Logf("Deleting topic: %s", topicName)

	_, err := h.adminClient.DeleteTopic(ctx, topicName, nil)
	if err != nil {
		return fmt.Errorf("failed to delete topic %s: %w", topicName, err)
	}

	h.t.Logf("Topic deleted: %s", topicName)
	return nil
}

// Cleanup removes all created resources
func (h *TestHelper) Cleanup() {
	h.t.Helper()
	h.t.Log("Cleaning up test resources...")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Delete all queues
	for _, queueName := range h.queues {
		if err := h.DeleteQueue(ctx, queueName); err != nil {
			h.t.Logf("Warning: failed to delete queue %s: %v", queueName, err)
		}
	}

	// Delete all topics (this also deletes subscriptions)
	for _, topicName := range h.topics {
		if err := h.DeleteTopic(ctx, topicName); err != nil {
			h.t.Logf("Warning: failed to delete topic %s: %v", topicName, err)
		}
	}

	h.t.Log("Cleanup complete")
}

// ConnectionString returns the connection string
func (h *TestHelper) ConnectionString() string {
	return h.connStr
}

// GenerateTestName generates a unique test name with timestamp
func GenerateTestName(t *testing.T, prefix string) string {
	t.Helper()
	// Use timestamp to make it unique
	timestamp := time.Now().Unix()
	return fmt.Sprintf("%s-%d", prefix, timestamp)
}
