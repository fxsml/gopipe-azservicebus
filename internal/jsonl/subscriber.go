package jsonl

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"sync/atomic"

	"github.com/fxsml/gopipe/message"
)

type SubscriberConfig struct {
	// BufferSize is the channel buffer size (default: 100).
	BufferSize int
}

type Subscriber struct {
	reader      io.Reader
	config      SubscriberConfig
	sent        atomic.Int64
	scanned     atomic.Int64
	parseErrors atomic.Int64
}

type SubscriberMetrics struct {
	Sent        int64
	Scanned     int64
	ParseErrors int64
}

func NewSubscriber(reader io.Reader, config SubscriberConfig) *Subscriber {
	return &Subscriber{
		reader: reader,
		config: config,
	}
}

func (s *Subscriber) Subscribe(ctx context.Context) (<-chan *message.RawMessage, error) {
	ch := make(chan *message.RawMessage, s.config.BufferSize)

	go func() {
		defer close(ch)

		var lineNum int64
		scanner := bufio.NewScanner(s.reader)
		scanner.Buffer(make([]byte, 1<<20), 10<<20) // up to 10 MiB per line
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			lineNum++
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			s.scanned.Add(1)

			msg, err := message.ParseRaw(bytes.NewReader(line))
			if err != nil {
				s.parseErrors.Add(1)
				slog.Error("Failed to parse message", "line", lineNum, "error", err)
				continue
			}

			select {
			case <-ctx.Done():
				return
			case ch <- msg:
				s.sent.Add(1)
			}
		}

		if err := scanner.Err(); err != nil {
			slog.Error("Failed to scan message", "error", err)
		}
		slog.Info("Subscribe complete", "lines", lineNum, "sent", s.sent.Load(), "parseErrors", s.parseErrors.Load())
	}()

	return ch, nil
}

func (s *Subscriber) Metrics() SubscriberMetrics {
	return SubscriberMetrics{
		Sent:        s.sent.Load(),
		Scanned:     s.scanned.Load(),
		ParseErrors: s.parseErrors.Load(),
	}
}
