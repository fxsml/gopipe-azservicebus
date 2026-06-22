package main

import (
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
