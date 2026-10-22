package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPubCommand_MissingInputFileReturnsError verifies the pub command returns
// a clear, wrapped error when --input-file doesn't exist. The file is opened
// before any Service Bus connection is attempted, so this runs fully offline.
func TestPubCommand_MissingInputFileReturnsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	args := []string{
		"gopipe-azservicebus",
		"--connection", "dummy",
		"pub",
		"--topic", "whatever",
		"--input-file", "/nonexistent/path/does-not-exist.jsonl",
	}

	err := rootCmd.Run(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open input file")
	assert.Contains(t, err.Error(), "does-not-exist.jsonl")
}

// TestMain_ReportsCommandFailureOnStderr verifies the compiled CLI actually
// surfaces a failed command to the user: previously main() discarded the
// error returned by rootCmd.Run entirely, so a missing --input-file produced
// exit code 1 with zero output anywhere (see issue #2).
func TestMain_ReportsCommandFailureOnStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".",
		"--connection", "dummy",
		"pub",
		"--topic", "whatever",
		"--input-file", "/nonexistent/path/does-not-exist.jsonl",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.Error(t, err, "command should exit non-zero")

	output := stderr.String()
	assert.NotEmpty(t, output, "user should see an error message, not silent exit code 1")
	assert.True(t, strings.Contains(output, "does-not-exist.jsonl") || strings.Contains(output, "Command failed"),
		"stderr should mention the failure; got: %q", output)
}
