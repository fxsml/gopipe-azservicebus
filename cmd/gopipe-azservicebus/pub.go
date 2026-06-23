package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	servicebus "github.com/fxsml/gopipe-azservicebus"
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
		Action: func(ctx context.Context, cmd *cli.Command) error {
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

			// Create ServiceBus client and publisher
			sbClient, err := servicebus.NewClient(cmd.String("connection"))
			if err != nil {
				return fmt.Errorf("create ServiceBus client: %w", err)
			}

			publisher, err := servicebus.NewPublisher(sbClient, topic, servicebus.PublisherConfig{})
			if err != nil {
				return fmt.Errorf("create ServiceBus publisher: %w", err)
			}
			defer func() {
				if err := publisher.Close(); err != nil {
					slog.Error("Failed to close publisher", "topic", topic, "error", err)
				}
			}()

			// Create channel for messages
			ch := make(chan *message.RawMessage, 100)

			// Start publisher
			done, err := publisher.Publish(ctx, cliPipeline, ch)
			if err != nil {
				return fmt.Errorf("start publisher: %w", err)
			}

			// Parse JSONL messages using gopipe's ParseRaw()
			scanner := bufio.NewScanner(reader)
			scanner.Buffer(make([]byte, 1<<20), 10<<20) // up to 10 MiB per line
			for scanner.Scan() {
				line := scanner.Bytes()
				if len(line) == 0 {
					continue
				}

				msg, err := message.ParseRaw(bytes.NewReader(line))
				if err != nil {
					slog.Error("Failed to parse message", "error", err)
					continue
				}

				ch <- msg
			}

			if err := scanner.Err(); err != nil {
				close(ch)
				<-done
				return fmt.Errorf("reading input: %w", err)
			}

			// Close channel and wait for publisher to finish
			close(ch)
			<-done

			return nil
		},
		Flags: []cli.Flag{
			sbTopicFlag,
			sbInputFileFlag,
		},
	}
)
