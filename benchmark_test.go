package azservicebus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus/admin"
	"github.com/fxsml/gopipe"
	"github.com/fxsml/gopipe/message"
	"github.com/joho/godotenv"
)

// BenchmarkMessage is a simple test message for benchmarking
type BenchmarkMessage struct {
	ID      int    `json:"id"`
	Payload string `json:"payload"`
}

// BenchmarkSenderDirect benchmarks the Sender.Send method directly (synchronous)
func BenchmarkSenderDirect(b *testing.B) {
	helper := NewTestHelperB(b)
	defer helper.Cleanup()

	ctx := context.Background()
	queueName := GenerateTestNameB(b, "bench-sender-direct")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	sender := NewSender(client, SenderConfig{
		SendTimeout: 30 * time.Second,
	})
	defer sender.Close()

	// Prepare messages
	messages := make([]*message.Message[[]byte], b.N)
	for i := range b.N {
		msg := BenchmarkMessage{ID: i, Payload: "benchmark-payload"}
		body, _ := json.Marshal(msg)
		messages[i] = message.New(body)
	}

	b.ResetTimer()
	b.ReportAllocs()

	// Send in batches
	batchSize := 10
	for i := 0; i < len(messages); i += batchSize {
		end := i + batchSize
		if end > len(messages) {
			end = len(messages)
		}
		if err := sender.Send(ctx, queueName, messages[i:end]); err != nil {
			b.Fatalf("Failed to send messages: %v", err)
		}
	}
}

// BenchmarkSenderGopipeBatchPipe benchmarks using Sender with gopipe's BatchPipe
// This implementation leverages gopipe's built-in pipeline infrastructure
func BenchmarkSenderGopipeBatchPipe(b *testing.B) {
	helper := NewTestHelperB(b)
	defer helper.Cleanup()

	ctx := context.Background()
	queueName := GenerateTestNameB(b, "bench-sender-gopipe")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	sender := NewSender(client, SenderConfig{
		SendTimeout: 30 * time.Second,
	})
	defer sender.Close()

	// Prepare messages
	messages := make([]*message.Message[[]byte], b.N)
	for i := range b.N {
		msg := BenchmarkMessage{ID: i, Payload: "benchmark-payload"}
		body, _ := json.Marshal(msg)
		messages[i] = message.New(body)
	}

	b.ResetTimer()
	b.ReportAllocs()

	// Use gopipe's BatchPipe with batching
	batchSize := 10
	batchTimeout := 50 * time.Millisecond
	batchPipe := gopipe.NewBatchPipe(
		func(ctx context.Context, batch []*message.Message[[]byte]) ([]struct{}, error) {
			if err := sender.Send(ctx, queueName, batch); err != nil {
				return nil, err
			}
			return make([]struct{}, len(batch)), nil
		},
		batchSize,
		batchTimeout,
	)

	// Create input channel
	in := make(chan *message.Message[[]byte], b.N)
	go func() {
		for _, msg := range messages {
			in <- msg
		}
		close(in)
	}()

	// Run pipeline using Start method
	out := batchPipe.Start(ctx, in)

	// Drain output
	for range out {
	}
}

// BenchmarkSenderGopipeHelper benchmarks using the NewMessageSinkPipe helper
func BenchmarkSenderGopipeHelper(b *testing.B) {
	helper := NewTestHelperB(b)
	defer helper.Cleanup()

	ctx := context.Background()
	queueName := GenerateTestNameB(b, "bench-sender-helper")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	sender := NewSender(client, SenderConfig{
		SendTimeout: 30 * time.Second,
	})
	defer sender.Close()

	// Prepare messages
	messages := make([]*message.Message[[]byte], b.N)
	for i := range b.N {
		msg := BenchmarkMessage{ID: i, Payload: "benchmark-payload"}
		body, _ := json.Marshal(msg)
		messages[i] = message.New(body)
	}

	b.ResetTimer()
	b.ReportAllocs()

	// Use the helper function
	sinkPipe := NewMessageSinkPipe(sender, queueName, 10, 50*time.Millisecond)

	// Create input channel
	in := make(chan *message.Message[[]byte], b.N)
	go func() {
		for _, msg := range messages {
			in <- msg
		}
		close(in)
	}()

	// Run pipeline
	out := sinkPipe.Start(ctx, in)

	// Drain output
	for range out {
	}
}

