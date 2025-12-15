package azservicebus

import (
	"context"
	"time"

	"github.com/fxsml/gopipe"
	"github.com/fxsml/gopipe/message"
)

// NewMessageGenerator creates a gopipe Generator that produces messages from the Receiver.
// This allows seamless integration with gopipe pipelines for consuming messages.
//
// Usage:
//
//	receiver := NewReceiver(client, ReceiverConfig{})
//	generator := NewMessageGenerator(receiver, "my-queue")
//	msgs := generator.Generate(ctx)
//	for msg := range msgs {
//	    // Process message
//	    msg.Ack()
//	}
func NewMessageGenerator(receiver *Receiver, queueOrTopic string, opts ...gopipe.Option[struct{}, *message.Message[[]byte]]) gopipe.Generator[*message.Message[[]byte]] {
	return gopipe.NewGenerator(
		func(ctx context.Context) ([]*message.Message[[]byte], error) {
			return receiver.Receive(ctx, queueOrTopic)
		},
		opts...,
	)
}

// NewMessageSinkPipe creates a gopipe Pipe that sends messages using the Sender.
// Messages are batched for efficient sending.
//
// Usage:
//
//	sender := NewSender(client, SenderConfig{})
//	sink := NewMessageSinkPipe(sender, "my-queue", 10, 100*time.Millisecond)
//	out := sink.Start(ctx, msgs)
//	for range out {
//	    // Each output indicates a batch was sent
//	}
func NewMessageSinkPipe(sender *Sender, queueOrTopic string, batchSize int, batchTimeout time.Duration, opts ...gopipe.Option[[]*message.Message[[]byte], struct{}]) gopipe.Pipe[*message.Message[[]byte], struct{}] {
	return gopipe.NewBatchPipe(
		func(ctx context.Context, batch []*message.Message[[]byte]) ([]struct{}, error) {
			if err := sender.Send(ctx, queueOrTopic, batch); err != nil {
				return nil, err
			}
			return make([]struct{}, len(batch)), nil
		},
		batchSize,
		batchTimeout,
		opts...,
	)
}

// NewMessageTransformSink creates a gopipe Pipe that transforms and sends messages.
// The transform function is applied to each message before batching and sending.
//
// Usage:
//
//	sender := NewSender(client, SenderConfig{})
//	sink := NewMessageTransformSink(sender, "my-queue", 10, 100*time.Millisecond,
//	    func(ctx context.Context, order Order) (*message.Message[[]byte], error) {
//	        body, err := json.Marshal(order)
//	        if err != nil {
//	            return nil, err
//	        }
//	        return message.New(body), nil
//	    })
func NewMessageTransformSink[In any](
	sender *Sender,
	queueOrTopic string,
	batchSize int,
	batchTimeout time.Duration,
	transform func(context.Context, In) (*message.Message[[]byte], error),
	opts ...gopipe.Option[In, struct{}],
) gopipe.Pipe[In, struct{}] {
	// First transform, then batch and send
	transformPipe := gopipe.NewTransformPipe(transform)

	// Create the batch sink
	batchSink := NewMessageSinkPipe(sender, queueOrTopic, batchSize, batchTimeout)

	// Return a combined pipe
	return &combinedPipe[In]{
		transform: transformPipe,
		sink:      batchSink,
	}
}

// combinedPipe combines a transform pipe with a sink pipe
type combinedPipe[In any] struct {
	transform gopipe.Pipe[In, *message.Message[[]byte]]
	sink      gopipe.Pipe[*message.Message[[]byte], struct{}]
}

func (p *combinedPipe[In]) Start(ctx context.Context, in <-chan In) <-chan struct{} {
	transformed := p.transform.Start(ctx, in)
	return p.sink.Start(ctx, transformed)
}
