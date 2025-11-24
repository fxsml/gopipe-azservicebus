# gopipe-azservicebus

Azure Service Bus integration for the gopipe pipeline framework.

## Features

- **Publisher**: Publish messages from gopipe pipelines to Azure Service Bus queues and topics
- **Subscriber**: Consume messages from Azure Service Bus and feed them into gopipe pipelines (planned)
- **Local Development**: Run tests against the Azure Service Bus emulator
- **Message Mapping**: Automatic conversion between gopipe messages and Azure Service Bus messages
- **Metadata Support**: Full support for message properties and application properties

## Installation

```bash
go get github.com/fxsml/gopipe-azservicebus
```

## Quick Start

### Publishing Messages

```go
package main

import (
    "context"

    "github.com/fxsml/gopipe"
    azservicebus "github.com/fxsml/gopipe-azservicebus"
    "github.com/fxsml/gopipe/channel"
)

func main() {
    // Create client using connection string
    client, err := azservicebus.NewClient("Endpoint=sb://...")
    if err != nil {
        panic(err)
    }
    defer client.Close(context.Background())

    // Create publisher
    publisher := azservicebus.NewPublisher(client, azservicebus.PublisherConfig{})
    defer publisher.Close()

    // Create and publish message
    msg := &gopipe.Message[any]{
        Payload: map[string]string{"hello": "world"},
    }
    msg.Properties().Set("message_id", "123")

    err = publisher.Publish("my-queue", channel.FromValues(msg))
    if err != nil {
        panic(err)
    }
}
```

## Local Development with Emulator

The project includes Docker Compose configuration for running the Azure Service Bus emulator locally.

### Prerequisites

- Docker Desktop for Mac (with Rosetta 2 support for Apple Silicon)
- Go 1.24 or later

### Start the Emulator

```bash
make emulator-start
```

See [EMULATOR.md](EMULATOR.md) for detailed emulator setup instructions and troubleshooting.

### Run Tests

```bash
# All tests
make test

# Integration tests only (requires emulator)
make test-integration

# Unit tests only
make test-unit
```

## Configuration

### Publisher Configuration

```go
config := azservicebus.PublisherConfig{
    PublishTimeout:      30 * time.Second,  // Timeout for publish operations
    CloseTimeout:        30 * time.Second,  // Timeout for closing senders
    ShutdownTimeout:     60 * time.Second,  // Maximum graceful shutdown time
    BatchMaxSize:        1,                 // Messages per batch
    BatchMaxDuration:    0,                 // Batch wait time
    MarshalFunc:         json.Marshal,      // Custom marshaler
}

publisher := azservicebus.NewPublisher(client, config)
```

### Authentication

The library supports two authentication methods:

```go
// Connection string (for development/testing)
client, err := azservicebus.NewClient("Endpoint=sb://...")

// DefaultAzureCredential (recommended for production)
client, err := azservicebus.NewClient("your-namespace.servicebus.windows.net")
```

## Message Metadata

Gopipe message properties are automatically mapped to Azure Service Bus message properties:

- `message_id` → MessageID
- `subject` → Subject
- `correlation_id` → CorrelationID
- `content_type` → ContentType
- `to` → To
- `reply_to` → ReplyTo
- `session_id` → SessionID
- All other properties → ApplicationProperties

```go
msg.Properties().Set("message_id", "abc123")
msg.Properties().Set("custom_field", "value")
```

## Architecture

This library follows the adapter pattern to integrate Azure Service Bus with gopipe:

```
gopipe Pipeline → Publisher → Azure Service Bus → Subscriber → gopipe Pipeline
```

### Key Components

- **Client**: Manages connection to Azure Service Bus
- **Publisher**: Implements gopipe publisher interface for sending messages
- **Message Transform**: Converts between gopipe and Azure Service Bus message formats
- **Batch Processing**: Groups messages for efficient sending

## Development Commands

```bash
# Format code
make fmt

# Run linter
make lint

# Run go vet
make vet

# Tidy modules
make tidy

# Build
make build

# Clean up
make clean
```

## Known Issues

### macOS Emulator DNS Issues

The Azure Service Bus emulator may experience DNS resolution issues on macOS, particularly with Apple Silicon. The emulator may show as "unhealthy" but still function correctly. See [EMULATOR.md](EMULATOR.md) for workarounds.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests: `make test`
5. Submit a pull request

## License

See [LICENSE](LICENSE) file for details.

## Resources

- [gopipe Framework](https://github.com/fxsml/gopipe)
- [Azure Service Bus Documentation](https://learn.microsoft.com/en-us/azure/service-bus-messaging/)
- [Azure SDK for Go](https://github.com/Azure/azure-sdk-for-go)
