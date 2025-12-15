// Package main demonstrates a full gopipe pipeline with Azure Service Bus integration.
//
// This example shows:
// - Using gopipe's Generator, Processor, and Sink patterns
// - Message transformation through pipeline stages
// - Content-based routing to different queues
// - Multi-subscriber for merging messages from multiple sources
// - Proper error handling and retries
//
// Prerequisites:
// - Set AZURE_SERVICEBUS_CONNECTION_STRING environment variable
// - Create queues: "orders-queue", "high-value-orders", "standard-orders"
//
// Architecture:
//
//	[Order Generator] -> [Validation] -> [Router] -> [high-value-orders]
//	                                             -> [standard-orders]
//
//	[MultiSubscriber] <- [high-value-orders]
//	                  <- [standard-orders]
//	                  -> [Order Processor] -> [Notification Sink]
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
	"github.com/fxsml/gopipe"
	servicebus "github.com/fxsml/gopipe-azservicebus"
	"github.com/fxsml/gopipe/channel"
	"github.com/fxsml/gopipe/message"
	"github.com/joho/godotenv"
)

// Order represents an incoming order
type Order struct {
	ID        string    `json:"id"`
	Customer  string    `json:"customer"`
	Amount    float64   `json:"amount"`
	Priority  string    `json:"priority"`
	CreatedAt time.Time `json:"created_at"`
}

// ProcessedOrder represents an order after processing
type ProcessedOrder struct {
	Order
	ProcessedAt time.Time `json:"processed_at"`
	Status      string    `json:"status"`
	Category    string    `json:"category"`
}

const (
	highValueQueue     = "high-value-orders"
	standardQueue      = "standard-orders"
	highValueThreshold = 500.0
)

func main() {
	_ = godotenv.Load()

	connectionString := os.Getenv("AZURE_SERVICEBUS_CONNECTION_STRING")
	if connectionString == "" {
		log.Fatal("AZURE_SERVICEBUS_CONNECTION_STRING is required")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutting down...")
		cancel()
	}()

	// Create client
	client, err := servicebus.NewClient(connectionString)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(context.Background())

	log.Println("Connected to Azure Service Bus")

	// Run the pipeline demo
	if err := runPipeline(ctx, client); err != nil {
		log.Fatalf("Pipeline failed: %v", err)
	}
}

