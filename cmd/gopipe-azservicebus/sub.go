package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	servicebus "github.com/fxsml/gopipe-azservicebus"
	"github.com/urfave/cli/v3"
)

var (
	// subscribe flags

	sbSubscriptionFlag = &cli.StringFlag{
		Name:     "subscription",
		Aliases:  []string{"s"},
		Usage:    "subscription to read from, in 'topic/subscription' format",
		Required: true,
	}
	sbOutputFileFlag = &cli.StringFlag{
		Name:    "output-file",
		Aliases: []string{"o"},
		Usage:   "jsonl (JSON Lines) file to write messages to; omit to write to stdout",
	}
	sbTimeoutFlag = &cli.DurationFlag{
		Name:    "timeout",
		Aliases: []string{"t"},
		Usage:   "stop after this duration; 0 or unset means no timeout",
	}
	sbLimitFlag = &cli.IntFlag{
		Name:    "limit",
		Aliases: []string{"l"},
		Usage:   "maximum number of messages to read before exiting; 0 means unlimited",
	}

	// subscribe command

	sbSubscribeCmd = &cli.Command{
		Name:  "sub",
		Usage: "subscribe to CloudEvents messages from a Service Bus subscription",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			var cancel context.CancelFunc
			if timeout := cmd.Duration("timeout"); timeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, timeout)
			} else {
				ctx, cancel = context.WithCancel(ctx)
			}
			defer cancel()

			limit := cmd.Int("limit")

			subscription := cmd.String("subscription")
			var writer io.Writer = os.Stdout
			outputFile := cmd.String("output-file")
			if outputFile != "" {
				file, err := os.Create(outputFile)
				if err != nil {
					return fmt.Errorf("create output file: %w", err)
				}
				defer func() {
					if err := file.Close(); err != nil {
						slog.Error("Failed to close output file", "file", outputFile, "error", err)
					}
				}()
				writer = file
			}

			// Create ServiceBus client
			sbClient, err := servicebus.NewClient(cmd.String("connection"))
			if err != nil {
				return fmt.Errorf("create ServiceBus client: %w", err)
			}

			cfg := servicebus.SubscriberConfig{}
			if limit > 0 {
				// MaxInFlight=1 ensures no next message is prefetched before Ack(),
				// so cancel() in the loop below stops intake cleanly at the limit.
				cfg.MaxInFlight = 1
			}

			// Create subscriber for this subscription
			subscriber, err := servicebus.NewSubscriber(sbClient, subscription, cliPipeline, cfg)
			if err != nil {
				return fmt.Errorf("create subscriber for %s: %w", subscription, err)
			}

			msgs, err := subscriber.Subscribe(ctx, cliPipeline)
			if err != nil {
				return fmt.Errorf("start subscriber for %s: %w", subscription, err)
			}

			// Stream messages as JSONL using gopipe's WriteTo()
			var received, written int64
			for msg := range msgs {
				received++

				if _, err := msg.WriteTo(writer); err != nil {
					slog.Info("Subscribe complete", "received", received, "written", written)
					return fmt.Errorf("write msg: %w", err)
				}
				if _, err := writer.Write([]byte("\n")); err != nil {
					slog.Info("Subscribe complete", "received", received, "written", written)
					return fmt.Errorf("write msg: %w", err)
				}
				written++

				// Cancel before Ack so the subscriber stops fetching. Safe because
				// MaxInFlight=1 guarantees no message is already in flight at this point.
				if limit > 0 && written >= int64(limit) {
					cancel()
				}

				msg.Ack()
			}

			slog.Info("Subscribe complete", "received", received, "written", written)
			return nil
		},
		Flags: []cli.Flag{
			sbSubscriptionFlag,
			sbTimeoutFlag,
			sbLimitFlag,
			sbOutputFileFlag,
		},
	}
)