// BenchmarkReceiverDirect benchmarks the Receiver.Receive method directly
func BenchmarkReceiverDirect(b *testing.B) {
	if b.N < 10 {
		b.Skip("Skipping receiver benchmark for small N")
	}

	helper := NewTestHelperB(b)
	defer helper.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	queueName := GenerateTestNameB(b, "bench-recv-direct")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// First, publish messages
	sender := NewSender(client, SenderConfig{})
	messages := make([]*message.Message[[]byte], b.N)
	for i := range b.N {
		msg := BenchmarkMessage{ID: i, Payload: "benchmark-payload"}
		body, _ := json.Marshal(msg)
		messages[i] = message.New(body)
	}

	batchSize := 100
	for i := 0; i < len(messages); i += batchSize {
		end := i + batchSize
		if end > len(messages) {
			end = len(messages)
		}
		if err := sender.Send(ctx, queueName, messages[i:end]); err != nil {
			b.Fatalf("Failed to send messages: %v", err)
		}
	}
	sender.Close()

	time.Sleep(1 * time.Second)

	b.ResetTimer()
	b.ReportAllocs()

	receiver := NewReceiver(client, ReceiverConfig{
		MaxMessageCount: 10,
	})
	defer receiver.Close()

	received := 0
	for received < b.N {
		msgs, err := receiver.Receive(ctx, queueName)
		if err != nil {
			b.Fatalf("Failed to receive messages: %v", err)
		}
		for _, msg := range msgs {
			msg.Ack()
			received++
		}
	}
}

// BenchmarkReceiverGopipeGenerator benchmarks using Receiver with gopipe's Generator
func BenchmarkReceiverGopipeGenerator(b *testing.B) {
	if b.N < 10 {
		b.Skip("Skipping receiver benchmark for small N")
	}

	helper := NewTestHelperB(b)
	defer helper.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	queueName := GenerateTestNameB(b, "bench-recv-gopipe")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// First, publish messages to subscribe to
	sender := NewSender(client, SenderConfig{})
	messages := make([]*message.Message[[]byte], b.N)
	for i := range b.N {
		msg := BenchmarkMessage{ID: i, Payload: "benchmark-payload"}
		body, _ := json.Marshal(msg)
		messages[i] = message.New(body)
	}

	// Send in batches
	batchSize := 100
	for i := 0; i < len(messages); i += batchSize {
		end := i + batchSize
		if end > len(messages) {
			end = len(messages)
		}
		if err := sender.Send(ctx, queueName, messages[i:end]); err != nil {
			b.Fatalf("Failed to send messages: %v", err)
		}
	}
	sender.Close()

	// Give Azure time to process
	time.Sleep(1 * time.Second)

	b.ResetTimer()
	b.ReportAllocs()

	// Use gopipe's Generator with Receiver
	receiver := NewReceiver(client, ReceiverConfig{
		MaxMessageCount: 10,
	})
	defer receiver.Close()

	generator := gopipe.NewGenerator(
		func(ctx context.Context) ([]*message.Message[[]byte], error) {
			return receiver.Receive(ctx, queueName)
		},
	)

	out := generator.Generate(ctx)

	received := 0
	for msg := range out {
		msg.Ack()
		received++
		if received >= b.N {
			break
		}
	}
}

