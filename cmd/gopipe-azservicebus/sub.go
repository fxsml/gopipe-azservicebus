package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	servicebus "github.com/fxsml/gopipe-azservicebus"
	"github.com/fxsml/gopipe-azservicebus/internal/jsonl"
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
	sbPeekFlag = &cli.BoolFlag{
		Name:    "peek",
		Aliases: []string{"p"},
		Usage:   "peek mode; true if you want to use non-destructive peek mode",
	}

	// subscribe command

	sbSubscribeCmd = &cli.Command{
		Name:  "sub",
		Usage: "subscribe to CloudEvents messages from a Service Bus subscription",
		Action: withOSSignal(func(ctx context.Context, cmd *cli.Command) error {
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

			cfg := servicebus.SubscriberConfig{
				EnablePeekMode: cmd.Bool("peek"),
			}
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

			publisher := jsonl.NewPublisher(writer, jsonl.PublisherConfig{})

			// Use context.Background() because we want to ensure that the publisher can finish writing
			// all messages even if the main context is canceled.
			done, err := publisher.Publish(context.Background(), msgs)
			if err != nil {
				return fmt.Errorf("start publisher for %s: %w", subscription, err)
			}

			<-done
			metrics := publisher.Metrics()
			slog.Info("Publisher finished",
				"subscription", subscription,
				"limit", limit,
				"timeout", cmd.Duration("timeout"),
				"output-file", outputFile,
				"received", metrics.Received,
				"written", metrics.Written,
			)
			return nil
		}),
		Flags: []cli.Flag{
			sbSubscriptionFlag,
			sbTimeoutFlag,
			sbLimitFlag,
			sbOutputFileFlag,
			sbPeekFlag,
		},
	}
)
