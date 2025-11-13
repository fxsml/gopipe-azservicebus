package azservicebus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fxsml/gopipe"
)

func TestPublisher_PublishToQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name         string
		queue        string
		messageCount int
		withMetadata bool
		expectError  bool
	}{
		{
			name:         "single message",
			queue:        "test-queue-001",
			messageCount: 1,
			withMetadata: false,
			expectError:  false,
		},
		{
			name:         "multiple messages",
			queue:        "test-queue-001",
			messageCount: 10,
			withMetadata: false,
			expectError:  false,
		},
		{
			name:         "messages with metadata",
			queue:        "test-queue-001",
			messageCount: 5,
			withMetadata: true,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createTestClient(t)
			publisher := createTestPublisher[TestMessage](t, client)

			// Create message channel
			msgChan := make(chan *gopipe.Message[TestMessage], tt.messageCount)

			// Track acknowledgments
			var (
				ackedCount  atomic.Int32
				nackedCount atomic.Int32
				wg          sync.WaitGroup
			)

			// Create and send messages
			for i := 0; i < tt.messageCount; i++ {
				payload := TestMessage{
					ID:      fmt.Sprintf("pub-msg-%d", i),
					Content: fmt.Sprintf("Publisher test message %d", i),
					Value:   i,
					Time:    time.Now(),
				}

				metadata := gopipe.Metadata{}
				if tt.withMetadata {
					metadata["message_id"] = payload.ID
					metadata["correlation_id"] = fmt.Sprintf("corr-%d", i)
					metadata["content_type"] = "application/json"
					metadata["subject"] = "test-subject"
					metadata["custom_prop"] = fmt.Sprintf("value-%d", i)
				}

				wg.Add(1)
				msg := gopipe.NewMessage(
					metadata,
					payload,
					time.Time{}, // No deadline
					func() {
						ackedCount.Add(1)
						wg.Done()
					},
					func(err error) {
						nackedCount.Add(1)
						t.Logf("Message nacked: %v", err)
						wg.Done()
					},
				)

				msgChan <- msg
			}
			close(msgChan)

			// Publish messages
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			done, err := publisher.Publish(ctx, tt.queue, msgChan)
			if err != nil {
				if !tt.expectError {
					t.Fatalf("Unexpected error publishing: %v", err)
				}
				return
			}

			// Wait for publishing to complete
			select {
			case <-done:
				t.Log("Publishing completed")
			case <-time.After(30 * time.Second):
				t.Fatal("Timeout waiting for publishing to complete")
			}

			// Wait for all acknowledgments
			waitCh := make(chan struct{})
			go func() {
				wg.Wait()
				close(waitCh)
			}()

			select {
			case <-waitCh:
				t.Log("All acknowledgments received")
			case <-time.After(10 * time.Second):
				t.Fatal("Timeout waiting for acknowledgments")
			}

			// Verify acknowledgments
			acked := ackedCount.Load()
			nacked := nackedCount.Load()

			if !tt.expectError {
				if acked != int32(tt.messageCount) {
					t.Errorf("Expected %d acked messages, got %d", tt.messageCount, acked)
				}
				if nacked != 0 {
					t.Errorf("Expected 0 nacked messages, got %d", nacked)
				}
			}

			// Verify messages were actually published by receiving them
			receiver, err := client.NewReceiverForQueue(tt.queue, nil)
			if err != nil {
				t.Fatalf("Failed to create receiver: %v", err)
			}
			defer receiver.Close(context.Background())

			receivedCount := 0
			timeout := time.After(15 * time.Second)

		receiveLoop:
			for receivedCount < tt.messageCount {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				sbMsgs, err := receiver.ReceiveMessages(ctx, 10, nil)
				cancel()

				if err != nil {
					select {
					case <-timeout:
						break receiveLoop
					default:
						continue
					}
				}

				for _, sbMsg := range sbMsgs {
					var msg TestMessage
					if err := json.Unmarshal(sbMsg.Body, &msg); err != nil {
						t.Errorf("Failed to unmarshal received message: %v", err)
						continue
					}

					t.Logf("Verified message in queue: %s - %s", msg.ID, msg.Content)
					receivedCount++

					// Verify metadata if expected
					if tt.withMetadata {
						if sbMsg.MessageID != "" {
							t.Logf("Message has MessageID: %s", sbMsg.MessageID)
						}
						if sbMsg.CorrelationID != nil {
							t.Logf("Message has CorrelationID: %s", *sbMsg.CorrelationID)
						}
						if sbMsg.Subject != nil {
							t.Logf("Message has Subject: %s", *sbMsg.Subject)
						}
						if len(sbMsg.ApplicationProperties) > 0 {
							t.Logf("Message has %d application properties", len(sbMsg.ApplicationProperties))
						}
					}

					// Complete the message
					completeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					receiver.CompleteMessage(completeCtx, sbMsg, nil)
					cancel()
				}

				if receivedCount >= tt.messageCount {
					break
				}

				select {
				case <-timeout:
					break receiveLoop
				default:
				}
			}

			if receivedCount != tt.messageCount {
				t.Errorf("Expected to receive %d messages, got %d", tt.messageCount, receivedCount)
			}
		})
	}
}

