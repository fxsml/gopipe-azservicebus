# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`gopipe-azservicebus` is a Go library that provides Azure Service Bus integration for the gopipe pipeline framework. It enables pipeline stages to consume from and publish to Azure Service Bus queues and topics.

## Development Commands

### Initial Setup
```bash
go mod init github.com/fxsml/gopipe-azservicebus
go mod tidy
```

### Build
```bash
go build ./...
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests with verbose output
go test -v ./...

# Run a specific test
go test -v -run TestName ./path/to/package
```

### Code Quality
```bash
# Format code
go fmt ./...

# Run linter (requires golangci-lint)
golangci-lint run

# Run go vet
go vet ./...
```

## Architecture

This library follows the adapter pattern to integrate Azure Service Bus with gopipe pipelines:

### Core Components (Expected)

1. **Source Stage**: Consumes messages from Azure Service Bus queues/topics and feeds them into a gopipe pipeline
2. **Sink Stage**: Publishes messages from a gopipe pipeline to Azure Service Bus queues/topics
3. **Configuration**: Manages connection strings, queue/topic names, and Service Bus client options
4. **Message Mapping**: Converts between gopipe data formats and Azure Service Bus messages

### Integration Points

- Uses Azure SDK for Go (`github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus`)
- Implements gopipe stage interfaces for pipeline integration
- Handles message acknowledgment, retries, and dead-letter queues according to Service Bus semantics
- Uses gopipe.Message
    - with Ack/Nack implementation
    - with Metadata for all custom and broker properties
- Explicitly allows parallel downstream processing
- Uses no ouput buffer to delegate parallelism to downstream processing

### Error Handling

- Transient errors should trigger retries with exponential backoff
- Should use gopipe's build in error handling with retries if applicable
- Fatal errors should properly close connections and clean up resources
- Failed messages should be dead-lettered when appropriate

### Concurrency

- Service Bus receivers and senders must be thread-safe
- Pipeline stages should support concurrent message processing
- Use proper synchronization for shared state

### Publisher

- The publisher type should implement the following interface

```go
type Publisher[T any] interface {
	Publish(ctx context.Context, topic string, msgs <-chan Message[T]) (<-chan struct{}, error)
}
```

- Publisher may use gopipe.NewGenerator if applicable

### Subscriber 

- The subscriber should implement the following interface

```go
type Subsciber[T any] interface {
	Subscibe(ctx context.Context, topic string) (<-chan Message[T], error)
}
```

- Subscriber may use gopipe.NewSinkPipe if applicable