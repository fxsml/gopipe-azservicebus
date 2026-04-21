# gopipe-azservicebus

Guidelines for AI coding agents working on the Azure Service Bus adapter for gopipe.

## Quick Commands

```bash
go test ./...         # Run all tests (integration tests skip without Azure env)
go test -race ./...   # With race detector
go build ./...        # Build all packages
go vet ./...          # Run linters
```

## Key Rules

1. **Never merge to main** — Use PRs through develop
2. **Conventional commits** — `feat:`, `fix:`, `docs:`, etc.
3. **Document before push** — Features, ADRs, CHANGELOG
4. **Test before push** — `go test ./... && go build ./... && go vet ./...`

## Claude Code Integration

See `.claude/CLAUDE.md` for the full Claude Code configuration.

### Auto-Applied Skills

Domain expertise loaded automatically from `.claude/skills/`:

| Skill | Expertise Area |
|-------|----------------|
| `managing-git-workflow` | Git flow, branch naming, single-module tagging, approval gates |
| `developing-go-code` | Go standards, testing, Azure Service Bus patterns |

### Slash Commands

| Command | Purpose |
|---------|---------|
| `/release-feature BRANCH` | Merge feature branch to develop (history cleanup → PR → verify) |
| `/release VERSION` | Release develop to main with single-module tag |
| `/hotfix NAME` | Create and release a hotfix from main |
| `/create-feature NAME` | Create feature branch from develop |
| `/verify` | Run `go test ./...` && `go build ./...` && `go vet ./...` |
| `/docs-lint` | Check documentation quality and index consistency |
| `/create-adr TITLE` | Create new Architecture Decision Record |
| `/create-plan TITLE` | Create implementation plan |
| `/changelog TYPE DESC` | Add entry to CHANGELOG under [Unreleased] |
| `/review-pr [NUMBER]` | Review PR against project standards |

### Hooks

- **PostToolUse**: `gofmt -w` runs automatically after editing any `.go` file
- **PreToolUse**: `go test ./... && go build ./... && go vet ./...` runs before `git commit` or `git push`

## Package Overview

| File | Purpose |
|------|---------|
| `client.go` | Azure Service Bus client creation (connection string or DefaultAzureCredential) |
| `publisher.go` | Batching publisher with reconnection and graceful shutdown |
| `subscriber.go` | Concurrent subscriber with backpressure, lock renewal, and reconnection |
| `telemetry.go` | OpenTelemetry counters and histograms |
| `internal/semaphore/` | Weighted semaphore with OTel metrics for in-flight tracking |

## Project Structure

| Location | Content |
|----------|---------|
| [docs/procedures/](docs/procedures/) | Modular procedures (git, go, docs, planning) |
| [docs/plans/](docs/plans/) | Implementation plans |
| [docs/adr/](docs/adr/) | Architecture decisions |

### Procedures Reference

| Topic | Procedure |
|-------|-----------|
| Git workflow, commits, releases | [git.md](docs/procedures/git.md) |
| Go standards, godoc, testing | [go.md](docs/procedures/go.md) |
| Documentation, ADRs, templates | [documentation.md](docs/procedures/documentation.md) |
| Plans, prompts, hierarchy | [planning.md](docs/procedures/planning.md) |

## Architecture

### Key Design Decisions

- **Semaphore-based backpressure**: `internal/semaphore` wraps `golang.org/x/sync/semaphore` with OTel metrics. Tracks in-flight messages and caps at `MaxInFlight`.
- **Reconnection on failure**: Both publisher and subscriber recreate their sender/receiver on connection errors.
- **Graceful shutdown**: `Close()` drains in-flight messages using semaphore acquire with a timeout (GracePeriod).
- **Lock renewal**: Auto-detected from broker's `LockedUntil`; configurable override via `LockRenewalInterval`.
- **done channel pattern**: Publisher and Subscriber both use `done` channel + `doneOnce` for clean shutdown.

### API Conventions

| Context | Pattern | Example |
|---------|---------|---------|
| Constructors | Config struct | `NewSubscriber(client, topic, sub, SubscriberConfig{})` |
| Authentication | Client factory | `NewClient(connString)` or `NewClientFromCredential(ns, cred)` |

## Integration Testing

Tests require Azure Service Bus. Without env vars they skip gracefully:

```bash
export SERVICEBUS_CONNECTION="Endpoint=sb://..."
export SERVICEBUS_CONNECTION_MANAGEMENT="Endpoint=sb://..."
export SERVICEBUS_TEST_QUEUE_1="my-queue-1"
export SERVICEBUS_TEST_QUEUE_2="my-queue-2"
go test ./...
```

See `.env.test` for a template (not committed).

## Common Mistakes

### ❌ Ignoring Azure SB error codes

```go
// WRONG — treats all settlement errors as unexpected
if err := receiver.CompleteMessage(ctx, msg, nil); err != nil {
    log.Error("failed to complete", "error", err)
}

// CORRECT — distinguish expected shutdown/reconnect errors
var sbErr *azservicebus.Error
if errors.As(err, &sbErr) {
    switch sbErr.Code {
    case azservicebus.CodeLockLost, azservicebus.CodeClosed, azservicebus.CodeConnectionLost:
        log.Warn("message will be redelivered", "reason", sbErr.Code)
        return
    }
}
log.Error("unexpected settlement error", "error", err)
```

### ❌ Using bool for shutdown signaling

```go
// WRONG — race-prone, inconsistent with Subscriber pattern
type Publisher struct { closing bool }

// CORRECT — done channel + sync.Once
type Publisher struct {
    done     chan struct{}
    doneOnce sync.Once
}
```

### ❌ Acquiring semaphore without releasing in all paths

```go
// WRONG — leaks semaphore slot if ack/nack skipped
s.sem.Acquire(ctx, 1)
ch <- msg  // consumer may not ack

// CORRECT — release in both ack and nack callbacks
msg.Ack = func() { defer s.sem.Release(1); ... }
msg.Nack = func() { defer s.sem.Release(1); ... }
```
