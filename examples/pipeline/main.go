// Package main demonstrates a full gopipe pipeline with Azure Service Bus integration.
//
// This example shows:
// - Using gopipe's Generator, Processor, and Sink patterns
// - Message transformation through pipeline stages
// - Content-based routing to different queues using Sender
// - Receiving from multiple sources using Fan-In pattern
// - Proper error handling and retries
//
// Prerequisites:
// - Set AZURE_SERVICEBUS_CONNECTION_STRING environment variable
// - Create queues: "orders-queue", "high-value-orders", "standard-orders"
//
// Architecture:
//
//	[Order Generator] -> [Validation] -> [Router/Sender] -> [high-value-orders]
//	                                                     -> [standard-orders]
//
//	[Fan-In Receivers] <- [high-value-orders]
//	                   <- [standard-orders]
//	                   -> [Order Processor] -> [Notification Sink]
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

func runPipeline(ctx context.Context, client *servicebus.Client) error {
	// ========================================
	// PART 1: Publishing Pipeline with Routing
	// ========================================

	log.Println("=== Starting Publishing Pipeline ===")

	// Create sender for routing to multiple destinations
	sender := servicebus.NewSender(client, servicebus.SenderConfig{
		SendTimeout: 30 * time.Second,
	})
	defer sender.Close()

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
		func(ctx context.Context, order Order) (*message.Message, error) {
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
			msg := message.New(body, message.Attributes{
				message.AttrID: order.ID,
				"customer":     order.Customer,
				"priority":     order.Priority,
				"amount":       order.Amount,
			})

			log.Printf("Validated order: %s (Amount: $%.2f)", order.ID, order.Amount)
			return msg, nil
		},
		gopipe.WithConcurrency[Order, *message.Message](2),
	)

	validatedMsgs := validationPipe.Start(ctx, orderChan)

	// Route messages based on amount
	for msg := range validatedMsgs {
		// Determine target queue based on amount
		targetQueue := standardQueue
		if amount, ok := msg.Attributes["amount"]; ok {
			if amt, ok := amount.(float64); ok && amt >= highValueThreshold {
				targetQueue = highValueQueue
			}
		}

		// Send to appropriate queue
		if err := sender.Send(ctx, targetQueue, []*message.Message{msg}); err != nil {
			log.Printf("Failed to send order to %s: %v", targetQueue, err)
			continue
		}
		log.Printf("Routed order to %s", targetQueue)
	}

	log.Println("Publishing complete")

	// Wait for messages to propagate
	time.Sleep(2 * time.Second)

	// ========================================
	// PART 2: Subscription Pipeline using Fan-In
	// ========================================

	log.Println("\n=== Starting Subscription Pipeline ===")

	// Create receivers for both queues
	highValueReceiver := servicebus.NewReceiver(client, servicebus.ReceiverConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 10,
	})
	defer highValueReceiver.Close()

	standardReceiver := servicebus.NewReceiver(client, servicebus.ReceiverConfig{
		ReceiveTimeout:  5 * time.Second,
		MaxMessageCount: 10,
	})
	defer standardReceiver.Close()

	// Create generators for both queues
	highValueGen := servicebus.NewMessageGenerator(highValueReceiver, highValueQueue)
	standardGen := servicebus.NewMessageGenerator(standardReceiver, standardQueue)

	// Start generators
	highValueMsgs := highValueGen.Generate(ctx)
	standardMsgs := standardGen.Generate(ctx)

	// Merge using gopipe FanIn
	fanIn := gopipe.NewFanIn[*message.Message](gopipe.FanInConfig{})
	fanIn.Add(highValueMsgs)
	fanIn.Add(standardMsgs)
	incomingMsgs := fanIn.Start(ctx)

	// Process orders
	processPipe := gopipe.NewTransformPipe(
		func(ctx context.Context, msg *message.Message) (ProcessedOrder, error) {
			var order Order
			if err := json.Unmarshal(msg.Data, &order); err != nil {
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
		gopipe.WithConcurrency[*message.Message, ProcessedOrder](3),
	)

	processedOrders := processPipe.Start(ctx, incomingMsgs)

	// Sink - final notification/logging
	notificationSink := channel.Sink(processedOrders, func(order ProcessedOrder) {
		log.Printf("Processed: %s | Customer: %s | Amount: $%.2f | Category: %s",
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
