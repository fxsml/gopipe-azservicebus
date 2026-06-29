# Go Procedures

## Pre-Push Checklist

```bash
go test ./...   # Tests pass
go build ./...  # Build succeeds
go vet ./...    # No issues
```

## Godoc Standards

### DO

```go
// NewPublisher creates a Publisher that sends messages to the given Azure
// Service Bus topic or queue using the provided client.
//
// Example:
//
//	pub, err := azservicebus.NewPublisher(client, "my-topic")
func NewPublisher(client *Client, entity string) (*Publisher, error)
```

### DON'T

```go
// NewPublisher creates a publisher.  // Too brief, no context
func NewPublisher(client *Client, entity string) (*Publisher, error)
```

### Guidelines

1. **Precise and concise** - Explain what it does, not how it works internally
2. **Include examples** - Brief usage examples in godoc when helpful
3. **First sentence** - Starts with function name, describes what it does
4. **Parameters** - Document non-obvious parameters

## Deprecation

Mark deprecated code clearly:

```go
// Deprecated: Use NewTyped for pipelines or New for CloudEvents messages.
func OldFunction() {}
```

## Testing

- Tests in `*_test.go` files
- Table-driven tests preferred
- Use `t.Parallel()` where safe
- Mock external dependencies

## Error Handling

- Return errors, don't panic (except in `Must*` functions)
- Wrap errors with context: `fmt.Errorf("operation: %w", err)`
- Check errors immediately after function call
