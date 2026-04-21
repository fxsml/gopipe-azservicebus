# Go Procedures

## Pre-Push Validation

```bash
go test ./...         # Run all tests (integration tests skip without Azure env)
go test -race ./...   # With race detector — required before pushing concurrency changes
go build ./...        # Build all packages
go vet ./...          # Static analysis
```

Integration tests skip gracefully when Azure env vars are not set (`skipIfNoServiceBus`, `skipIfNoManagement`). Unit tests (e.g. `internal/semaphore`) always run.

## Documentation Standards

Godoc comments must begin with the function name and describe its purpose:

```go
// NewSubscriber creates a Subscriber for the given topic and subscription.
// cfg may be zero-valued; defaults are applied for all unset fields.
func NewSubscriber(client *Client, topic, subscription string, cfg SubscriberConfig) *Subscriber {
```

For complex functions, include practical usage examples.

## Code Lifecycle

When deprecating:

```go
// Deprecated: Use NewSubscriberWithOptions instead.
func OldNewSubscriber() {}
```

Update CHANGELOG under `[Unreleased]` and add migration guidance in the godoc or README.

## Testing Practices

- **Integration tests**: call `skipIfNoServiceBus(t)` or `skipIfNoManagement(t)` at the top
- **Unit tests**: no external dependencies; test logic in isolation
- Use table-driven tests for multiple cases
- Apply `t.Parallel()` where safe
- Use `-race` flag for any concurrency-sensitive code (semaphore, shutdown, reconnection)

## Error Handling

- Return errors, don't panic (except `Must*` functions)
- Wrap with context: `fmt.Errorf("subscriber: receive messages: %w", err)`
- Check Azure SB error codes using `errors.As(err, &sbErr)` and `sbErr.Code`

## Azure Service Bus Patterns

**Expected errors on shutdown/reconnect:**

```go
var sbErr *azservicebus.Error
if errors.As(err, &sbErr) {
    switch sbErr.Code {
    case azservicebus.CodeLockLost,
         azservicebus.CodeClosed,
         azservicebus.CodeConnectionLost:
        // Expected during shutdown or reconnect — log warn, don't error
        s.logger.Warn("message will be redelivered", "reason", sbErr.Code)
        return
    }
}
// Unexpected — log error
s.logger.Error("settlement failed", "error", err)
```

**Shutdown signaling** — use `done` channel + `sync.Once`, not a bool:

```go
type Subscriber struct {
    done     chan struct{}
    doneOnce sync.Once
}

func (s *Subscriber) close() {
    s.doneOnce.Do(func() { close(s.done) })
}
```

**In-flight backpressure** — semaphore acquire before send, release in ack/nack:

```go
if err := s.sem.Acquire(ctx, 1); err != nil {
    return // context cancelled
}
// release must happen in both ack and nack
msg.Ack = func() { defer s.sem.Release(1); ... }
msg.Nack = func() { defer s.sem.Release(1); ... }
ch <- msg
```
