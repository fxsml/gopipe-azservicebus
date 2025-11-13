package azservicebus

import "time"

// SubscriberConfig holds configuration for Azure Service Bus subscriber
type SubscriberConfig struct {
	// BatchSize is the number of messages to receive in a single batch
	// Default: 1
	BatchSize int

	// MessageTimeout is the maximum time allowed for processing a single message
	// Default: 60 seconds
	MessageTimeout time.Duration

	// AbandonTimeout is the timeout for abandon operations
	// Default: 30 seconds
	AbandonTimeout time.Duration

	// CompleteTimeout is the timeout for complete operations
	// Default: 30 seconds
	CompleteTimeout time.Duration

	// CloseTimeout is the timeout for closing receivers during shutdown
	// Default: 30 seconds
	CloseTimeout time.Duration

	// ConcurrentMessages is the number of messages to process concurrently
	// Default: 1 (sequential processing)
	ConcurrentMessages int
}

// DefaultSubscriberConfig returns a SubscriberConfig with sensible defaults
func DefaultSubscriberConfig() SubscriberConfig {
	return SubscriberConfig{
		BatchSize:          1,
		MessageTimeout:     60 * time.Second,
		AbandonTimeout:     30 * time.Second,
		CompleteTimeout:    30 * time.Second,
		CloseTimeout:       30 * time.Second,
		ConcurrentMessages: 1,
	}
}

// PublisherConfig holds configuration for Azure Service Bus publisher
type PublisherConfig struct {
	// PublishTimeout is the timeout for publish operations
	// Default: 30 seconds
	PublishTimeout time.Duration

	// CloseTimeout is the timeout for closing senders during shutdown
	// Default: 30 seconds
	CloseTimeout time.Duration
}

// DefaultPublisherConfig returns a PublisherConfig with sensible defaults
func DefaultPublisherConfig() PublisherConfig {
	return PublisherConfig{
		PublishTimeout: 30 * time.Second,
		CloseTimeout:   30 * time.Second,
	}
}
