package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"
)

const cliPipeline = "cli"

var (
	// global flags

	envFileFlag = &cli.StringFlag{
		Name:  "env-file",
		Usage: "path to env file (loaded before flag parsing; e.g. --env-file .env.local)",
	}
	sbConnectionFlag = &cli.StringFlag{
		Name:     "connection",
		Aliases:  []string{"c"},
		Required: true,
		Usage:    "Service Bus connection string or namespace hostname",
		Sources:  cli.EnvVars("SERVICEBUS_CONNECTION"),
	}

	// global command

	rootCmd = &cli.Command{
		Name:  "gopipe-azservicebus",
		Usage: "publish and subscribe to Azure Service Bus",
		Commands: []*cli.Command{
			sbPublishCmd,
			sbSubscribeCmd,
		},
		Flags: []cli.Flag{
			envFileFlag,
			sbConnectionFlag,
		},
	}
)

func withOSSignal(action cli.ActionFunc) cli.ActionFunc {
	return func(ctx context.Context, command *cli.Command) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		// Create a channel to listen for OS signals
		signalChan := make(chan os.Signal, 1)
		signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

		var err error
		errChan := make(chan error)
		go func() {
			err = action(ctx, command)
			close(errChan)
		}()

		select {
		case <-ctx.Done():
			slog.Debug("CLI shutdown triggered", "reason", ctx.Err())
		case sig := <-signalChan:
			slog.Debug("CLI shutdown triggered", "reason", "signal", "signal", sig)
			cancel() // Cancel context to trigger pipeline shutdown
		case <-errChan:
			slog.Debug("CLI shutdown triggered", "reason", "finished", "error", err)
		}

		<-errChan
		return err
	}
}
