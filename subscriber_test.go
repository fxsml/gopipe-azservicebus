package azservicebus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe"
)

type TestMessage struct {
	ID      string    `json:"id"`
	Content string    `json:"content"`
	Value   int       `json:"value"`
	Time    time.Time `json:"time"`
}

func TestSubscriber_ReceiveAndAck(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name          string
		queue         string
		messageCount  int
		ackAll        bool
		expectRequeue bool
	}{
		{
			name:          "single message ack",
			queue:         "test-queue-001",
			messageCount:  1,
			ackAll:        true,
			expectRequeue: false,
		},
		{
			name:          "multiple messages ack",
			queue:         "test-queue-001",
			messageCount:  5,
			ackAll:        true,
			expectRequeue: false,
		},
		{
			name:          "single message nack - should requeue",
			queue:         "test-queue-002",
			messageCount:  1,
			ackAll:        false,
			expectRequeue: true,
		},
		{
			name:          "partial ack - some messages nacked",
			queue:         "test-queue-002",
			messageCount:  5,
			ackAll:        false,
			expectRequeue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createTestClient(t)
			subscriber := createTestSubscriber[TestMessage](t, client)

			// Publish test messages
			sender, err := client.NewSender(tt.queue, nil)
			if err != nil {
				t.Fatalf("Failed to create sender: %v", err)
			}
			defer sender.Close(context.Background())

			publishedIDs := make([]string, tt.messageCount)
			for i := 0; i < tt.messageCount; i++ {
				msg := TestMessage{
					ID:      fmt.Sprintf("msg-%d", i),
					Content: fmt.Sprintf("Test message %d", i),
					Value:   i,
					Time:    time.Now(),
				}

				body, err := json.Marshal(msg)
				if err != nil {
					t.Fatalf("Failed to marshal message: %v", err)
				}

				sbMsg := &azservicebus.Message{
					Body: body,
				}
				msgID := msg.ID
				sbMsg.MessageID = &msgID

				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := sender.SendMessage(ctx, sbMsg, nil); err != nil {
					cancel()
					t.Fatalf("Failed to send message %d: %v", i, err)
				}
				cancel()

				publishedIDs[i] = msg.ID
			}

			// Subscribe and receive messages
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			messages, err := subscriber.Subscribe(ctx, tt.queue)
			if err != nil {
				t.Fatalf("Failed to subscribe: %v", err)
			}

			// Process messages
			receivedCount := 0
			timeout := time.After(20 * time.Second)

			for receivedCount < tt.messageCount {
				select {
				case msg := <-messages:
					if msg == nil {
						t.Fatal("Received nil message")
					}

					t.Logf("Received message: ID=%s, Content=%s, Value=%d",
						msg.Payload.ID, msg.Payload.Content, msg.Payload.Value)

					// Verify message content
					if msg.Payload.ID == "" {
						t.Error("Message ID is empty")
					}

					// Verify metadata
					if msgID, ok := msg.Metadata["message_id"]; ok {
						t.Logf("Message metadata message_id: %v", msgID)
					}

					// Ack or Nack based on test case
					if tt.ackAll {
						msg.Ack()
						t.Logf("Acknowledged message %s", msg.Payload.ID)
					} else {
						// Nack odd-numbered messages
						if receivedCount%2 == 1 {
							msg.Nack(fmt.Errorf("intentional nack for testing"))
							t.Logf("Nacked message %s", msg.Payload.ID)
						} else {
							msg.Ack()
							t.Logf("Acknowledged message %s", msg.Payload.ID)
						}
					}

					receivedCount++

				case <-timeout:
					t.Fatalf("Timeout waiting for messages. Expected %d, got %d", tt.messageCount, receivedCount)
				}
			}

			// Verify all messages were received
			if receivedCount != tt.messageCount {
				t.Errorf("Expected %d messages, got %d", tt.messageCount, receivedCount)
			}

			// If we expect requeue, wait a bit and verify messages come back
			if tt.expectRequeue {
				time.Sleep(2 * time.Second)

				requeuedCount := 0
				requeueTimeout := time.After(15 * time.Second)

			requeueLoop:
				for {
					select {
					case msg := <-messages:
						if msg == nil {
							continue
						}
						t.Logf("Received requeued message: %s", msg.Payload.ID)
						msg.Ack() // Clean up
						requeuedCount++

					case <-requeueTimeout:
						break requeueLoop
					}
				}

				if requeuedCount == 0 {
					t.Error("Expected at least one requeued message, got none")
				} else {
					t.Logf("Received %d requeued messages as expected", requeuedCount)
				}
			}
		})
	}
}

