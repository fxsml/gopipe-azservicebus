package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"

	servicebus "github.com/fxsml/gopipe-azservicebus"
	"github.com/fxsml/gopipe-azservicebus/internal/jsonl"
	"github.com/fxsml/gopipe/message"
	"github.com/urfave/cli/v3"
)

var (
	// publish flags

	sbTopicFlag = &cli.StringFlag{
		Name:     "topic",
		Aliases:  []string{"T"},
		Required: true,
		Usage:    "topic or queue name to publish to",
	}
	sbInputFileFlag = &cli.StringFlag{
		Name:    "input-file",
		Aliases: []string{"i"},
		Usage:   "file to read messages from; omit or use '-' for stdin",
	}
	sbFormatFlag = &cli.StringFlag{
		Name:    "format",
		Aliases: []string{"f"},
		Value:   "auto",
		Usage:   "input format: auto, json (single event), or jsonl (JSON Lines); auto detects .json extension",
	}

	// publish command

	sbPublishCmd = &cli.Command{
		Name:  "pub",
		Usage: "publish CloudEvents messages to a Service Bus topic",
		Action: withOSSignal(func(ctx context.Context, cmd *cli.Command) error {
			var reader io.Reader
			inputFile := cmd.String("input-file")
			if inputFile == "" || inputFile == "-" {
				reader = os.Stdin
			} else {
				f, err := os.Open(inputFile)
				if err != nil {
					return err
				}
				defer func(f *os.File) {
					if err := f.Close(); err != nil {
						slog.Error("Failed to close input file", "file", inputFile, "error", err)
					}
				}(f)
				reader = f
			}

			topic := cmd.String("topic")
			format := resolveInputFormat(inputFile, cmd.String("format"))
			if format != "json" && format != "jsonl" {
				slog.Error("Invalid format", "format", format)
				return fmt.Errorf("invalid format %q: must be json or jsonl", format)
			}

			// Create ServiceBus client and publisher
			sbClient, err := servicebus.NewClient(cmd.String("connection"))
			if err != nil {
				return fmt.Errorf("create ServiceBus client: %w", err)
			}

			var publishErrors atomic.Int64
			publisher, err := servicebus.NewPublisher(sbClient, topic, servicebus.PublisherConfig{
				ErrorHandler: func(batch []*message.Message, err error) {
					publishErrors.Add(int64(len(batch)))
					slog.Error("Failed to publish batch", "count", len(batch), "error", err)
				},
			})
			if err != nil {
				return fmt.Errorf("create ServiceBus publisher: %w", err)
			}
			defer func() {
				if err := publisher.Close(); err != nil {
					slog.Error("Failed to close publisher", "topic", topic, "error", err)
				}
			}()

			var (
				ch         <-chan *message.Message
				subscriber *jsonl.Subscriber
			)

			if format == "json" {
				msg, err := message.ParseRaw(reader)
				if err != nil {
					slog.Error("Failed to parse message", "error", err)
					return fmt.Errorf("parse json: %w", err)
				}
				msgCh := make(chan *message.Message, 1)
				msgCh <- msg
				close(msgCh)
				ch = msgCh
			} else {
				subscriber = jsonl.NewSubscriber(reader, jsonl.SubscriberConfig{})
				ch, err = subscriber.Subscribe(ctx)
				if err != nil {
					return fmt.Errorf("subscribe to file %s: %w", inputFile, err)
				}
			}

			// Use context.Background() because we want to ensure that the publisher can finish writing
			// all messages even if the main context is canceled.
			done, err := publisher.Publish(context.Background(), cliPipeline, ch)
			if err != nil {
				return fmt.Errorf("start publisher: %w", err)
			}

			<-done

			publishFailed := publishErrors.Load()
			var scanned, sent, parseErrs int64
			if subscriber != nil {
				m := subscriber.Metrics()
				scanned, sent, parseErrs = m.Scanned, m.Sent, m.ParseErrors
			} else {
				scanned, sent = 1, 1
			}
			slog.Info("Publish complete",
				"topic", topic,
				"scanned", scanned,
				"ingested", sent,
				"parse_errors", parseErrs,
				"publish_errors", publishFailed,
			)
			if publishFailed > 0 || parseErrs > 0 {
				return fmt.Errorf("%d message(s) failed to publish, %d message(s) failed to parse", publishFailed, parseErrs)
			}
			return nil
		}),
		Flags: []cli.Flag{
			sbTopicFlag,
			sbInputFileFlag,
			sbFormatFlag,
		},
	}
)

// resolveInputFormat determines the effective input format.
// The explicit flag wins; "auto" falls back to file extension detection.
func resolveInputFormat(inputFile, flag string) string {
	if flag != "auto" {
		return flag
	}
	if strings.HasSuffix(strings.ToLower(inputFile), ".json") {
		return "json"
	}
	return "jsonl"
}
