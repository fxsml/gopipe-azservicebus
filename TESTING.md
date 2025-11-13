# Testing Guide

This document explains how to run tests for gopipe-azservicebus using the Azure Service Bus Emulator.

## Prerequisites

- Docker and Docker Compose
- Go 1.21 or later

## Quick Start

### 1. Start the Azure Service Bus Emulator

```bash
# Start the emulator in the background
docker compose up -d

# Wait for the emulator to be ready (check logs)
docker compose logs -f emulator

# Wait until you see: "Emulator Service is Successfully Up!"
```

### 2. Run Tests

```bash
# Run all tests
go test -v ./...

# Run specific test suites
go test -v -run TestSubscriber
go test -v -run TestPublisher
go test -v -run TestIntegration

# Run with race detector
go test -v -race ./...

# Generate coverage report
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### 3. Stop the Emulator

```bash
# Stop and remove containers
docker compose down

# Stop and remove all data
docker compose down -v
```

## Test Organization

### Subscriber Tests (`subscriber_test.go`)

Table-driven tests focusing on message receiving behavior:

- **TestSubscriber_ReceiveAndAck**: Tests message acknowledgment (Ack) and rejection (Nack)
  - Single message ack
  - Multiple messages ack
  - Message nack and requeue behavior
  - Partial ack scenarios

- **TestSubscriber_ConcurrentProcessing**: Tests concurrent message processing
  - Sequential processing (1 worker)
  - Concurrent processing (5 workers)
  - Verifies parallelism

- **TestSubscriber_TopicSubscription**: Tests topic/subscription patterns
  - Subscribe using "topic/subscription" format
  - Verify message delivery to subscriptions

- **TestSubscriber_MetadataMapping**: Tests metadata preservation
  - Standard Service Bus properties (MessageID, Subject, CorrelationID, etc.)
  - Custom application properties
  - Metadata extraction and mapping

### Publisher Tests (`publisher_test.go`)

Table-driven tests focusing on message publishing behavior:

- **TestPublisher_PublishToQueue**: Tests publishing to queues
  - Single message publishing
  - Multiple messages publishing
  - Publishing with metadata
  - Verification of published messages

- **TestPublisher_PublishToTopic**: Tests publishing to topics
  - Publish to topic
  - Verify delivery to subscriptions

- **TestPublisher_ConcurrentPublish**: Tests concurrent publishing
  - Single publisher
  - Multiple publishers publishing simultaneously
  - Verification of all messages

- **TestPublisher_ContextCancellation**: Tests context cancellation behavior
  - Cancel context during publishing
  - Verify proper cleanup

- **TestPublisher_MetadataPreservation**: Tests metadata mapping
  - Standard Service Bus properties
  - Custom application properties
  - Verification of preserved metadata

### Integration Tests (`integration_test.go`)

End-to-end tests combining publisher and subscriber:

- **TestIntegration_PublishSubscribe**: Basic publish-subscribe flow
  - Publish messages, verify receipt
  - With and without processing delays

- **TestIntegration_Pipeline**: Message transformation pipeline
  - Input queue → Transform → Output queue
  - Message filtering
  - Acknowledgment forwarding

- **TestIntegration_InFlightTracking**: Verifies in-flight message tracking
  - Tracks messages during processing
  - Verifies graceful shutdown waits for in-flight messages

- **TestIntegration_ErrorRecovery**: Tests error handling and retries
  - Simulated failures
  - Message redelivery
  - Eventual success after retries

## Test Configuration

### Environment Variables

Tests automatically use the Azure Service Bus Emulator by default. To test against real Azure Service Bus:

```bash
# Option 1: Connection string
export AZURE_SERVICEBUS_CONNECTION_STRING="Endpoint=sb://your-namespace..."

# Option 2: Namespace (uses DefaultAzureCredential)
export AZURE_SERVICEBUS_NAMESPACE="your-namespace.servicebus.windows.net"

