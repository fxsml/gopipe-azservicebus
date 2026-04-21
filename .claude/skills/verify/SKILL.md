# Verify

Run the full pre-push verification suite.

## Steps

1. `go test ./...` — run all tests (integration tests skip without Azure env vars)
2. `go build ./...` — build all packages
3. `go vet ./...` — static analysis

## Available Tools

Bash only.

## Report

Pass/fail status for each step. On failure, show relevant error messages and remediation suggestions.
