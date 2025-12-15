package azservicebus

import (
	"time"

	sdk "github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe/message"
)

// Publisher wraps the Sender with gopipe's Publisher for channel-based message publishing.
// It uses gopipe's built-in message.NewPublisher for batching and publishing.
type Publisher = message.Publisher

// PublisherConfig configures the Publisher behavior.
type PublisherConfig = message.PublisherConfig

// NewPublisher creates a new Publisher for Azure Service Bus.
// It wraps the Sender with gopipe's message.Publisher for efficient batching and publishing.
//
// Example:
//
//	sender := azservicebus.NewSender(client, azservicebus.SenderConfig{})
//	publisher := azservicebus.NewPublisher(sender, azservicebus.PublisherConfig{
//	    MaxBatchSize: 10,
//	    MaxDuration:  100 * time.Millisecond,
//	})
//
//	msgs := make(chan *message.Message)
//	done := publisher.Publish(ctx, "my-queue", msgs)
func NewPublisher(sender *Sender, config PublisherConfig) *Publisher {
	return message.NewPublisher(sender, config)
}

// NewPublisherFromClient creates a new Publisher directly from an Azure Service Bus client.
// This is a convenience function that creates a Sender internally.
//
// Example:
//
//	publisher := azservicebus.NewPublisherFromClient(client, azservicebus.SenderConfig{}, azservicebus.PublisherConfig{
//	    MaxBatchSize: 10,
//	    MaxDuration:  100 * time.Millisecond,
//	})
func NewPublisherFromClient(client *sdk.Client, senderConfig SenderConfig, publisherConfig PublisherConfig) *Publisher {
	sender := NewSender(client, senderConfig)
	return message.NewPublisher(sender, publisherConfig)
}

// DefaultPublisherConfig returns a default PublisherConfig with sensible defaults.
func DefaultPublisherConfig() PublisherConfig {
	return PublisherConfig{
		MaxBatchSize: 10,
		MaxDuration:  100 * time.Millisecond,
		Concurrency:  1,
	}
}
