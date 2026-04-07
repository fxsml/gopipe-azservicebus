package azservicebus

import (
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
)

// NewClient creates a new Azure Service Bus client.
// If connOrNamespace starts with "Endpoint=", it is treated as a connection string.
// Otherwise, it is treated as a namespace and DefaultAzureCredential is used.
func NewClient(connOrNamespace string) (*azservicebus.Client, error) {
	// Configure retry options - low retries for faster error visibility
	// Throttling (50009) will surface quickly instead of timing out after many retries
	retryOpts := azservicebus.RetryOptions{
		MaxRetries:    2,
		RetryDelay:    500 * time.Millisecond,
		MaxRetryDelay: 4 * time.Second,
	}

	clientOpts := &azservicebus.ClientOptions{
		RetryOptions: retryOpts,
	}

	if strings.HasPrefix(connOrNamespace, "Endpoint=") {
		// Connection string
		client, err := azservicebus.NewClientFromConnectionString(connOrNamespace, clientOpts)
		if err != nil {
			return nil, fmt.Errorf("servicebus: new client from connection string: %w", err)
		}
		return client, nil
	}

	// Namespace with DefaultAzureCredential
	namespace := connOrNamespace
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("servicebus: new default azure credential: %w", err)
	}
	client, err := azservicebus.NewClient(namespace, cred, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("servicebus: new client: %w", err)
	}
	return client, nil
}

// cloudEventsPrefix is the AMQP application-property prefix for CloudEvents attributes.
// See: https://github.com/cloudevents/spec/blob/v1.0.2/cloudevents/bindings/amqp-protocol-binding.md
const cloudEventsPrefix = "cloudEvents:"
