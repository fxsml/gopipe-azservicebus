package jsonl

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"

	"github.com/fxsml/gopipe/message"
)

type PublisherConfig struct{}

type Publisher struct {
	writer   io.Writer
	config   PublisherConfig
	received atomic.Int64
	written  atomic.Int64
}

type PublisherMetrics struct {
	Received int64
	Written  int64
}

func NewPublisher(writer io.Writer, config PublisherConfig) *Publisher {
	return &Publisher{
		writer: writer,
		config: config,
	}
}

func (p *Publisher) Publish(ctx context.Context, in <-chan *message.Message) (<-chan struct{}, error) {
	done := make(chan struct{})

	go func() {
		defer func() {
			close(done)
			slog.Info("Publish complete", "received", p.received.Load(), "written", p.written.Load())
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-in:
				if !ok {
					return
				}
				p.received.Add(1)

				if _, err := msg.WriteTo(p.writer); err != nil {
					slog.Error("Failed to write message", "error", err)
					msg.Nack(err)
					return
				}
				if _, err := p.writer.Write([]byte("\n")); err != nil {
					slog.Error("Failed to write message", "error", err)
					msg.Nack(err)
					return
				}
				p.written.Add(1)
				msg.Ack()
			}
		}
	}()

	return done, nil
}

func (p *Publisher) Metrics() PublisherMetrics {
	return PublisherMetrics{
		Received: p.received.Load(),
		Written:  p.written.Load(),
	}
}
