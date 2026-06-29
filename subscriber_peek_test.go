package azservicebus_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	gosb "github.com/fxsml/gopipe-azservicebus"
	"github.com/fxsml/gopipe/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSubscriber_PeekMode verifies that EnablePeekMode reads messages without consuming them:
// after the peek subscriber has processed all messages, a subsequent peek via the raw SDK
// must still find every message on the bus with DeliveryCount == 0.
func TestSubscriber_PeekMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topicName, subName, cleanup := testTopicSetup(t, ctx)
	defer cleanup()

	topicSub := fmt.Sprintf("%s/%s", topicName, subName)

	pub, err := gosb.NewPublisher(client, topicName, gosb.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	const numMessages = 3
	msgIDs := []string{"peek-msg-1", "peek-msg-2", "peek-msg-3"}

	rawMsgs := make([]*message.RawMessage, numMessages)
	for i, id := range msgIDs {
		rawMsgs[i] = message.NewRaw(
			[]byte(fmt.Sprintf(`{"id":%q}`, id)),
			message.Attributes{
				message.AttrID:   id,
				message.AttrType: "azservicebus.integration.peek",
			},
			nil,
		)
	}
	err = pub.PublishBatch(ctx, "test", rawMsgs...)
	require.NoError(t, err)
	t.Logf("Published %d messages", numMessages)

	for range 3 {
		func() {
			sub, err := gosb.NewSubscriber(client, topicSub, "test", gosb.SubscriberConfig{
				MaxInFlight:      numMessages,
				EnablePeekMode:   true,
				PeekPollInterval: 250 * time.Millisecond,
			})
			require.NoError(t, err)

			subCtx, subCancel := context.WithCancel(ctx)

			msgChan, err := sub.Subscribe(subCtx, "test")
			require.NoError(t, err)

			defer sub.Close()
			defer subCancel()

			// Receive all messages through the peek subscriber and ack each one.
			// Ack in our pipeline signals processing is done; it must not settle with Service Bus.
			for i := range numMessages {
				select {
				case msg := <-msgChan:
					msg.Ack()
					t.Logf("Peeked message %d/%d: %s", i+1, numMessages, msg.ID())
				case <-time.After(30 * time.Second):
					t.Fatalf("Timeout waiting for peeked message %d/%d", i+1, numMessages)
				}
			}
		}()
	}

	// Verify via raw SDK that every message is still on the bus with DeliveryCount == 1.
	verifyReceiver, err := client.NewReceiverForSubscription(topicName, subName, nil)
	require.NoError(t, err)
	defer verifyReceiver.Close(ctx)

	peeked, err := verifyReceiver.PeekMessages(ctx, numMessages+1, nil)
	require.NoError(t, err)
	require.Len(t, peeked, numMessages, "all messages must still be on the bus after peek-mode processing")

	for _, m := range peeked {
		// The Go SDK sets DeliveryCount = amqpHeader.DeliveryCount + 1, so a freshly published
		// message that has never been locked or abandoned reads as 1, not 0. A value > 1 would
		// mean the message was received under PeekLock and its lock expired or was abandoned —
		// which must not happen in peek mode.
		assert.Equal(t, uint32(1), m.DeliveryCount,
			"peek mode must not lock or abandon messages; DeliveryCount > 1 means redelivery occurred (message %s has DeliveryCount=%d)",
			m.MessageID, m.DeliveryCount)
	}
}