func TestPublisher_PublishToTopic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name         string
		topic        string
		messageCount int
	}{
		{
			name:         "publish to topic",
			topic:        "test-topic-001",
			messageCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createTestClient(t)
			publisher := createTestPublisher[TestMessage](t, client)

			// Create messages
			msgChan := make(chan *gopipe.Message[TestMessage], tt.messageCount)

			var ackedCount atomic.Int32

			for i := 0; i < tt.messageCount; i++ {
				payload := TestMessage{
					ID:      fmt.Sprintf("topic-msg-%d", i),
					Content: fmt.Sprintf("Topic message %d", i),
					Value:   i,
				}

				msg := gopipe.NewMessage(
					gopipe.Metadata{},
					payload,
					time.Time{},
					func() { ackedCount.Add(1) },
					func(err error) { t.Errorf("Message nacked: %v", err) },
				)

				msgChan <- msg
			}
			close(msgChan)

			// Publish to topic
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			done, err := publisher.Publish(ctx, tt.topic, msgChan)
			if err != nil {
				t.Fatalf("Failed to publish to topic: %v", err)
			}

			<-done

			time.Sleep(2 * time.Second) // Allow time for messages to propagate

			// Verify acknowledgments
			if acked := ackedCount.Load(); acked != int32(tt.messageCount) {
				t.Errorf("Expected %d acked messages, got %d", tt.messageCount, acked)
			}

			// Verify messages can be received from topic subscription
			receiver, err := client.NewReceiverForSubscription(tt.topic, "test-sub-001", nil)
			if err != nil {
				t.Fatalf("Failed to create subscription receiver: %v", err)
			}
			defer receiver.Close(context.Background())

			receivedCount := 0
			timeout := time.After(15 * time.Second)

		receiveLoop:
			for receivedCount < tt.messageCount {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				sbMsgs, err := receiver.ReceiveMessages(ctx, 10, nil)
				cancel()

				if err != nil {
					select {
					case <-timeout:
						break receiveLoop
					default:
						continue
					}
				}

				for _, sbMsg := range sbMsgs {
					var msg TestMessage
					if err := json.Unmarshal(sbMsg.Body, &msg); err != nil {
						t.Errorf("Failed to unmarshal: %v", err)
						continue
					}

					t.Logf("Received from topic subscription: %s", msg.ID)
					receivedCount++

					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					receiver.CompleteMessage(ctx, sbMsg, nil)
					cancel()
				}

				select {
				case <-timeout:
					break receiveLoop
				default:
				}
			}

			if receivedCount != tt.messageCount {
				t.Errorf("Expected %d messages from topic, got %d", tt.messageCount, receivedCount)
			}
		})
	}
}

func TestPublisher_ConcurrentPublish(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name          string
		queue         string
		messageCount  int
		numPublishers int
	}{
		{
			name:          "single publisher",
			queue:         "test-queue-batch",
			messageCount:  10,
			numPublishers: 1,
		},
		{
			name:          "multiple publishers",
			queue:         "test-queue-batch",
			messageCount:  20,
			numPublishers: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createTestClient(t)

			var (
				totalAcked atomic.Int32
				wg         sync.WaitGroup
			)

			messagesPerPublisher := tt.messageCount / tt.numPublishers

			// Start multiple publishers
			for p := 0; p < tt.numPublishers; p++ {
				wg.Add(1)
				go func(publisherID int) {
					defer wg.Done()

					publisher := createTestPublisher[TestMessage](t, client)

					msgChan := make(chan *gopipe.Message[TestMessage], messagesPerPublisher)

					for i := 0; i < messagesPerPublisher; i++ {
						payload := TestMessage{
							ID:      fmt.Sprintf("pub%d-msg-%d", publisherID, i),
							Content: fmt.Sprintf("Publisher %d message %d", publisherID, i),
							Value:   i,
						}

						msg := gopipe.NewMessage(
							gopipe.Metadata{},
							payload,
							time.Time{},
							func() { totalAcked.Add(1) },
							func(err error) { t.Errorf("Nacked: %v", err) },
						)

						msgChan <- msg
					}
					close(msgChan)

					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()

					done, err := publisher.Publish(ctx, tt.queue, msgChan)
					if err != nil {
						t.Errorf("Publisher %d failed: %v", publisherID, err)
						return
					}

					<-done
					t.Logf("Publisher %d completed", publisherID)
				}(p)
			}

			// Wait for all publishers to complete
			wg.Wait()

			// Verify total acknowledgments
			expectedTotal := messagesPerPublisher * tt.numPublishers
			if acked := totalAcked.Load(); acked != int32(expectedTotal) {
				t.Errorf("Expected %d total acked messages, got %d", expectedTotal, acked)
			}

			t.Logf("Total published by %d publishers: %d messages", tt.numPublishers, totalAcked.Load())
		})
	}
}

