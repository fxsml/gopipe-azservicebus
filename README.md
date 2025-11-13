# gopipe-azservicebus

Azure Service Bus integration for the [gopipe](https://github.com/fxsml/gopipe) pipeline framework.

## Overview

`gopipe-azservicebus` provides Azure Service Bus adapters that enable pipeline stages to consume from and publish to Azure Service Bus queues and topics. It implements the Publisher and Subscriber patterns with full support for message acknowledgment, metadata handling, and concurrent processing.

## Features

- **Subscriber**: Consume messages from Azure Service Bus queues and topic subscriptions
- **Publisher**: Publish messages to Azure Service Bus queues and topics
- **Message Mapping**: Automatic conversion between gopipe.Message and Azure Service Bus messages
- **Metadata Support**: Full support for Service Bus properties (MessageID, CorrelationID, Subject, etc.)
- **Concurrent Processing**: Configurable concurrent message processing
- **Graceful Shutdown**: Proper handling of in-flight messages during shutdown
- **Error Handling**: Automatic message redelivery on failure (Nack) with dead-letter queue support
- **Flexible Authentication**: Support for both connection strings and DefaultAzureCredential

## Installation

```bash
go get github.com/fxsml/gopipe-azservicebus
```

## Quick Start

### Subscriber Example

```go
package main

import (
    "context"
    "log"

    azservicebus "github.com/fxsml/gopipe-azservicebus"
)

type MyMessage struct {
    ID      string `json:"id"`
    Content string `json:"content"`
}

func main() {
    // Create Azure Service Bus client
    client, err := azservicebus.NewClient("Endpoint=sb://your-namespace.servicebus.windows.net/;...")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close(context.Background())

    // Create subscriber with default configuration
    subscriber := azservicebus.NewSubscriber[MyMessage](client, azservicebus.DefaultSubscriberConfig())
    defer subscriber.Close()

    // Subscribe to a queue
    ctx := context.Background()
    messages, err := subscriber.Subscribe(ctx, "myqueue")
    if err != nil {
        log.Fatal(err)
    }

    // Process messages
    for msg := range messages {
        log.Printf("Received: %s", msg.Payload.Content)

        // Acknowledge successful processing
        msg.Ack()

        // Or reject for redelivery
        // msg.Nack(err)
    }
}
```

### Publisher Example

```go
package main

import (
    "context"
    "log"
    "time"

    azservicebus "github.com/fxsml/gopipe-azservicebus"
    "github.com/fxsml/gopipe"
)

type MyMessage struct {
    ID      string `json:"id"`
    Content string `json:"content"`
}

func main() {
    // Create Azure Service Bus client
    client, err := azservicebus.NewClient("Endpoint=sb://your-namespace.servicebus.windows.net/;...")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close(context.Background())

    // Create publisher
    publisher := azservicebus.NewPublisher[MyMessage](client, azservicebus.DefaultPublisherConfig())
    defer publisher.Close()

    // Create message channel
    msgChan := make(chan *gopipe.Message[MyMessage], 10)

    // Publish messages
    ctx := context.Background()
    done, err := publisher.Publish(ctx, "myqueue", msgChan)
    if err != nil {
        log.Fatal(err)
    }

    // Send messages
    go func() {
        metadata := gopipe.Metadata{
            "message_id": "msg-1",
            "content_type": "application/json",
        }

        msg := gopipe.NewMessage(
            metadata,
            MyMessage{ID: "msg-1", Content: "Hello, Service Bus!"},
            time.Time{}, // No deadline
            func() { log.Println("Message acknowledged") },
            func(err error) { log.Printf("Message rejected: %v", err) },
        )

        msgChan <- msg
        close(msgChan)
    }()

    // Wait for completion
    <-done
}
```

## Configuration

### Subscriber Configuration

```go
config := azservicebus.SubscriberConfig{
    BatchSize:          10,               // Number of messages to receive per batch
    MessageTimeout:     60 * time.Second, // Max time for processing a message
    AbandonTimeout:     30 * time.Second, // Timeout for abandon operations
    CompleteTimeout:    30 * time.Second, // Timeout for complete operations
    CloseTimeout:       30 * time.Second, // Timeout for closing receivers
    ConcurrentMessages: 5,                // Number of concurrent messages to process
}

subscriber := azservicebus.NewSubscriber[MyMessage](client, config)
```

### Publisher Configuration

```go
config := azservicebus.PublisherConfig{
    PublishTimeout: 30 * time.Second, // Timeout for publish operations
    CloseTimeout:   30 * time.Second, // Timeout for closing senders
}

publisher := azservicebus.NewPublisher[MyMessage](client, config)
```

### Default Configurations

```go
// Use default configurations
subscriberConfig := azservicebus.DefaultSubscriberConfig()
publisherConfig := azservicebus.DefaultPublisherConfig()
```

## Authentication

### Connection String

```go
client, err := azservicebus.NewClient(
    "Endpoint=sb://your-namespace.servicebus.windows.net/;SharedAccessKeyName=...;SharedAccessKey=..."
)
```

### DefaultAzureCredential (Recommended for Production)

```go
client, err := azservicebus.NewClient("your-namespace.servicebus.windows.net")
```

This uses [DefaultAzureCredential](https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/azidentity#DefaultAzureCredential) which supports:
- Managed Identity
- Azure CLI credentials
- Environment variables
- And more...

## Topic Subscriptions

To subscribe to a topic subscription, use the format `"topic/subscription"`:

```go
messages, err := subscriber.Subscribe(ctx, "mytopic/mysubscription")
```

## Metadata Mapping

Service Bus properties are automatically mapped to gopipe.Metadata:

| Service Bus Property | Metadata Key      |
|---------------------|-------------------|
| MessageID           | message_id        |
| Subject             | subject           |
| CorrelationID       | correlation_id    |
| ContentType         | content_type      |
| To                  | to                |
| ReplyTo             | reply_to          |
| SessionID           | session_id        |
| ApplicationProperties | Preserved as-is |

When publishing, these metadata keys are automatically mapped back to Service Bus properties.

## Message Acknowledgment

### Ack (Success)
Call `msg.Ack()` to mark a message as successfully processed. This calls `CompleteMessage` on the Service Bus receiver, removing the message from the queue.

```go
msg.Ack()
```

### Nack (Failure)
Call `msg.Nack(err)` to reject a message. This calls `AbandonMessage` on the Service Bus receiver, allowing the message to be redelivered (subject to the queue's max delivery count).

```go
if err := processMessage(msg); err != nil {
    msg.Nack(err)
    continue
}
```

## Concurrent Processing

The subscriber supports concurrent message processing:

```go
config := azservicebus.DefaultSubscriberConfig()
config.ConcurrentMessages = 10 // Process up to 10 messages concurrently

subscriber := azservicebus.NewSubscriber[MyMessage](client, config)
```

Note: Downstream pipeline stages handle their own concurrency. The subscriber's `ConcurrentMessages` setting controls how many messages are being processed *at the receiver level* before being sent to the pipeline.

## Error Handling

- **Transient Errors**: Automatically retried by the Azure SDK
- **Processing Errors**: Call `msg.Nack(err)` to allow redelivery
- **Fatal Errors**: Close the subscriber/publisher to clean up resources
- **Dead Letter Queue**: Messages that exceed max delivery count are automatically moved to the dead-letter queue by Service Bus

## Graceful Shutdown

Both Subscriber and Publisher support graceful shutdown:

```go
// Close waits for in-flight messages to complete (with timeout)
subscriber.Close()
publisher.Close()
```

## Complete Pipeline Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    azservicebus "github.com/fxsml/gopipe-azservicebus"
    "github.com/fxsml/gopipe"
)

type InputMessage struct {
    ID   string `json:"id"`
    Text string `json:"text"`
}

type OutputMessage struct {
    ID        string `json:"id"`
    Processed string `json:"processed"`
}

func main() {
    client, _ := azservicebus.NewClient("Endpoint=sb://...")
    defer client.Close(context.Background())

    // Create subscriber for input queue
    subscriber := azservicebus.NewSubscriber[InputMessage](
        client,
        azservicebus.DefaultSubscriberConfig(),
    )
    defer subscriber.Close()

    // Create publisher for output queue
    publisher := azservicebus.NewPublisher[OutputMessage](
        client,
        azservicebus.DefaultPublisherConfig(),
    )
    defer publisher.Close()

    ctx := context.Background()

    // Subscribe to input
    inputMessages, _ := subscriber.Subscribe(ctx, "input-queue")

    // Create output channel
    outputMessages := make(chan *gopipe.Message[OutputMessage], 10)

    // Start publisher
    done, _ := publisher.Publish(ctx, "output-queue", outputMessages)

    // Process messages
    go func() {
        for msg := range inputMessages {
            // Transform message
            output := OutputMessage{
                ID:        msg.Payload.ID,
                Processed: fmt.Sprintf("Processed: %s", msg.Payload.Text),
            }

            // Forward with same acknowledgment
            outMsg := gopipe.NewMessage(
                msg.Metadata,
                output,
                msg.Deadline(),
                msg.Ack,
                msg.Nack,
            )

            outputMessages <- outMsg
        }
        close(outputMessages)
    }()

    <-done
}
```

## Architecture

This library follows the adapter pattern to integrate Azure Service Bus with gopipe:

- **Subscriber**: Pulls messages from Service Bus and converts them to `gopipe.Message[T]`
- **Publisher**: Accepts `gopipe.Message[T]` from a channel and publishes to Service Bus
- **Message Mapping**: Handles conversion between gopipe and Service Bus message formats
- **Concurrency**: Uses semaphores for controlled concurrent processing
- **Thread Safety**: Double-checked locking for sender/receiver management

## Requirements

- Go 1.21 or later
- Azure Service Bus namespace
- [gopipe](https://github.com/fxsml/gopipe) framework
- [Azure SDK for Go](https://github.com/Azure/azure-sdk-for-go)

## License

See LICENSE file for details.

## Contributing

Contributions are welcome! Please feel free to submit issues or pull requests.
