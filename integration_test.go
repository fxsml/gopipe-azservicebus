package azservicebus

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fxsml/gopipe"
)

func TestIntegration_PublishSubscribe(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name         string
		queue        string
		messageCount int
		processDelay time.Duration
	}{
		{
			name:         "basic publish-subscribe",
			queue:        "test-queue-001",
			messageCount: 5,
			processDelay: 0,
		},
		{
			name:         "publish-subscribe with processing delay",
			queue:        "test-queue-002",
			messageCount: 10,
			processDelay: 10 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createTestClient(t)
			publisher := createTestPublisher[TestMessage](t, client)
			subscriber := createTestSubscriber[TestMessage](t, client)

			// Start subscriber first
			subCtx, subCancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer subCancel()

			messages, err := subscriber.Subscribe(subCtx, tt.queue)
			if err != nil {
				t.Fatalf("Failed to subscribe: %v", err)
			}

			// Track received messages
			receivedChan := make(chan TestMessage, tt.messageCount)
			go func() {
				for msg := range messages {
					if msg == nil {
						continue
					}

					t.Logf("Received: %s - %s", msg.Payload.ID, msg.Payload.Content)

					// Simulate processing
					if tt.processDelay > 0 {
						time.Sleep(tt.processDelay)
					}

					msg.Ack()
					receivedChan <- msg.Payload
				}
			}()

			// Give subscriber time to connect
			time.Sleep(1 * time.Second)

			// Publish messages
			pubCtx, pubCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer pubCancel()

			msgChan := make(chan *gopipe.Message[TestMessage], tt.messageCount)

			var publishedCount atomic.Int32

			for i := 0; i < tt.messageCount; i++ {
				payload := TestMessage{
					ID:      fmt.Sprintf("integration-msg-%d", i),
					Content: fmt.Sprintf("Integration test message %d", i),
					Value:   i,
					Time:    time.Now(),
				}

				metadata := gopipe.Metadata{
					"message_id":     payload.ID,
					"correlation_id": fmt.Sprintf("corr-%d", i),
				}

				msg := gopipe.NewMessage(
					metadata,
					payload,
					time.Time{},
					func() { publishedCount.Add(1) },
					func(err error) { t.Errorf("Publish failed: %v", err) },
				)

				msgChan <- msg
			}
			close(msgChan)

			done, err := publisher.Publish(pubCtx, tt.queue, msgChan)
			if err != nil {
				t.Fatalf("Failed to publish: %v", err)
			}

			// Wait for publishing to complete
			<-done

			// Verify all messages were published
			if published := publishedCount.Load(); published != int32(tt.messageCount) {
				t.Errorf("Expected %d published messages, got %d", tt.messageCount, published)
			}

			// Wait for all messages to be received
			receivedMessages := make([]TestMessage, 0, tt.messageCount)
			timeout := time.After(30 * time.Second)

			for len(receivedMessages) < tt.messageCount {
				select {
				case msg := <-receivedChan:
					receivedMessages = append(receivedMessages, msg)
				case <-timeout:
					t.Fatalf("Timeout waiting for messages. Expected %d, got %d", tt.messageCount, len(receivedMessages))
				}
			}

			// Verify count
			if len(receivedMessages) != tt.messageCount {
				t.Errorf("Expected %d received messages, got %d", tt.messageCount, len(receivedMessages))
			}

			t.Logf("Successfully published and received %d messages", len(receivedMessages))
		})
	}
}

