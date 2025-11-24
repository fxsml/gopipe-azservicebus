package azservicebus

import (
	"context"
	"time"

	"github.com/fxsml/gopipe"
	"github.com/fxsml/gopipe/message"
)

type Subscriber struct{}

func NewSubscriber() *Subscriber {
	return &Subscriber{}
}

type SubscriberConfig struct {
	// SubscribeTimeout is the timeout for subscribe operations
	// Default: 30 seconds
	SubscribeTimeout time.Duration

	// CloseTimeout is the timeout for closing senders during shutdown
	// Default: 30 seconds
	CloseTimeout time.Duration

	// ShutdownTimeout is the maximum time to wait for graceful shutdown
	// Default: 60 seconds
	ShutdownTimeout time.Duration

	// BatchMaxSize is the number of messages to send in a single batch
	// Default: 1
	BatchMaxSize int

	// BatchMaxDuration is the maximum duration to wait before sending a batch
	// Default: 0 (no delay)
	BatchMaxDuration time.Duration

	// MarshalFunc is a function to marshal the message payload
	// Default: json.Marshal
	MarshalFunc func(any) ([]byte, error)
}

func (s *Subscriber) Subscribe(ctx context.Context, queueOrTopic string) (<-chan message.Message, error) {
	// TODO: implement subscription logic
	// use gopipe.NewGenerator to create a channel of messages, e.g.
	_ = gopipe.NewGenerator(func(ctx context.Context) ([]message.Message, error) {
		return nil, nil
	}).Generate(ctx)
	return nil, nil
}
