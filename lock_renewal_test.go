package azservicebus_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/stretchr/testify/require"
)

// TestLockedUntilBehavior verifies how LockedUntil behaves when RenewMessageLock is called.
// This is critical for understanding how to implement lock renewal correctly.
func TestLockedUntilBehavior(t *testing.T) {
	skipIfNoServiceBus(t)

	if testQueue1 == "" {
		t.Skip("Skipping: SERVICEBUS_TEST_QUEUE_1 not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create sender and receiver
	sender, err := client.NewSender(testQueue1, nil)
	require.NoError(t, err)
	defer sender.Close(ctx)

	receiver, err := client.NewReceiverForQueue(testQueue1, &azservicebus.ReceiverOptions{
		ReceiveMode: azservicebus.ReceiveModePeekLock,
	})
	require.NoError(t, err)
	defer receiver.Close(ctx)

	// Send a test message
	testBody := []byte(`{"test": "lock_renewal_behavior"}`)
	err = sender.SendMessage(ctx, &azservicebus.Message{Body: testBody}, nil)
	require.NoError(t, err)
	t.Log("Sent test message")

	// Receive the message
	msgs, err := receiver.ReceiveMessages(ctx, 1, nil)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	msg := msgs[0]

	// Log initial state
	t.Logf("Message received at: %s", time.Now().Format(time.RFC3339Nano))
	t.Logf("Initial LockedUntil: %v", msg.LockedUntil)

	if msg.LockedUntil != nil {
		initialLockDuration := time.Until(*msg.LockedUntil)
		t.Logf("Initial lock duration (time until expiry): %s", initialLockDuration)
	} else {
		t.Log("LockedUntil is nil!")
	}

	// Store initial LockedUntil for comparison
	initialLockedUntil := msg.LockedUntil

	// Wait a bit to make the difference clear
	time.Sleep(2 * time.Second)
	t.Logf("After 2s sleep, time.Now(): %s", time.Now().Format(time.RFC3339Nano))

	// Renew the lock
	t.Log("Calling RenewMessageLock...")
	err = receiver.RenewMessageLock(ctx, msg, nil)
	require.NoError(t, err)
	t.Log("RenewMessageLock succeeded")

	// Check if LockedUntil was updated
	t.Logf("After renewal, msg.LockedUntil: %v", msg.LockedUntil)

	if msg.LockedUntil != nil && initialLockedUntil != nil {
		if msg.LockedUntil.Equal(*initialLockedUntil) {
			t.Log("RESULT: LockedUntil was NOT updated by RenewMessageLock")
		} else {
			t.Logf("RESULT: LockedUntil WAS updated by RenewMessageLock")
			t.Logf("  Initial: %s", initialLockedUntil.Format(time.RFC3339Nano))
			t.Logf("  After:   %s", msg.LockedUntil.Format(time.RFC3339Nano))
			t.Logf("  Difference: %s", msg.LockedUntil.Sub(*initialLockedUntil))
		}

		newLockDuration := time.Until(*msg.LockedUntil)
		t.Logf("New lock duration (time until expiry): %s", newLockDuration)
	}

	// Clean up - complete the message
	err = receiver.CompleteMessage(ctx, msg, nil)
	require.NoError(t, err)
	t.Log("Message completed")
}

// TestMultipleLockRenewals verifies that we can renew the lock multiple times
// and that LockedUntil is updated each time.
func TestMultipleLockRenewals(t *testing.T) {
	skipIfNoServiceBus(t)

	if testQueue1 == "" {
		t.Skip("Skipping: SERVICEBUS_TEST_QUEUE_1 not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	sender, err := client.NewSender(testQueue1, nil)
	require.NoError(t, err)
	defer sender.Close(ctx)

	receiver, err := client.NewReceiverForQueue(testQueue1, &azservicebus.ReceiverOptions{
		ReceiveMode: azservicebus.ReceiveModePeekLock,
	})
	require.NoError(t, err)
	defer receiver.Close(ctx)

	// Send a test message
	err = sender.SendMessage(ctx, &azservicebus.Message{Body: []byte(`{"test": "multiple_renewals"}`)}, nil)
	require.NoError(t, err)

	// Receive the message
	msgs, err := receiver.ReceiveMessages(ctx, 1, nil)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	msg := msgs[0]
	receiveTime := time.Now()

	t.Logf("Received at: %s", receiveTime.Format(time.RFC3339))
	require.NotNil(t, msg.LockedUntil, "LockedUntil should not be nil")

	initialLockDuration := msg.LockedUntil.Sub(receiveTime)
	t.Logf("Initial lock duration: %s", initialLockDuration)

	// Renew multiple times with 5 second intervals
	for i := 1; i <= 3; i++ {
		time.Sleep(5 * time.Second)

		beforeRenewal := *msg.LockedUntil
		beforeRenewalTimeUntil := time.Until(beforeRenewal)

		err = receiver.RenewMessageLock(ctx, msg, nil)
		require.NoError(t, err)

		afterRenewal := *msg.LockedUntil
		afterRenewalTimeUntil := time.Until(afterRenewal)

		t.Logf("Renewal %d:", i)
		t.Logf("  Before: %s (expires in %s)", beforeRenewal.Format(time.RFC3339), beforeRenewalTimeUntil)
		t.Logf("  After:  %s (expires in %s)", afterRenewal.Format(time.RFC3339), afterRenewalTimeUntil)
		t.Logf("  Extended by: %s", afterRenewal.Sub(beforeRenewal))

		// Verify lock was extended
		require.True(t, afterRenewal.After(beforeRenewal), "LockedUntil should be extended after renewal")
	}

	// Complete the message
	err = receiver.CompleteMessage(ctx, msg, nil)
	require.NoError(t, err)
	t.Log("Message completed successfully after multiple renewals")
}

// TestLockDurationCalculation verifies we can accurately calculate lock duration
// from the LockedUntil field at receive time.
func TestLockDurationCalculation(t *testing.T) {
	skipIfNoServiceBus(t)

	if testQueue1 == "" {
		t.Skip("Skipping: SERVICEBUS_TEST_QUEUE_1 not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sender, err := client.NewSender(testQueue1, nil)
	require.NoError(t, err)
	defer sender.Close(ctx)

	receiver, err := client.NewReceiverForQueue(testQueue1, &azservicebus.ReceiverOptions{
		ReceiveMode: azservicebus.ReceiveModePeekLock,
	})
	require.NoError(t, err)
	defer receiver.Close(ctx)

	// Send a test message
	err = sender.SendMessage(ctx, &azservicebus.Message{Body: []byte(`{"test": "lock_duration_calc"}`)}, nil)
	require.NoError(t, err)

	// Receive and immediately calculate lock duration
	receiveStart := time.Now()
	msgs, err := receiver.ReceiveMessages(ctx, 1, nil)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	receiveEnd := time.Now()

	msg := msgs[0]
	require.NotNil(t, msg.LockedUntil)

	// Calculate lock duration using different reference points
	lockDurationFromStart := msg.LockedUntil.Sub(receiveStart)
	lockDurationFromEnd := msg.LockedUntil.Sub(receiveEnd)
	lockDurationTimeUntil := time.Until(*msg.LockedUntil)

	t.Logf("LockedUntil: %s", msg.LockedUntil.Format(time.RFC3339Nano))
	t.Logf("Lock duration (from receive start): %s", lockDurationFromStart)
	t.Logf("Lock duration (from receive end):   %s", lockDurationFromEnd)
	t.Logf("Lock duration (time.Until):         %s", lockDurationTimeUntil)
	t.Logf("Receive operation took:             %s", receiveEnd.Sub(receiveStart))

	// The lock duration should be approximately 60 seconds (default Service Bus lock)
	// Allow for some variance due to network latency
	expectedLockDuration := 60 * time.Second
	tolerance := 5 * time.Second

	t.Logf("\nAnalysis:")
	t.Logf("  Expected lock duration: %s", expectedLockDuration)
	t.Logf("  Calculated (time.Until): %s", lockDurationTimeUntil)
	t.Logf("  Difference from expected: %s", lockDurationTimeUntil-expectedLockDuration)

	require.InDelta(t, expectedLockDuration.Seconds(), lockDurationTimeUntil.Seconds(), tolerance.Seconds(),
		"Lock duration should be approximately %s", expectedLockDuration)

	// Clean up
	err = receiver.CompleteMessage(ctx, msg, nil)
	require.NoError(t, err)
}