func TestSubscriber_ConcurrentProcessing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name              string
		queue             string
		messageCount      int
		concurrentWorkers int
		batchSize         int
	}{
		{
			name:              "sequential processing",
			queue:             "test-queue-concurrent",
			messageCount:      10,
			concurrentWorkers: 1,
			batchSize:         1,
		},
		{
			name:              "concurrent processing",
			queue:             "test-queue-concurrent",
			messageCount:      20,
			concurrentWorkers: 5,
			batchSize:         5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createTestClient(t)

			// Create subscriber with custom config
			config := DefaultSubscriberConfig()
			config.ConcurrentMessages = tt.concurrentWorkers
			config.BatchSize = tt.batchSize
			config.MessageTimeout = 30 * time.Second

			subscriber := NewSubscriber[TestMessage](client, config)
			defer subscriber.Close()

			// Publish messages
			sender, err := client.NewSender(tt.queue, nil)
			if err != nil {
				t.Fatalf("Failed to create sender: %v", err)
			}
			defer sender.Close(context.Background())

			for i := 0; i < tt.messageCount; i++ {
				msg := TestMessage{
					ID:      fmt.Sprintf("concurrent-msg-%d", i),
					Content: fmt.Sprintf("Concurrent test %d", i),
					Value:   i,
					Time:    time.Now(),
				}

				body, _ := json.Marshal(msg)
				sbMsg := &azservicebus.Message{Body: body}

				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := sender.SendMessage(ctx, sbMsg, nil); err != nil {
					cancel()
					t.Fatalf("Failed to send message: %v", err)
				}
				cancel()
			}

			// Subscribe
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			messages, err := subscriber.Subscribe(ctx, tt.queue)
			if err != nil {
				t.Fatalf("Failed to subscribe: %v", err)
			}

			// Track concurrent processing
			var (
				receivedCount atomic.Int32
				activeCount   atomic.Int32
				maxActive     atomic.Int32
				wg            sync.WaitGroup
			)

			// Process messages concurrently
			wg.Add(tt.messageCount)
			timeout := time.After(45 * time.Second)

			for i := 0; i < tt.messageCount; i++ {
				select {
				case msg := <-messages:
					if msg == nil {
						wg.Done()
						continue
					}

					go func(m *gopipe.Message[TestMessage]) {
						defer wg.Done()

						// Track active workers
						active := activeCount.Add(1)
						defer activeCount.Add(-1)

						// Update max active
						for {
							current := maxActive.Load()
							if active <= current || maxActive.CompareAndSwap(current, active) {
								break
							}
						}

						// Simulate processing
						time.Sleep(10 * time.Millisecond)

						m.Ack()
						receivedCount.Add(1)
					}(msg)

				case <-timeout:
					t.Fatal("Timeout waiting for messages")
				}
			}

			// Wait for all processing to complete
			wg.Wait()

			// Verify results
			received := receivedCount.Load()
			if received != int32(tt.messageCount) {
				t.Errorf("Expected %d messages processed, got %d", tt.messageCount, received)
			}

			maxActiveWorkers := maxActive.Load()
			t.Logf("Max concurrent workers: %d (configured: %d)", maxActiveWorkers, tt.concurrentWorkers)

			if tt.concurrentWorkers > 1 && maxActiveWorkers < 2 {
				t.Error("Expected concurrent processing but got sequential")
			}
		})
	}
}

