package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"

	"github.com/joho/godotenv"
)

func main() {
	// Load env file before rootCmd.Run so that env vars are available as flag
	// Sources (e.g. SERVICEBUS_CONNECTION) during flag parsing.
	if err := preloadEnvFile(); err != nil {
		slog.Error("Could not load env file", "error", err)
		os.Exit(1)
	}
	if err := rootCmd.Run(context.Background(), os.Args); err != nil {
		os.Exit(1)
	}
}

// preloadEnvFile parses --env-file from os.Args before CLI flag parsing so
// env vars from the file are visible to flag Sources.
func preloadEnvFile() error {
	// FlagSet with ContinueOnError is required: flag.Parse() exits on unknown
	// flags, and all urfave/cli flags are unknown to the stdlib flag package.
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	envFile := fs.String("env-file", "", "")
	_ = fs.Parse(os.Args[1:])
	if *envFile == "" {
		return nil
	}
	return godotenv.Load(*envFile)
}
