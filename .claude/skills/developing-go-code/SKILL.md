# Developing Go Code

The gopipe-azservicebus codebase follows these key Go development practices.

## Build & Testing

```bash
go test ./...         # Run all tests (integration tests skip without Azure env)
go test -race ./...   # With race detector — use before pushing
go build ./...        # Build all packages
go vet ./...          # Static analysis
```

Integration tests call `skipIfNoServiceBus(t)` or `skipIfNoManagement(t)` — they skip gracefully without Azure env vars.

## API Design Principles

- **Constructors** use config structs: `NewSubscriber(client, topic, sub, SubscriberConfig{})`
- **Methods** take direct parameters
- **Optional config** uses zero values as defaults (e.g. zero `GracePeriod` → 30s default)

## Documentation Requirements

Godoc comments must begin by stating the function name and its purpose:

```go
// NewSubscriber creates a Subscriber for the given topic and subscription.
func NewSubscriber(client *Client, topic, subscription string, cfg SubscriberConfig) *Subscriber {
```

For complex functions, include practical usage examples.

## Error Handling

Return errors rather than panicking (except `Must*` functions). Wrap errors with context immediately after calls:

```go
if err := receiver.ReceiveMessages(ctx, n, nil); err != nil {
    return fmt.Errorf("subscriber: receive: %w", err)
}
```

## Azure Service Bus Patterns

**Settlement error codes** — use `errors.As` to check `sbErr.Code`:
- `CodeLockLost`, `CodeClosed`, `CodeConnectionLost` → expected on shutdown/reconnect → log warn, return
- Anything else → unexpected → log error

**Reconnection** — recreate sender/receiver on connection errors; use `done` channel + `sync.Once` for shutdown signaling (not a bool flag).

**In-flight tracking** — acquire semaphore before sending message to channel; release in both ack and nack callbacks.

**Graceful shutdown** — acquire all semaphore slots with context timeout to wait for all messages to settle before closing receivers.

## Testing Patterns

- Unit tests: no external dependencies, test logic in isolation
- Integration tests: call `skipIfNoServiceBus(t)` or `skipIfNoManagement(t)`
- Use table-driven tests for multiple cases
- Apply `t.Parallel()` where safe
- Use `-race` flag for concurrency-sensitive code
