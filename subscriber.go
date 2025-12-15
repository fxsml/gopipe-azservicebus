package azservicebus

import (
	sdk "github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe/message"
)

// Subscriber wraps the Receiver with gopipe's Subscriber for channel-based message subscription.
// It uses gopipe's built-in message.NewSubscriber for continuous polling.
type Subscriber = message.Subscriber

// SubscriberConfig configures the Subscriber behavior.
type SubscriberConfig = message.SubscriberConfig

// NewSubscriber creates a new Subscriber for Azure Service Bus.
// It wraps the Receiver with gopipe's message.Subscriber for continuous message polling.
//
// Example:
//
//	receiver := azservicebus.NewReceiver(client, azservicebus.ReceiverConfig{})
//	subscriber := azservicebus.NewSubscriber(receiver, azservicebus.SubscriberConfig{
//	    Concurrency: 1,
//	})
//
//	msgs, err := subscriber.Subscribe(ctx, "my-queue")
//	for msg := range msgs {
//	    // process message
//	    msg.Ack()
//	}
func NewSubscriber(receiver *Receiver, config SubscriberConfig) *Subscriber {
	return message.NewSubscriber(receiver, config)
}

// NewSubscriberFromClient creates a new Subscriber directly from an Azure Service Bus client.
// This is a convenience function that creates a Receiver internally.
//
// Example:
//
//	subscriber := azservicebus.NewSubscriberFromClient(client, azservicebus.ReceiverConfig{}, azservicebus.SubscriberConfig{
//	    Concurrency: 1,
//	})
func NewSubscriberFromClient(client *sdk.Client, receiverConfig ReceiverConfig, subscriberConfig SubscriberConfig) *Subscriber {
	receiver := NewReceiver(client, receiverConfig)
	return message.NewSubscriber(receiver, subscriberConfig)
}

// DefaultSubscriberConfig returns a default SubscriberConfig with sensible defaults.
func DefaultSubscriberConfig() SubscriberConfig {
	return SubscriberConfig{
		Concurrency: 1,
	}
}
