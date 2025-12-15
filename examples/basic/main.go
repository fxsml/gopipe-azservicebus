// Package main demonstrates basic Azure Service Bus publish/subscribe using gopipe-azservicebus.
//
// This example shows:
// - Creating a client with connection string from environment
// - Publishing messages using the Publisher
// - Subscribing to messages using the Subscriber
// - Proper message acknowledgment
//
// Prerequisites:
// - Set AZURE_SERVICEBUS_CONNECTION_STRING environment variable
// - Create a queue named "demo-queue" in your Azure Service Bus namespace
//
// Or use a .env file:
//
//	AZURE_SERVICEBUS_CONNECTION_STRING=Endpoint=sb://your-namespace.servicebus.windows.net/;SharedAccessKeyName=...;SharedAccessKey=...
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	servicebus "github.com/fxsml/gopipe-azservicebus"
	"github.com/fxsml/gopipe/message"
	"github.com/joho/godotenv"
)

// Order represents a sample order message
type Order struct {
	ID       string    `json:"id"`
	Customer string    `json:"customer"`
	Amount   float64   `json:"amount"`
	Created  time.Time `json:"created"`
}

func main() {
	// Load environment variables from .env file
	_ = godotenv.Load()

	connectionString := os.Getenv("AZURE_SERVICEBUS_CONNECTION_STRING")
	if connectionString == "" {
		log.Fatal("AZURE_SERVICEBUS_CONNECTION_STRING environment variable is required")
	}

	queueName := os.Getenv("AZURE_SERVICEBUS_QUEUE")
	if queueName == "" {
		queueName = "demo-queue"
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutdown signal received")
		cancel()
	}()

	// Create Azure Service Bus client
	client, err := servicebus.NewClient(connectionString)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(context.Background())

	log.Printf("Connected to Azure Service Bus")

	// Run publisher and subscriber
	if err := runDemo(ctx, client, queueName); err != nil {
		log.Fatalf("Demo failed: %v", err)
	}
}

func runDemo(ctx context.Context, client *azservicebus.Client, queueName string) error {
	// Create publisher
	publisher := servicebus.NewPublisher(client, servicebus.PublisherConfig{
		BatchSize:    5,
		BatchTimeout: 100 * time.Millisecond,
		SendTimeout:  30 * time.Second,
	})
	defer publisher.Close()

	// Create subscriber
	subscriber := servicebus.NewSubscriber(client, servicebus.SubscriberConfig{
		ReceiveTimeout:  10 * time.Second,
		MaxMessageCount: 10,
		BufferSize:      100,
		PollInterval:    1 * time.Second,
	})
	defer subscriber.Close()

	// Start subscriber in background
	msgs, err := subscriber.Subscribe(ctx, queueName)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	// Process received messages
	go func() {
		for msg := range msgs {
			var order Order
			if err := json.Unmarshal(msg.Payload(), &order); err != nil {
				log.Printf("Failed to unmarshal message: %v", err)
				msg.Nack(err)
				continue
			}
			log.Printf("Received order: ID=%s, Customer=%s, Amount=$%.2f", order.ID, order.Customer, order.Amount)
			msg.Ack()
		}
		log.Println("Subscriber channel closed")
	}()

	// Publish sample orders
	pubMsgs := make(chan *message.Message[[]byte])
	done, err := publisher.Publish(ctx, queueName, pubMsgs)
	if err != nil {
		return fmt.Errorf("failed to start publishing: %w", err)
	}

	// Send sample orders
	orders := []Order{
		{ID: "ORD-001", Customer: "Alice", Amount: 99.99, Created: time.Now()},
		{ID: "ORD-002", Customer: "Bob", Amount: 149.50, Created: time.Now()},
		{ID: "ORD-003", Customer: "Charlie", Amount: 250.00, Created: time.Now()},
	}

	go func() {
		defer close(pubMsgs)
		for _, order := range orders {
			body, err := json.Marshal(order)
			if err != nil {
				log.Printf("Failed to marshal order: %v", err)
				continue
			}
			msg := message.New(body,
				message.WithID[[]byte](order.ID),
				message.WithProperty[[]byte]("customer", order.Customer),
			)
			select {
			case pubMsgs <- msg:
				log.Printf("Published order: %s", order.ID)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for publishing to complete
	<-done
	log.Println("Publishing complete")

	// Wait a bit for messages to be processed
	time.Sleep(5 * time.Second)

	return nil
}