func runPipeline(ctx context.Context, client *azservicebus.Client) error {
	// ========================================
	// PART 1: Publishing Pipeline
	// ========================================

	log.Println("=== Starting Publishing Pipeline ===")

	// Create multi-publisher for routing
	multiPub := servicebus.NewMultiPublisher(client, servicebus.MultiPublisherConfig{
		PublisherConfig: servicebus.PublisherConfig{
			BatchSize:    5,
			BatchTimeout: 100 * time.Millisecond,
		},
	})
	defer multiPub.Close()

	// Generate sample orders using gopipe channel utilities
	orders := []Order{
		{ID: "ORD-001", Customer: "Alice", Amount: 150.00, Priority: "normal", CreatedAt: time.Now()},
		{ID: "ORD-002", Customer: "Bob", Amount: 750.00, Priority: "high", CreatedAt: time.Now()},
		{ID: "ORD-003", Customer: "Charlie", Amount: 50.00, Priority: "low", CreatedAt: time.Now()},
		{ID: "ORD-004", Customer: "Diana", Amount: 1200.00, Priority: "high", CreatedAt: time.Now()},
		{ID: "ORD-005", Customer: "Eve", Amount: 300.00, Priority: "normal", CreatedAt: time.Now()},
	}

	// Transform orders to messages using gopipe
	orderChan := channel.FromSlice(orders)

	// Validate and transform orders
	validationPipe := gopipe.NewTransformPipe(
		func(ctx context.Context, order Order) (*message.Message[[]byte], error) {
			// Validate order
			if order.Amount <= 0 {
				return nil, fmt.Errorf("invalid order amount: %f", order.Amount)
			}
			if order.Customer == "" {
				return nil, fmt.Errorf("customer name required")
			}

			// Marshal to JSON
			body, err := json.Marshal(order)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal order: %w", err)
			}

			// Create message with properties
			msg := message.New(body,
				message.WithID[[]byte](order.ID),
				message.WithProperty[[]byte]("customer", order.Customer),
				message.WithProperty[[]byte]("priority", order.Priority),
				message.WithProperty[[]byte]("amount", order.Amount),
			)

			log.Printf("Validated order: %s (Amount: $%.2f)", order.ID, order.Amount)
			return msg, nil
		},
		gopipe.WithConcurrency[Order, *message.Message[[]byte]](2),
	)

	validatedMsgs := validationPipe.Start(ctx, orderChan)

	// Create buffered channel for multi-publisher
	pubMsgs := make(chan *message.Message[[]byte], 10)
	go func() {
		defer close(pubMsgs)
		for msg := range validatedMsgs {
			select {
			case pubMsgs <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Router function - route based on order amount
	router := func(msg *message.Message[[]byte]) string {
		if amount, ok := msg.Properties().Get("amount"); ok {
			if amt, ok := amount.(float64); ok && amt >= highValueThreshold {
				return highValueQueue
			}
		}
		return standardQueue
	}

	// Publish with routing
	pubDone, err := multiPub.Publish(ctx, pubMsgs, router)
	if err != nil {
		return fmt.Errorf("failed to start multi-publishing: %w", err)
	}

	<-pubDone
	log.Println("Publishing complete")

	// Wait for messages to propagate
	time.Sleep(2 * time.Second)

	// ========================================
	// PART 2: Subscription Pipeline
	// ========================================

	log.Println("\n=== Starting Subscription Pipeline ===")

	// Create multi-subscriber for both queues
	multiSub := servicebus.NewMultiSubscriber(client, servicebus.MultiSubscriberConfig{
		SubscriberConfig: servicebus.SubscriberConfig{
			ReceiveTimeout:  5 * time.Second,
			MaxMessageCount: 10,
			BufferSize:      50,
			PollInterval:    500 * time.Millisecond,
		},
		MergeBufferSize: 100,
	})
	defer multiSub.Close()

	// Subscribe to both queues
	incomingMsgs, err := multiSub.Subscribe(ctx, highValueQueue, standardQueue)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	// Create a channel for gopipe processing
	processInput := make(chan *message.Message[[]byte], 50)
	go func() {
		defer close(processInput)
		for msg := range incomingMsgs {
			select {
			case processInput <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Process orders
	processPipe := gopipe.NewTransformPipe(
		func(ctx context.Context, msg *message.Message[[]byte]) (ProcessedOrder, error) {
			var order Order
			if err := json.Unmarshal(msg.Payload(), &order); err != nil {
				msg.Nack(err)
				return ProcessedOrder{}, err
			}

			// Determine category
			category := "standard"
			if order.Amount >= highValueThreshold {
				category = "high-value"
			}

			processed := ProcessedOrder{
				Order:       order,
				ProcessedAt: time.Now(),
				Status:      "processed",
				Category:    category,
			}

			// Acknowledge the original message
			msg.Ack()

			return processed, nil
		},
		gopipe.WithConcurrency[*message.Message[[]byte], ProcessedOrder](3),
	)

	processedOrders := processPipe.Start(ctx, processInput)

	// Sink - final notification/logging
	notificationSink := channel.Sink(processedOrders, func(order ProcessedOrder) {
		log.Printf("📦 Processed: %s | Customer: %s | Amount: $%.2f | Category: %s",
			order.ID, order.Customer, order.Amount, order.Category)
	})

	// Wait for processing with timeout
	select {
	case <-notificationSink:
		log.Println("All orders processed")
	case <-time.After(15 * time.Second):
		log.Println("Timeout - some orders may not have been processed")
	case <-ctx.Done():
		log.Println("Context cancelled")
	}

	return nil
}