# Run tests
go test -v ./...
```

### Emulator Configuration

The emulator is configured via `emulator-config.json` which preconfigures:

**Queues:**
- `test-queue-001`: General purpose test queue
- `test-queue-002`: Secondary test queue
- `test-queue-batch`: For batch processing tests
- `test-queue-concurrent`: For concurrent processing tests
- `test-queue-deadletter`: For error/retry tests

**Topics:**
- `test-topic-001` with subscription `test-sub-001`
- `test-topic-multi` with subscriptions `sub-001` and `sub-002`

## Running Specific Test Scenarios

### Test Message Acknowledgment

```bash
go test -v -run TestSubscriber_ReceiveAndAck
```

### Test Concurrent Processing

```bash
go test -v -run TestSubscriber_ConcurrentProcessing
go test -v -run TestPublisher_ConcurrentPublish
```

### Test Metadata Mapping

```bash
go test -v -run TestSubscriber_MetadataMapping
go test -v -run TestPublisher_MetadataPreservation
```

### Test Complete Pipeline

```bash
go test -v -run TestIntegration_Pipeline
```

### Test Error Recovery

```bash
go test -v -run TestIntegration_ErrorRecovery
```

## Skipping Integration Tests

Integration tests are skipped when running in short mode:

```bash
# Skip integration tests (runs only unit tests)
go test -short ./...
```

## Troubleshooting

### Emulator Won't Start

Check if ports are already in use:

```bash
# Check port 5672 (AMQP)
lsof -i :5672

# Check port 5300 (Config API)
lsof -i :5300

# Stop conflicting services or change ports in compose.yaml
```

### Tests Timeout

Increase test timeout:

```bash
go test -v -timeout 10m ./...
```

### View Emulator Logs

```bash
# Follow emulator logs
docker compose logs -f emulator

# View MSSQL logs
docker compose logs -f mssql

# Check container status
docker compose ps
```

### Reset Test Environment

```bash
# Stop everything and remove volumes
docker compose down -v

# Start fresh
docker compose up -d
```

### Test Failures Due to Timing

Some tests use timeouts. If running on slow hardware, you may need to adjust:

```bash
# Edit test files and increase timeout values
# Look for: time.After(20 * time.Second)
# Increase to: time.After(60 * time.Second)
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Start Azure Service Bus Emulator
        run: docker compose up -d

      - name: Wait for emulator
        run: |
          timeout=60
          elapsed=0
          while [ $elapsed -lt $timeout ]; do
            if docker compose logs emulator 2>&1 | grep -q "Emulator Service is Successfully Up"; then
              echo "Emulator is ready!"
              exit 0
            fi
            sleep 5
            elapsed=$((elapsed + 5))
          done
          echo "Timeout waiting for emulator"
          exit 1

      - name: Run tests
        run: go test -v -race -coverprofile=coverage.out ./...

      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          files: ./coverage.out

      - name: Cleanup
        if: always()
        run: docker compose down -v
```

## Test Coverage

View test coverage:

```bash
# Generate coverage
go test -coverprofile=coverage.out ./...

# View in terminal
go tool cover -func=coverage.out

# View in browser
go tool cover -html=coverage.out
```

## Writing New Tests

When adding new tests:

1. Use table-driven tests for multiple scenarios
2. Focus on behavior, not implementation
3. Use test helpers from `testing.go`
4. Add `if testing.Short()` skip for integration tests
5. Use descriptive test names
6. Log intermediate steps for debugging
7. Clean up resources in `t.Cleanup()`

Example:

```go
func TestYourFeature(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }

    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"scenario 1", "input1", "output1"},
        {"scenario 2", "input2", "output2"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

## Performance Testing

For performance/load testing:

```bash
# Run with verbose output and timing
go test -v -bench=. -benchmem ./...

# Test with specific message count
go test -v -run TestIntegration_PublishSubscribe -count 1
```

## Additional Resources

- [Azure Service Bus Emulator Documentation](https://learn.microsoft.com/en-us/azure/service-bus-messaging/test-locally-with-service-bus-emulator)
- [Go Testing Documentation](https://pkg.go.dev/testing)
- [Table-Driven Tests in Go](https://go.dev/wiki/TableDrivenTests)
