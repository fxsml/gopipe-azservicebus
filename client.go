package azservicebus

import (
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
)

// ClientConfig holds configuration options for creating an Azure Service Bus client
type ClientConfig struct {
	// MaxRetries is the maximum number of retry attempts for operations
	// Default: 5
	MaxRetries int32

	// RetryDelay is the initial delay between retry attempts
	// Default: 1 second
	RetryDelay time.Duration

	// MaxRetryDelay is the maximum delay between retry attempts
	// Default: 60 seconds
	MaxRetryDelay time.Duration
}

// setDefaults sets default values for ClientConfig
func (c *ClientConfig) setDefaults() {
	if c.MaxRetries == 0 {
		c.MaxRetries = 5
	}
	if c.RetryDelay == 0 {
		c.RetryDelay = time.Second
	}
	if c.MaxRetryDelay == 0 {
		c.MaxRetryDelay = 60 * time.Second
	}
}

// toAzureOptions converts ClientConfig to Azure SDK options
func (c *ClientConfig) toAzureOptions() *azservicebus.ClientOptions {
	c.setDefaults()
	return &azservicebus.ClientOptions{
		RetryOptions: azservicebus.RetryOptions{
			MaxRetries:    c.MaxRetries,
			RetryDelay:    c.RetryDelay,
			MaxRetryDelay: c.MaxRetryDelay,
		},
	}
}

// NewClient creates a new Azure Service Bus client from either a connection string
// or a namespace using DefaultAzureCredential.
//
// If connOrNamespace starts with "Endpoint=", it's treated as a connection string.
// Otherwise, it's treated as a namespace and DefaultAzureCredential is used for authentication.
//
// Example connection string:
//
//	Endpoint=sb://your-namespace.servicebus.windows.net/;SharedAccessKeyName=...;SharedAccessKey=...
//
// Example namespace:
//
//	your-namespace.servicebus.windows.net
func NewClient(connOrNamespace string) (*azservicebus.Client, error) {
	return NewClientWithConfig(connOrNamespace, ClientConfig{})
}

// NewClientWithConfig creates a new Azure Service Bus client with custom configuration
func NewClientWithConfig(connOrNamespace string, config ClientConfig) (*azservicebus.Client, error) {
	opts := config.toAzureOptions()

	if strings.HasPrefix(connOrNamespace, "Endpoint=") {
		// Connection string authentication
		return azservicebus.NewClientFromConnectionString(connOrNamespace, opts)
	}

	// DefaultAzureCredential authentication (recommended for production)
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DefaultAzureCredential: %w", err)
	}

	return azservicebus.NewClient(connOrNamespace, cred, opts)
}
