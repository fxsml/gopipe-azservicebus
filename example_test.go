package azservicebus_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/fxsml/gopipe"
	azservicebus "github.com/fxsml/gopipe-azservicebus"
)

// Message represents a sample message type
type Message struct {
	ID      string    `json:"id"`
	Content string    `json:"content"`
	Time    time.Time `json:"time"`
}

func ExampleSubscriber() {
	// Create Azure Service Bus client
	client, err := azservicebus.NewClient("Endpoint=sb://your-namespace.servicebus.windows.net/;SharedAccessKeyName=...;SharedAccessKey=...")
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close(context.Background())

	// Create subscriber with custom configuration
	config := azservicebus.DefaultSubscriberConfig()
	config.BatchSize = 10
	config.ConcurrentMessages = 5
	config.MessageTimeout = 60 * time.Second

	subscriber := azservicebus.NewSubscriber[Message](client, config)
	defer subscriber.Close()

	// Subscribe to a queue
	ctx := context.Background()
	messages, err := subscriber.Subscribe(ctx, "myqueue")
	if err != nil {
		log.Fatal(err)
	}

	// Process messages
	for msg := range messages {
		fmt.Printf("Received message: %s - %s\n", msg.Payload.ID, msg.Payload.Content)

		// Process the message
		if err := processMessage(msg.Payload); err != nil {
			// Nack on error - message will be redelivered
			msg.Nack(err)
			continue
		}

		// Ack on success
		msg.Ack()
	}
}

func ExampleSubscriber_topicSubscription() {
	client, err := azservicebus.NewClient("Endpoint=sb://your-namespace.servicebus.windows.net/;...")
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close(context.Background())

	subscriber := azservicebus.NewSubscriber[Message](client, azservicebus.DefaultSubscriberConfig())
	defer subscriber.Close()

	// Subscribe to a topic subscription using "topic/subscription" format
	ctx := context.Background()
	messages, err := subscriber.Subscribe(ctx, "mytopic/mysubscription")
	if err != nil {
		log.Fatal(err)
	}

	for msg := range messages {
		fmt.Printf("Received from topic: %s\n", msg.Payload.Content)
		msg.Ack()
	}
}

func ExamplePublisher() {
	// Create Azure Service Bus client
	client, err := azservicebus.NewClient("Endpoint=sb://your-namespace.servicebus.windows.net/;...")
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close(context.Background())

	// Create publisher with custom configuration
	config := azservicebus.DefaultPublisherConfig()
	config.PublishTimeout = 30 * time.Second

	publisher := azservicebus.NewPublisher[Message](client, config)
	defer publisher.Close()

	// Create a channel for messages
	msgChan := make(chan *gopipe.Message[Message], 10)

	// Publish messages
	ctx := context.Background()
	done, err := publisher.Publish(ctx, "myqueue", msgChan)
	if err != nil {
		log.Fatal(err)
	}

	// Send messages
	go func() {
		for i := 0; i < 5; i++ {
			metadata := gopipe.Metadata{
				"message_id":     fmt.Sprintf("msg-%d", i),
				"correlation_id": "correlation-123",
				"content_type":   "application/json",
			}

			msg := gopipe.NewMessage(
				metadata,
				Message{
					ID:      fmt.Sprintf("msg-%d", i),
					Content: fmt.Sprintf("Message content %d", i),
					Time:    time.Now(),
				},
				time.Time{}, // No deadline
				func() { fmt.Println("Message acknowledged") },
				func(err error) { fmt.Printf("Message rejected: %v\n", err) },
			)

			msgChan <- msg
		}
		close(msgChan)
	}()

	// Wait for all messages to be published
	<-done
	fmt.Println("All messages published")
}

func ExamplePublisher_withPipeline() {
	client, err := azservicebus.NewClient("Endpoint=sb://your-namespace.servicebus.windows.net/;...")
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close(context.Background())

	// Create subscriber and publisher
	subscriber := azservicebus.NewSubscriber[Message](client, azservicebus.DefaultSubscriberConfig())
	defer subscriber.Close()

	publisher := azservicebus.NewPublisher[Message](client, azservicebus.DefaultPublisherConfig())
	defer publisher.Close()

	ctx := context.Background()

	// Subscribe to input queue
	inputMessages, err := subscriber.Subscribe(ctx, "input-queue")
	if err != nil {
		log.Fatal(err)
	}

	// Create output channel for processed messages
	outputMessages := make(chan *gopipe.Message[Message], 10)

	// Start publisher for output queue
	publishDone, err := publisher.Publish(ctx, "output-queue", outputMessages)
	if err != nil {
		log.Fatal(err)
	}

	// Process messages in a pipeline
	go func() {
		for msg := range inputMessages {
			// Transform the message
			processed := Message{
				ID:      msg.Payload.ID,
				Content: fmt.Sprintf("Processed: %s", msg.Payload.Content),
				Time:    time.Now(),
			}

			// Create output message with same metadata
			outMsg := gopipe.NewMessage(
				msg.Metadata,
				processed,
				time.Time{},
				msg.Ack,
				msg.Nack,
			)

			outputMessages <- outMsg
		}
		close(outputMessages)
	}()

	// Wait for completion
	<-publishDone
}

func processMessage(msg Message) error {
	// Simulate message processing
	return nil
}
