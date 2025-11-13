package azservicebus

import (
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
)

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
	if strings.HasPrefix(connOrNamespace, "Endpoint=") {
		// Connection string authentication
		return azservicebus.NewClientFromConnectionString(connOrNamespace, nil)
	}

	// DefaultAzureCredential authentication (recommended for production)
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DefaultAzureCredential: %w", err)
	}

	return azservicebus.NewClient(connOrNamespace, cred, nil)
}