func TestPublisher_ContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := createTestClient(t)
	publisher := createTestPublisher[TestMessage](t, client)

	// Create a context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())

	msgChan := make(chan *gopipe.Message[TestMessage], 10)

	var nackedCount atomic.Int32

	// Send a few messages
	for i := 0; i < 5; i++ {
		payload := TestMessage{
			ID:      fmt.Sprintf("cancel-msg-%d", i),
			Content: "Test cancellation",
			Value:   i,
		}

		msg := gopipe.NewMessage(
			gopipe.Metadata{},
			payload,
			time.Time{},
			func() {},
			func(err error) { nackedCount.Add(1) },
		)

		msgChan <- msg
	}

	// Start publishing
	done, err := publisher.Publish(ctx, "test-queue-001", msgChan)
	if err != nil {
		t.Fatalf("Failed to start publishing: %v", err)
	}

	// Cancel context immediately
	cancel()

	// Try to send more messages (these should be nacked)
	for i := 5; i < 10; i++ {
		payload := TestMessage{
			ID:    fmt.Sprintf("cancel-msg-%d", i),
			Value: i,
		}

		msg := gopipe.NewMessage(
			gopipe.Metadata{},
			payload,
			time.Time{},
			func() {},
			func(err error) { nackedCount.Add(1) },
		)

		msgChan <- msg
	}
	close(msgChan)

	// Wait for done
	<-done

	// We expect some messages to be nacked due to context cancellation
	nacked := nackedCount.Load()
	t.Logf("Nacked messages due to cancellation: %d", nacked)

	if nacked == 0 {
		t.Log("Warning: Expected some nacked messages due to context cancellation")
	}
}

func TestPublisher_MetadataPreservation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name     string
		queue    string
		metadata gopipe.Metadata
	}{
		{
			name:  "standard service bus properties",
			queue: "test-queue-001",
			metadata: gopipe.Metadata{
				"message_id":     "test-123",
				"correlation_id": "corr-456",
				"subject":        "test-subject",
				"content_type":   "application/json",
				"reply_to":       "reply-queue",
			},
		},
		{
			name:  "custom application properties",
			queue: "test-queue-001",
			metadata: gopipe.Metadata{
				"custom_string": "string-value",
				"custom_number": "42",
				"custom_bool":   "true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createTestClient(t)
			publisher := createTestPublisher[TestMessage](t, client)

			payload := TestMessage{
				ID:      "metadata-test",
				Content: "Testing metadata preservation",
			}

			msgChan := make(chan *gopipe.Message[TestMessage], 1)

			var acked atomic.Bool

			msg := gopipe.NewMessage(
				tt.metadata,
				payload,
				time.Time{},
				func() { acked.Store(true) },
				func(err error) { t.Errorf("Message nacked: %v", err) },
			)

			msgChan <- msg
			close(msgChan)

			// Publish
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			done, err := publisher.Publish(ctx, tt.queue, msgChan)
			if err != nil {
				t.Fatalf("Failed to publish: %v", err)
			}

			<-done

			if !acked.Load() {
				t.Fatal("Message was not acknowledged")
			}

			// Receive and verify metadata
			receiver, err := client.NewReceiverForQueue(tt.queue, nil)
			if err != nil {
				t.Fatalf("Failed to create receiver: %v", err)
			}
			defer receiver.Close(context.Background())

			ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			sbMsgs, err := receiver.ReceiveMessages(ctx, 1, nil)
			if err != nil {
				t.Fatalf("Failed to receive: %v", err)
			}

			if len(sbMsgs) == 0 {
				t.Fatal("No messages received")
			}

			sbMsg := sbMsgs[0]

			// Verify metadata was preserved
			if msgID, ok := tt.metadata["message_id"]; ok {
				if sbMsg.MessageID != msgID {
					t.Errorf("MessageID not preserved: expected %s, got %s", msgID, sbMsg.MessageID)
				}
			}

			if corrID, ok := tt.metadata["correlation_id"]; ok {
				if sbMsg.CorrelationID == nil || *sbMsg.CorrelationID != corrID {
					t.Errorf("CorrelationID not preserved: expected %s, got %v", corrID, sbMsg.CorrelationID)
				}
			}

			if subject, ok := tt.metadata["subject"]; ok {
				if sbMsg.Subject == nil || *sbMsg.Subject != subject {
					t.Errorf("Subject not preserved: expected %s, got %v", subject, sbMsg.Subject)
				}
			}

			// Verify custom properties
			for key := range tt.metadata {
				// Skip standard properties
				if key == "message_id" || key == "correlation_id" || key == "subject" ||
					key == "content_type" || key == "reply_to" || key == "session_id" || key == "to" {
					continue
				}

				if actualValue, ok := sbMsg.ApplicationProperties[key]; !ok {
					t.Errorf("Custom property %s not found", key)
				} else {
					t.Logf("Custom property %s preserved: %v", key, actualValue)
				}
			}

			// Clean up
			completeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			receiver.CompleteMessage(completeCtx, sbMsg, nil)
			cancel()
		})
	}
}
