package azservicebus_test

import (
	gosb "github.com/fxsml/gopipe-azservicebus"
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"


	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus/admin"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
)

var (
	client         *azservicebus.Client
	connString     string
	mgmtConnString string
	testQueue1     string
	testQueue2     string
)

func TestMain(m *testing.M) {
	_ = godotenv.Load(".env.test")

	connString = os.Getenv("SERVICEBUS_CONNECTION")
	mgmtConnString = os.Getenv("SERVICEBUS_CONNECTION_MANAGEMENT")
	testQueue1 = os.Getenv("SERVICEBUS_TEST_QUEUE_1")
	testQueue2 = os.Getenv("SERVICEBUS_TEST_QUEUE_2")

	if connString != "" {
		var err error
		client, err = gosb.NewClient(connString)
		if err != nil {
			fmt.Printf("Failed to create client: %v\n", err)
			os.Exit(1)
		}
	}

	code := m.Run()

	if client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Close(ctx)
	}

	os.Exit(code)
}

func skipIfNoServiceBus(t *testing.T) {
	t.Helper()
	if connString == "" {
		t.Skip("Skipping: SERVICEBUS_CONNECTION not set")
	}
}

func skipIfNoManagement(t *testing.T) {
	t.Helper()
	if mgmtConnString == "" {
		t.Skip("Skipping: SERVICEBUS_CONNECTION_MANAGEMENT not set")
	}
}

// testTopicSetup creates a temporary topic and subscription for testing.
// Returns cleanup function that must be deferred.
func testTopicSetup(t *testing.T, ctx context.Context) (topicName, subName string, cleanup func()) {
	t.Helper()
	skipIfNoServiceBus(t)
	skipIfNoManagement(t)

	adminClient, err := admin.NewClientFromConnectionString(mgmtConnString, nil)
	require.NoError(t, err)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	topicName = fmt.Sprintf("v2-integration-test-%d", rng.Intn(100000))
	subName = "test-sub"

	_, err = adminClient.CreateTopic(ctx, topicName, nil)
	require.NoError(t, err)
	t.Logf("Created topic: %s", topicName)

	_, err = adminClient.CreateSubscription(ctx, topicName, subName, nil)
	require.NoError(t, err)
	t.Logf("Created subscription: %s/%s", topicName, subName)

	cleanup = func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := adminClient.DeleteTopic(cleanupCtx, topicName, nil); err != nil {
			t.Logf("Failed to delete topic %s: %v", topicName, err)
		} else {
			t.Logf("Deleted topic: %s", topicName)
		}
	}

	return topicName, subName, cleanup
}