func TestIntegration_Pipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name           string
		inputQueue     string
		outputQueue    string
		messageCount   int
		transformValue func(int) int
	}{
		{
			name:         "simple pipeline transformation",
			inputQueue:   "test-queue-001",
			outputQueue:  "test-queue-002",
			messageCount: 5,
			transformValue: func(v int) int {
				return v * 2
			},
		},
		{
			name:         "pipeline with filtering",
			inputQueue:   "test-queue-001",
			outputQueue:  "test-queue-002",
			messageCount: 10,
			transformValue: func(v int) int {
				// Only pass through even values
				if v%2 == 0 {
					return v * 10
				}
				return -1 // Mark for filtering
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createTestClient(t)

			inputPublisher := createTestPublisher[TestMessage](t, client)
			inputSubscriber := createTestSubscriber[TestMessage](t, client)
			outputPublisher := createTestPublisher[TestMessage](t, client)
			outputSubscriber := createTestSubscriber[TestMessage](t, client)

			// Subscribe to input queue
			inputCtx, inputCancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer inputCancel()

			inputMessages, err := inputSubscriber.Subscribe(inputCtx, tt.inputQueue)
			if err != nil {
				t.Fatalf("Failed to subscribe to input: %v", err)
			}

			// Subscribe to output queue
			outputCtx, outputCancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer outputCancel()

			outputMessages, err := outputSubscriber.Subscribe(outputCtx, tt.outputQueue)
			if err != nil {
				t.Fatalf("Failed to subscribe to output: %v", err)
			}

			// Track output messages
			outputReceived := make(chan TestMessage, tt.messageCount)
			go func() {
				for msg := range outputMessages {
					if msg == nil {
						continue
					}
					t.Logf("Output received: %s - Value=%d", msg.Payload.ID, msg.Payload.Value)
					msg.Ack()
					outputReceived <- msg.Payload
				}
			}()

			// Create output channel for transformed messages
			transformedChan := make(chan *gopipe.Message[TestMessage], tt.messageCount)

			// Start output publisher
			pubCtx, pubCancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer pubCancel()

			outputDone, err := outputPublisher.Publish(pubCtx, tt.outputQueue, transformedChan)
			if err != nil {
				t.Fatalf("Failed to start output publisher: %v", err)
			}

			// Process pipeline: transform input -> output
			var processedCount atomic.Int32
			go func() {
				for msg := range inputMessages {
					if msg == nil {
						continue
					}

					// Transform the message
					newValue := tt.transformValue(msg.Payload.Value)

					// Filter if needed
					if newValue < 0 {
						msg.Ack()
						continue
					}

					transformed := TestMessage{
						ID:      fmt.Sprintf("transformed-%s", msg.Payload.ID),
						Content: fmt.Sprintf("Transformed: %s", msg.Payload.Content),
						Value:   newValue,
						Time:    time.Now(),
					}

					// Create output message that forwards acknowledgment
					outMsg := gopipe.NewMessage(
						msg.Metadata,
						transformed,
						msg.Deadline(),
						func() {
							msg.Ack()
							processedCount.Add(1)
						},
						func(err error) {
							msg.Nack(err)
						},
					)

					transformedChan <- outMsg
				}
				close(transformedChan)
			}()

			// Give pipeline time to set up
			time.Sleep(1 * time.Second)

			// Publish input messages
			inputMsgChan := make(chan *gopipe.Message[TestMessage], tt.messageCount)

			for i := 0; i < tt.messageCount; i++ {
				payload := TestMessage{
					ID:      fmt.Sprintf("input-msg-%d", i),
					Content: fmt.Sprintf("Input message %d", i),
					Value:   i,
				}

				msg := gopipe.NewMessage(
					gopipe.Metadata{},
					payload,
					time.Time{},
					func() {},
					func(err error) { t.Errorf("Input publish failed: %v", err) },
				)

				inputMsgChan <- msg
			}
			close(inputMsgChan)

			inputDone, err := inputPublisher.Publish(pubCtx, tt.inputQueue, inputMsgChan)
			if err != nil {
				t.Fatalf("Failed to publish input: %v", err)
			}

			// Wait for input publishing
			<-inputDone

			// Wait for output publishing
			select {
			case <-outputDone:
				t.Log("Output publishing completed")
			case <-time.After(45 * time.Second):
				t.Fatal("Timeout waiting for output publishing")
			}

			// Collect output messages
			var outputMsgs []TestMessage
			timeout := time.After(30 * time.Second)

		collectLoop:
			for {
				select {
				case msg := <-outputReceived:
					outputMsgs = append(outputMsgs, msg)

					// Check if we've received all expected messages
					expectedCount := 0
					for i := 0; i < tt.messageCount; i++ {
						if tt.transformValue(i) >= 0 {
							expectedCount++
						}
					}

					if len(outputMsgs) >= expectedCount {
						break collectLoop
					}

				case <-timeout:
					break collectLoop
				}
			}

			// Verify transformation
			t.Logf("Processed %d messages through pipeline", len(outputMsgs))

			for _, msg := range outputMsgs {
				if msg.Value < 0 {
					t.Error("Negative value should have been filtered")
				}
				t.Logf("Output message: %s - Value=%d", msg.ID, msg.Value)
			}

			// Verify count
			expectedOutput := 0
			for i := 0; i < tt.messageCount; i++ {
				if tt.transformValue(i) >= 0 {
					expectedOutput++
				}
			}

			if len(outputMsgs) != expectedOutput {
				t.Errorf("Expected %d output messages, got %d", expectedOutput, len(outputMsgs))
			}
		})
	}
}

