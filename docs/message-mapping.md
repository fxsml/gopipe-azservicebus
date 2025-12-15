# Message Mapping Reference

This document describes how gopipe messages map to Azure Service Bus messages and provides context on CloudEvents compatibility.

## Protocol Background

### Azure Service Bus AMQP Protocol

Azure Service Bus uses **AMQP 1.0** as its primary protocol ([ISO/IEC 19464:2014](https://www.iso.org/standard/64955.html)). The Service Bus message model maps to standard AMQP 1.0 message sections:

- **Properties section**: Standard AMQP properties like `message-id`, `correlation-id`, `content-type`, `subject`, etc.
- **Application Properties section**: Custom key-value pairs for application-defined metadata
- **Message Annotations section**: Service Bus-specific metadata like `x-opt-enqueued-time`, `x-opt-sequence-number`
- **Application Data section**: The message body/payload

For the complete Azure Service Bus AMQP mapping, see:
- [AMQP 1.0 in Azure Service Bus Protocol Guide](https://learn.microsoft.com/en-us/azure/service-bus-messaging/service-bus-amqp-protocol-guide)

### CloudEvents AMQP Protocol Binding

The [CloudEvents AMQP Protocol Binding](https://github.com/cloudevents/spec/blob/main/cloudevents/bindings/amqp-protocol-binding.md) defines how CloudEvents map to AMQP 1.0 messages. It supports two modes:

1. **Binary Mode**: Event data in the `application-data` section, CloudEvents attributes in `application-properties` with a `cloudEvents_` or `cloudEvents:` prefix
2. **Structured Mode**: Complete event (metadata + data) serialized as JSON in `application-data` with `content-type: application/cloudevents+json`

### Why Not Use CloudEvents AMQP Binding Directly?

Azure Service Bus uses **native AMQP 1.0 properties** rather than the CloudEvents AMQP binding convention:

| Aspect | CloudEvents AMQP Binding | Azure Service Bus |
|--------|--------------------------|-------------------|
| ID location | `application-properties["cloudEvents_id"]` | `properties.message-id` |
| Subject location | `application-properties["cloudEvents_subject"]` | `properties.subject` |
| Content type | `application-properties["cloudEvents_datacontenttype"]` | `properties.content-type` |
| Custom attributes | `application-properties["cloudEvents_*"]` | `application-properties` (no prefix) |

This library uses **Azure Service Bus native mapping** for direct compatibility with the Azure SDK and existing Service Bus tooling. For CloudEvents structured mode (JSON envelope), use the `application/cloudevents+json` content type and serialize CloudEvents directly in the message body.

## Attribute Mapping

### gopipe → Azure Service Bus (Outbound)

When sending messages, gopipe `message.Attributes` map to Azure Service Bus properties:

| gopipe Attribute | Azure Service Bus Property | AMQP Section | Notes |
|------------------|---------------------------|--------------|-------|
| `message.AttrID` | `MessageID` | properties.message-id | Application-defined unique identifier |
| `message.AttrCorrelationID` | `CorrelationID` | properties.correlation-id | Request-response correlation |
| `message.AttrSubject` | `Subject` | properties.subject | Message purpose/category |
| `message.AttrDataContentType` | `ContentType` | properties.content-type | MIME type of message body |
| `message.AttrSource` | `ReplyTo` | properties.reply-to | Origin/source identifier |
| `"to"` | `To` | properties.to | Destination identifier |
| `"replyToSessionId"` | `ReplyToSessionId` | properties.reply-to-group-id | Session-aware reply routing |
| `"sessionId"` | `SessionId` | properties.group-id | Session identifier |
| `"ttl"` | `TimeToLive` | header.ttl | Message expiration (time.Duration) |
| `"partitionKey"` | `PartitionKey` | annotations.x-opt-partition-key | Partition routing key |
| `"scheduledEnqueueTime"` | `ScheduledEnqueueTime` | annotations.x-opt-scheduled-enqueue-time | Delayed delivery |
| *(other keys)* | `ApplicationProperties[key]` | application-properties | Custom metadata |

### Azure Service Bus → gopipe (Inbound)

When receiving messages, Azure Service Bus properties map to gopipe `message.Attributes`:

| Azure Service Bus Property | gopipe Attribute | Notes |
|---------------------------|------------------|-------|
| `MessageID` | `message.AttrID` | May be empty if not set by sender |
| `CorrelationID` | `message.AttrCorrelationID` | |
| `Subject` | `message.AttrSubject` | |
| `ContentType` | `message.AttrDataContentType` | |
| `EnqueuedTime` | `message.AttrTime` | Service-defined timestamp |
| `ReplyTo` | `message.AttrSource` | Mapped to source for CloudEvents alignment |
| `To` | `"to"` | |
| `TimeToLive` | `"ttl"` | |
| `DeliveryCount` | `"deliveryCount"` | Number of delivery attempts |
| `LockedUntil` | `"lockedUntil"` | Lock expiration time |
| `SequenceNumber` | `"sequenceNumber"` | Service-assigned sequence |
| `PartitionKey` | `"partitionKey"` | |
| `SessionId` | `"sessionId"` | |
| `ReplyToSessionId` | `"replyToSessionId"` | |
| `ApplicationProperties[*]` | *(same key)* | Preserved as-is |

## CloudEvents Compatibility

### Required CloudEvents Attributes

The [CloudEvents Specification](https://github.com/cloudevents/spec/blob/main/cloudevents/spec.md) defines these required attributes:

| CloudEvents Attribute | Type | gopipe Attribute | Service Bus Mapping |
|----------------------|------|------------------|---------------------|
| `id` | String | `message.AttrID` | `MessageID` |
| `source` | URI-reference | `message.AttrSource` | `ReplyTo` |
| `specversion` | String | `"specversion"` | `ApplicationProperties["specversion"]` |
| `type` | String | `message.AttrType` | `ApplicationProperties["type"]` |

### Optional CloudEvents Attributes

| CloudEvents Attribute | Type | gopipe Attribute | Service Bus Mapping |
|----------------------|------|------------------|---------------------|
| `datacontenttype` | String | `message.AttrDataContentType` | `ContentType` |
| `dataschema` | URI | `"dataschema"` | `ApplicationProperties["dataschema"]` |
| `subject` | String | `message.AttrSubject` | `Subject` |
| `time` | Timestamp | `message.AttrTime` | `EnqueuedTime` (read-only) |

### CloudEvents Structured Mode

For full CloudEvents compatibility using structured mode, serialize the entire CloudEvent as JSON:

```go
import (
    "encoding/json"
    "github.com/fxsml/gopipe/message"
)

// CloudEvent represents a CloudEvents 1.0 event
type CloudEvent struct {
    SpecVersion     string         `json:"specversion"`
    Type            string         `json:"type"`
    Source          string         `json:"source"`
    ID              string         `json:"id"`
    Time            string         `json:"time,omitempty"`
    DataContentType string         `json:"datacontenttype,omitempty"`
    Subject         string         `json:"subject,omitempty"`
    Data            any            `json:"data,omitempty"`
}

// Create a CloudEvents structured message
event := CloudEvent{
    SpecVersion:     "1.0",
    Type:            "com.example.order.created",
    Source:          "/orders/service",
    ID:              "order-123",
    DataContentType: "application/json",
    Data:            orderData,
}

body, _ := json.Marshal(event)
msg := message.New(body, message.Attributes{
    message.AttrDataContentType: "application/cloudevents+json",
})

sender.Send(ctx, "my-queue", []*message.Message{msg})
```

## Type Mapping

### Outbound (gopipe → Service Bus)

| Go Type | Service Bus Type | Notes |
|---------|------------------|-------|
| `string` | `string` | Direct mapping |
| `int`, `int64` | `int64` | Numeric types |
| `float64` | `double` | Floating point |
| `bool` | `bool` | Boolean |
| `time.Time` | `timestamp` | UTC timestamp |
| `time.Duration` | `time.Duration` | For TTL only |
| `[]byte` | `binary` | Binary data |

### Inbound (Service Bus → gopipe)

| Service Bus Type | Go Type | Notes |
|------------------|---------|-------|
| `string` | `string` | Direct mapping |
| `int64` | `int64` | Numeric |
| `double` | `float64` | Floating point |
| `bool` | `bool` | Boolean |
| `timestamp` | `time.Time` | Timestamps |
| `binary` | `[]byte` | Binary data |

## Service Bus-Specific Properties

These properties are managed by Azure Service Bus and are read-only on received messages:

| Property | Description |
|----------|-------------|
| `EnqueuedTime` | UTC time when the message was enqueued |
| `SequenceNumber` | Unique 64-bit sequence number assigned by Service Bus |
| `DeliveryCount` | Number of times this message has been delivered |
| `LockedUntil` | UTC time until which the message is locked for this receiver |
| `DeadLetterSource` | Original queue/subscription if received from dead-letter queue |

## References

- [CloudEvents Specification v1.0](https://github.com/cloudevents/spec/blob/main/cloudevents/spec.md)
- [CloudEvents AMQP Protocol Binding](https://github.com/cloudevents/spec/blob/main/cloudevents/bindings/amqp-protocol-binding.md)
- [Azure Service Bus AMQP Protocol Guide](https://learn.microsoft.com/en-us/azure/service-bus-messaging/service-bus-amqp-protocol-guide)
- [Azure Service Bus Messages and Payloads](https://learn.microsoft.com/en-us/azure/service-bus-messaging/service-bus-messages-payloads)
- [CloudEvents with Azure Service Bus (.NET Sample)](https://github.com/Azure/azure-sdk-for-net/blob/main/sdk/servicebus/Azure.Messaging.ServiceBus/samples/Sample11_CloudEvents.md)
