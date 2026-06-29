package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
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
		Usage:   "jsonl (JSON Lines) file to read messages from; omit or use '-' for stdin",
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

			// Create jsonl subscriber
			subscriber := jsonl.NewSubscriber(reader, jsonl.SubscriberConfig{})

			ch, err := subscriber.Subscribe(ctx)
			if err != nil {
				return fmt.Errorf("subscribe to file %s: %w", inputFile, err)
			}

			// Create ServiceBus client and publisher
			sbClient, err := servicebus.NewClient(cmd.String("connection"))
			if err != nil {
				return fmt.Errorf("create ServiceBus client: %w", err)
			}

			var publishErrors atomic.Int64
			publisher, err := servicebus.NewPublisher(sbClient, topic, servicebus.PublisherConfig{
				ErrorHandler: func(batch []*message.RawMessage, err error) {
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

			// Use context.Background() because we want to ensure that the publisher can finish writing
			// all messages even if the main context is canceled.
			done, err := publisher.Publish(context.Background(), cliPipeline, ch)
			if err != nil {
				return fmt.Errorf("start publisher: %w", err)
			}

			<-done

			publishFailed := publishErrors.Load()
			subMetrics := subscriber.Metrics()
			slog.Info("Publish complete",
				"topic", topic,
				"scanned", subMetrics.Scanned,
				"ingested", subMetrics.Sent,
				"parse_errors", subMetrics.ParseErrors,
				"publish_errors", publishFailed,
			)
			if publishFailed > 0 || subMetrics.ParseErrors > 0 {
				return fmt.Errorf("%d message(s) failed to publish, %d line(s) failed to parse", publishFailed, subMetrics.ParseErrors)
			}
			return nil
		}),
		Flags: []cli.Flag{
			sbTopicFlag,
			sbInputFileFlag,
		},
	}
)
