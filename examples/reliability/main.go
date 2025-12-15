// Package main demonstrates reliability features with Azure Service Bus and gopipe.
//
// This example shows:
// - Retry configuration with exponential backoff using gopipe
// - Proper Ack/Nack handling for message delivery guarantees
// - Dead-letter queue handling
// - Graceful shutdown with message completion
// - Error recovery patterns
//
// Prerequisites:
// - Set AZURE_SERVICEBUS_CONNECTION_STRING environment variable
// - Create a queue named "reliable-queue" with dead-letter enabled
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe"
	servicebus "github.com/fxsml/gopipe-azservicebus"
	"github.com/fxsml/gopipe/message"
	"github.com/joho/godotenv"
)

// Task represents a processing task
type Task struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Payload   string `json:"payload"`
	Retryable bool   `json:"retryable"`
}

// ErrTransient represents a transient error that can be retried
var ErrTransient = errors.New("transient error")

// ErrPermanent represents a permanent error that should not be retried
var ErrPermanent = errors.New("permanent error")

func main() {
	_ = godotenv.Load()

	connectionString := os.Getenv("AZURE_SERVICEBUS_CONNECTION_STRING")
	if connectionString == "" {
		log.Fatal("AZURE_SERVICEBUS_CONNECTION_STRING is required")
	}

	queueName := os.Getenv("AZURE_SERVICEBUS_QUEUE")
	if queueName == "" {
		queueName = "reliable-queue"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v, initiating graceful shutdown...", sig)
		cancel()
	}()

	client, err := servicebus.NewClientWithConfig(connectionString, servicebus.ClientConfig{
		MaxRetries:    5,
		RetryDelay:    time.Second,
		MaxRetryDelay: 30 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(context.Background())

	log.Println("Connected to Azure Service Bus with retry configuration")

	if err := runReliabilityDemo(ctx, client, queueName); err != nil {
		log.Fatalf("Demo failed: %v", err)
	}
}

func runReliabilityDemo(ctx context.Context, client *azservicebus.Client, queueName string) error {
	// ========================================
	// Publish tasks
	// ========================================

	log.Println("=== Publishing Tasks ===")

	publisher := servicebus.NewPublisher(client, servicebus.PublisherConfig{
		BatchSize:    10,
		BatchTimeout: 100 * time.Millisecond,
		SendTimeout:  30 * time.Second,
	})
	defer publisher.Close()

	// Create tasks with different characteristics
	tasks := []Task{
		{ID: "task-001", Type: "process", Payload: "data-1", Retryable: true},
		{ID: "task-002", Type: "validate", Payload: "data-2", Retryable: true},
		{ID: "task-003", Type: "transform", Payload: "data-3", Retryable: false}, // Will fail permanently
		{ID: "task-004", Type: "process", Payload: "data-4", Retryable: true},
		{ID: "task-005", Type: "notify", Payload: "data-5", Retryable: true},
	}

	msgs := make([]*message.Message[[]byte], len(tasks))
	for i, task := range tasks {
		body, _ := json.Marshal(task)
		msgs[i] = message.New(body,
			message.WithID[[]byte](task.ID),
			message.WithProperty[[]byte]("task_type", task.Type),
			message.WithProperty[[]byte]("retryable", task.Retryable),
		)
	}

	if err := publisher.PublishSync(ctx, queueName, msgs); err != nil {
		return fmt.Errorf("failed to publish tasks: %w", err)
	}
	log.Printf("Published %d tasks", len(tasks))

	time.Sleep(1 * time.Second)

	// ========================================
	// Process tasks with reliability
	// ========================================

	log.Println("\n=== Processing Tasks with Retry Logic ===")

	subscriber := servicebus.NewSubscriber(client, servicebus.SubscriberConfig{
		ReceiveTimeout:  10 * time.Second,
		AckTimeout:      30 * time.Second,
		MaxMessageCount: 5,
		BufferSize:      50,
		PollInterval:    1 * time.Second,
	})
	defer subscriber.Close()

	incomingMsgs, err := subscriber.Subscribe(ctx, queueName)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	// Statistics
	var processed, succeeded, failed atomic.Int32

	// Create processor with retry configuration
	processPipe := gopipe.NewTransformPipe(
		func(ctx context.Context, msg *message.Message[[]byte]) (string, error) {
			processed.Add(1)

			var task Task
			if err := json.Unmarshal(msg.Payload(), &task); err != nil {
				msg.Nack(err)
				failed.Add(1)
				return "", fmt.Errorf("unmarshal error: %w", err)
			}

			// Simulate processing with potential failures
			result, err := processTask(task)
			if err != nil {
				if errors.Is(err, ErrTransient) && task.Retryable {
					// Transient error - nack to retry
					log.Printf("⚠️  Task %s: transient error, will retry", task.ID)
					msg.Nack(err)
					return "", err
				}
				// Permanent error - acknowledge but log failure
				log.Printf("❌ Task %s: permanent failure: %v", task.ID, err)
				msg.Ack() // Ack to prevent infinite retry
				failed.Add(1)
				return "", err
			}

			// Success - acknowledge
			msg.Ack()
			succeeded.Add(1)
			log.Printf("✅ Task %s: %s", task.ID, result)
			return result, nil
		},
		gopipe.WithConcurrency[*message.Message[[]byte], string](2),
		gopipe.WithRetryConfig[*message.Message[[]byte], string](gopipe.RetryConfig{
			MaxAttempts: 3,
			Backoff:     gopipe.ExponentialBackoff(100*time.Millisecond, 2.0, 5*time.Second, 0.1),
			ShouldRetry: func(err error) bool {
				return errors.Is(err, ErrTransient)
			},
			Timeout: 30 * time.Second,
		}),
	)

	// Create input channel for processor
	processInput := make(chan *message.Message[[]byte], 20)
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

	// Start processing
	results := processPipe.Start(ctx, processInput)

	// Collect results
	timeout := time.After(30 * time.Second)
loop:
	for {
		select {
		case result, ok := <-results:
			if !ok {
				break loop
			}
			if result != "" {
				log.Printf("Result: %s", result)
			}
		case <-timeout:
			log.Println("Processing timeout")
			break loop
		case <-ctx.Done():
			break loop
		}
	}

	log.Printf("\n=== Summary ===")
	log.Printf("Processed: %d", processed.Load())
	log.Printf("Succeeded: %d", succeeded.Load())
	log.Printf("Failed: %d", failed.Load())

	return nil
}

// processTask simulates task processing with potential failures
func processTask(task Task) (string, error) {
	// Simulate processing time
	time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)

	// Simulate different outcomes based on task type
	switch task.Type {
	case "transform":
		// This task type always fails permanently
		if !task.Retryable {
			return "", fmt.Errorf("%w: transform tasks are not supported", ErrPermanent)
		}
	case "process":
		// 30% chance of transient failure
		if rand.Float32() < 0.3 {
			return "", fmt.Errorf("%w: temporary processing failure", ErrTransient)
		}
	case "validate":
		// 20% chance of transient failure
		if rand.Float32() < 0.2 {
			return "", fmt.Errorf("%w: validation service unavailable", ErrTransient)
		}
	}

	return fmt.Sprintf("completed_%s_%s", task.Type, task.ID), nil
}