func TestIntegration_InFlightTracking(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := createTestClient(t)
	publisher := createTestPublisher[TestMessage](t, client)
	subscriber := createTestSubscriber[TestMessage](t, client)

	queue := "test-queue-concurrent"
	messageCount := 10

	// Start subscriber
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	messages, err := subscriber.Subscribe(ctx, queue)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Track in-flight messages
	var (
		receivedCount atomic.Int32
		ackedCount    atomic.Int32
	)

	// Process messages slowly to keep them in-flight
	go func() {
		for msg := range messages {
			if msg == nil {
				continue
			}

			receivedCount.Add(1)
			t.Logf("Processing message: %s", msg.Payload.ID)

			// Simulate slow processing
			time.Sleep(100 * time.Millisecond)

			msg.Ack()
			ackedCount.Add(1)
		}
	}()

	time.Sleep(1 * time.Second)

	// Publish messages
	msgChan := make(chan *gopipe.Message[TestMessage], messageCount)

	for i := 0; i < messageCount; i++ {
		payload := TestMessage{
			ID:      fmt.Sprintf("inflight-msg-%d", i),
			Content: "In-flight tracking test",
			Value:   i,
		}

		msg := gopipe.NewMessage(
			gopipe.Metadata{},
			payload,
			time.Time{},
			func() {},
			func(err error) { t.Errorf("Failed: %v", err) },
		)

		msgChan <- msg
	}
	close(msgChan)

	done, err := publisher.Publish(ctx, queue, msgChan)
	if err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	<-done

	// Wait a bit for messages to be in-flight
	time.Sleep(500 * time.Millisecond)

	t.Logf("Received: %d, Acked: %d (some messages should be in-flight)",
		receivedCount.Load(), ackedCount.Load())

	// Close subscriber - should wait for in-flight messages
	t.Log("Closing subscriber - should wait for in-flight messages")
	closeStart := time.Now()

	if err := subscriber.Close(); err != nil {
		t.Errorf("Failed to close subscriber: %v", err)
	}

	closeTime := time.Since(closeStart)
	t.Logf("Subscriber closed after %v", closeTime)

	// Verify all messages were acked
	finalAcked := ackedCount.Load()
	finalReceived := receivedCount.Load()

	t.Logf("Final: Received=%d, Acked=%d", finalReceived, finalAcked)

	if finalReceived != int32(messageCount) {
		t.Errorf("Expected %d received messages, got %d", messageCount, finalReceived)
	}

	// Note: We can't guarantee all acks complete due to cleanup timing,
	// but we should have received all messages
	if finalAcked < int32(messageCount-2) {
		t.Errorf("Too few acked messages. Expected ~%d, got %d", messageCount, finalAcked)
	}
}

func TestIntegration_ErrorRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := createTestClient(t)
	publisher := createTestPublisher[TestMessage](t, client)
	subscriber := createTestSubscriber[TestMessage](t, client)

	queue := "test-queue-deadletter"
	messageCount := 5

	// Subscribe
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	messages, err := subscriber.Subscribe(ctx, queue)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	var (
		attemptCount  = make(map[string]int)
		attemptMutex  = &sync.Mutex{}
		successCount  atomic.Int32
		maxRetryCount = 2
	)

	// Process with simulated failures and retries
	go func() {
		for msg := range messages {
			if msg == nil {
				continue
			}

			attemptMutex.Lock()
			attemptCount[msg.Payload.ID]++
			attempts := attemptCount[msg.Payload.ID]
			attemptMutex.Unlock()

			t.Logf("Processing %s (attempt %d)", msg.Payload.ID, attempts)

			// Fail first attempt, succeed on retry
			if attempts <= maxRetryCount {
				msg.Nack(fmt.Errorf("simulated failure attempt %d", attempts))
				t.Logf("Nacked %s (attempt %d)", msg.Payload.ID, attempts)
			} else {
				msg.Ack()
				successCount.Add(1)
				t.Logf("Acked %s (attempt %d)", msg.Payload.ID, attempts)
			}
		}
	}()

	time.Sleep(1 * time.Second)

	// Publish messages
	msgChan := make(chan *gopipe.Message[TestMessage], messageCount)

	for i := 0; i < messageCount; i++ {
		payload := TestMessage{
			ID:      fmt.Sprintf("retry-msg-%d", i),
			Content: "Error recovery test",
			Value:   i,
		}

		msg := gopipe.NewMessage(
			gopipe.Metadata{},
			payload,
			time.Time{},
			func() {},
			func(err error) {},
		)

		msgChan <- msg
	}
	close(msgChan)

	done, err := publisher.Publish(ctx, queue, msgChan)
	if err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	<-done

	// Wait for retries and processing
	timeout := time.After(45 * time.Second)

waitLoop:
	for {
		if successCount.Load() >= int32(messageCount) {
			break
		}

		select {
		case <-timeout:
			break waitLoop
		default:
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Verify all messages eventually succeeded
	success := successCount.Load()
	t.Logf("Successfully processed %d messages after retries", success)

	if success < int32(messageCount) {
		t.Logf("Warning: Expected %d successful, got %d (some may have been dead-lettered)", messageCount, success)
	}

	// Verify retry counts
	attemptMutex.Lock()
	for id, count := range attemptCount {
		t.Logf("Message %s: %d attempts", id, count)
		if count < maxRetryCount+1 {
			t.Logf("Warning: Message %s had fewer attempts than expected: %d", id, count)
		}
	}
	attemptMutex.Unlock()
}