func TestSubscriber_TopicSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name           string
		topicSub       string
		messageCount   int
		expectAllRecvd bool
	}{
		{
			name:           "single topic subscription",
			topicSub:       "test-topic-001/test-sub-001",
			messageCount:   3,
			expectAllRecvd: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createTestClient(t)
			subscriber := createTestSubscriber[TestMessage](t, client)

			// Parse topic and subscription
			topic := "test-topic-001"

			// Publish to topic
			sender, err := client.NewSender(topic, nil)
			if err != nil {
				t.Fatalf("Failed to create topic sender: %v", err)
			}
			defer sender.Close(context.Background())

			for i := 0; i < tt.messageCount; i++ {
				msg := TestMessage{
					ID:      fmt.Sprintf("topic-msg-%d", i),
					Content: fmt.Sprintf("Topic message %d", i),
					Value:   i,
				}

				body, _ := json.Marshal(msg)
				sbMsg := &azservicebus.Message{Body: body}

				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := sender.SendMessage(ctx, sbMsg, nil); err != nil {
					cancel()
					t.Fatalf("Failed to send to topic: %v", err)
				}
				cancel()
			}

			// Subscribe to topic subscription
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			messages, err := subscriber.Subscribe(ctx, tt.topicSub)
			if err != nil {
				t.Fatalf("Failed to subscribe to topic: %v", err)
			}

			// Receive messages
			receivedCount := 0
			timeout := time.After(20 * time.Second)

			for receivedCount < tt.messageCount {
				select {
				case msg := <-messages:
					if msg == nil {
						t.Fatal("Received nil message")
					}

					t.Logf("Received from topic: %s - %s", msg.Payload.ID, msg.Payload.Content)
					msg.Ack()
					receivedCount++

				case <-timeout:
					if tt.expectAllRecvd {
						t.Fatalf("Timeout. Expected %d, got %d", tt.messageCount, receivedCount)
					}
					return
				}
			}

			if receivedCount != tt.messageCount {
				t.Errorf("Expected %d messages, got %d", tt.messageCount, receivedCount)
			}
		})
	}
}

func TestSubscriber_MetadataMapping(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name             string
		queue            string
		messageID        string
		subject          string
		correlationID    string
		contentType      string
		customProperties map[string]interface{}
		expectedMetadata map[string]string
	}{
		{
			name:          "standard properties",
			queue:         "test-queue-001",
			messageID:     "test-msg-123",
			subject:       "test-subject",
			correlationID: "corr-456",
			contentType:   "application/json",
			expectedMetadata: map[string]string{
				"message_id":     "test-msg-123",
				"subject":        "test-subject",
				"correlation_id": "corr-456",
				"content_type":   "application/json",
			},
		},
		{
			name:      "custom application properties",
			queue:     "test-queue-001",
			messageID: "test-msg-456",
			customProperties: map[string]interface{}{
				"custom_field_1": "value1",
				"custom_field_2": 12345,
				"custom_field_3": true,
			},
			expectedMetadata: map[string]string{
				"message_id":     "test-msg-456",
				"custom_field_1": "value1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createTestClient(t)
			subscriber := createTestSubscriber[TestMessage](t, client)

			// Publish message with metadata
			sender, err := client.NewSender(tt.queue, nil)
			if err != nil {
				t.Fatalf("Failed to create sender: %v", err)
			}
			defer sender.Close(context.Background())

			testMsg := TestMessage{
				ID:      "metadata-test",
				Content: "Metadata test message",
			}

			body, _ := json.Marshal(testMsg)
			sbMsg := &azservicebus.Message{
				Body:                  body,
				ApplicationProperties: make(map[string]interface{}),
			}

			if tt.messageID != "" {
				sbMsg.MessageID = &tt.messageID
			}
			if tt.subject != "" {
				sbMsg.Subject = &tt.subject
			}
			if tt.correlationID != "" {
				sbMsg.CorrelationID = &tt.correlationID
			}
			if tt.contentType != "" {
				sbMsg.ContentType = &tt.contentType
			}
			for k, v := range tt.customProperties {
				sbMsg.ApplicationProperties[k] = v
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := sender.SendMessage(ctx, sbMsg, nil); err != nil {
				cancel()
				t.Fatalf("Failed to send message: %v", err)
			}
			cancel()

			// Subscribe and verify metadata
			ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			messages, err := subscriber.Subscribe(ctx, tt.queue)
			if err != nil {
				t.Fatalf("Failed to subscribe: %v", err)
			}

			timeout := time.After(20 * time.Second)
			select {
			case msg := <-messages:
				if msg == nil {
					t.Fatal("Received nil message")
				}

				// Verify expected metadata
				for key, expectedValue := range tt.expectedMetadata {
					if actualValue, ok := msg.Metadata[key]; !ok {
						t.Errorf("Missing metadata key: %s", key)
					} else if actualValue != expectedValue {
						t.Errorf("Metadata[%s]: expected %s, got %s", key, expectedValue, actualValue)
					}
				}

				// Verify custom properties are present
				for key := range tt.customProperties {
					if _, ok := msg.Metadata[key]; !ok {
						t.Errorf("Missing custom property: %s", key)
					} else {
						t.Logf("Custom property %s: %v", key, msg.Metadata[key])
					}
				}

				msg.Ack()

			case <-timeout:
				t.Fatal("Timeout waiting for message")
			}
		})
	}
}