// BenchmarkReceiverGopipeHelper benchmarks using the NewMessageGenerator helper
func BenchmarkReceiverGopipeHelper(b *testing.B) {
	if b.N < 10 {
		b.Skip("Skipping receiver benchmark for small N")
	}

	helper := NewTestHelperB(b)
	defer helper.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	queueName := GenerateTestNameB(b, "bench-recv-helper")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	// First, publish messages
	sender := NewSender(client, SenderConfig{})
	messages := make([]*message.Message[[]byte], b.N)
	for i := range b.N {
		msg := BenchmarkMessage{ID: i, Payload: "benchmark-payload"}
		body, _ := json.Marshal(msg)
		messages[i] = message.New(body)
	}

	batchSize := 100
	for i := 0; i < len(messages); i += batchSize {
		end := i + batchSize
		if end > len(messages) {
			end = len(messages)
		}
		if err := sender.Send(ctx, queueName, messages[i:end]); err != nil {
			b.Fatalf("Failed to send messages: %v", err)
		}
	}
	sender.Close()

	time.Sleep(1 * time.Second)

	b.ResetTimer()
	b.ReportAllocs()

	// Use the helper function
	receiver := NewReceiver(client, ReceiverConfig{
		MaxMessageCount: 10,
	})
	defer receiver.Close()

	generator := NewMessageGenerator(receiver, queueName)
	out := generator.Generate(ctx)

	received := 0
	for msg := range out {
		msg.Ack()
		received++
		if received >= b.N {
			break
		}
	}
}

// BenchmarkConcurrentSend benchmarks concurrent sending
func BenchmarkConcurrentSend(b *testing.B) {
	helper := NewTestHelperB(b)
	defer helper.Cleanup()

	ctx := context.Background()
	queueName := GenerateTestNameB(b, "bench-concurrent")
	helper.CreateQueue(ctx, queueName)

	client, err := NewClient(helper.ConnectionString())
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close(ctx)

	sender := NewSender(client, SenderConfig{})
	defer sender.Close()

	b.ResetTimer()
	b.ReportAllocs()

	var wg sync.WaitGroup
	concurrency := 4
	perWorker := b.N / concurrency

	for w := range concurrency {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			messages := make([]*message.Message[[]byte], perWorker)
			for i := range perWorker {
				msg := BenchmarkMessage{
					ID:      workerID*perWorker + i,
					Payload: "concurrent-payload",
				}
				body, _ := json.Marshal(msg)
				messages[i] = message.New(body)
			}

			// Send in batches
			batchSize := 10
			for i := 0; i < len(messages); i += batchSize {
				end := i + batchSize
				if end > len(messages) {
					end = len(messages)
				}
				if err := sender.Send(ctx, queueName, messages[i:end]); err != nil {
					b.Errorf("Worker %d failed to send: %v", workerID, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
}

// NewTestHelperB creates a test helper for benchmarks
func NewTestHelperB(b *testing.B) *TestHelperB {
	b.Helper()

	connStr := getConnectionStringB(b)

	return &TestHelperB{
		b:       b,
		connStr: connStr,
		queues:  make([]string, 0),
	}
}

// TestHelperB provides utilities for managing Azure Service Bus resources in benchmarks
type TestHelperB struct {
	b       *testing.B
	connStr string
	queues  []string
	mu      sync.Mutex
}

func (h *TestHelperB) ConnectionString() string {
	return h.connStr
}

func (h *TestHelperB) CreateQueue(ctx context.Context, queueName string) {
	h.b.Helper()

	adminClient, err := admin.NewClientFromConnectionString(h.connStr, nil)
	if err != nil {
		h.b.Fatalf("Failed to create admin client: %v", err)
	}

	_, err = adminClient.CreateQueue(ctx, queueName, nil)
	if err != nil {
		h.b.Fatalf("Failed to create queue %s: %v", queueName, err)
	}

	h.mu.Lock()
	h.queues = append(h.queues, queueName)
	h.mu.Unlock()
}

func (h *TestHelperB) Cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	adminClient, err := admin.NewClientFromConnectionString(h.connStr, nil)
	if err != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for _, queueName := range h.queues {
		_, _ = adminClient.DeleteQueue(ctx, queueName, nil)
	}
}

func getConnectionStringB(b *testing.B) string {
	b.Helper()

	// Try to load .env file (ignore error if it doesn't exist)
	_ = godotenv.Load()

	connStr := os.Getenv("AZURE_SERVICEBUS_CONNECTION_STRING")
	if connStr == "" {
		b.Skip("AZURE_SERVICEBUS_CONNECTION_STRING not set - skipping benchmark")
	}

	return connStr
}

func GenerateTestNameB(b *testing.B, prefix string) string {
	b.Helper()
	timestamp := time.Now().Unix()
	return fmt.Sprintf("%s-%d", prefix, timestamp)
}
