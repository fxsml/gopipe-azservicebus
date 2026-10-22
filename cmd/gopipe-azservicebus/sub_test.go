package main

import (
	"bufio"
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus/admin"
	servicebus "github.com/fxsml/gopipe-azservicebus"
	"github.com/fxsml/gopipe/message"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLimitMessages_StopsAtN is a pure unit test (no network) for the helper
// backing the sub command's --limit flag: it must forward exactly n messages,
// close the output channel, and call cancel exactly once.
func TestLimitMessages_StopsAtN(t *testing.T) {
	in := make(chan *message.Message)
	go func() {
		defer close(in)
		for i := range 5 {
			in <- message.NewRaw(nil, message.Attributes{message.AttrID: fmt.Sprintf("msg-%d", i)}, nil)
		}
	}()

	cancelCount := 0
	cancel := func() { cancelCount++ }

	out := limitMessages(in, 2, cancel)

	var received []string
	for msg := range out {
		received = append(received, msg.ID())
	}

	assert.Equal(t, []string{"msg-0", "msg-1"}, received, "should forward exactly n messages")
	assert.Equal(t, 1, cancelCount, "cancel should be called exactly once after the nth message")
}

// TestLimitMessages_ClosesEarlyIfInputShorterThanLimit verifies that when
// fewer messages arrive than the limit, the output simply mirrors the input
// and cancel is never called.
func TestLimitMessages_ClosesEarlyIfInputShorterThanLimit(t *testing.T) {
	in := make(chan *message.Message)
	go func() {
		defer close(in)
		in <- message.NewRaw(nil, message.Attributes{message.AttrID: "only-msg"}, nil)
	}()

	cancelCount := 0
	cancel := func() { cancelCount++ }

	out := limitMessages(in, 5, cancel)

	var received []string
	for msg := range out {
		received = append(received, msg.ID())
	}

	assert.Equal(t, []string{"only-msg"}, received)
	assert.Equal(t, 0, cancelCount, "cancel should not be called when input closes before reaching the limit")
}

// TestSubCommand_LimitStopsAtN publishes more messages than --limit allows and
// verifies the sub command exits after exactly --limit messages, rather than
// draining the whole queue until --timeout fires.
func TestSubCommand_LimitStopsAtN(t *testing.T) {
	_ = godotenv.Load("../../.env.test")

	connString := os.Getenv("SERVICEBUS_CONNECTION")
	mgmtConnString := os.Getenv("SERVICEBUS_CONNECTION_MANAGEMENT")
	if connString == "" || mgmtConnString == "" {
		t.Skip("Skipping: SERVICEBUS_CONNECTION/SERVICEBUS_CONNECTION_MANAGEMENT not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adminClient, err := admin.NewClientFromConnectionString(mgmtConnString, nil)
	require.NoError(t, err)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	queueName := fmt.Sprintf("v2-cli-limit-test-%d", rng.Intn(100000))
	_, err = adminClient.CreateQueue(ctx, queueName, nil)
	require.NoError(t, err)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = adminClient.DeleteQueue(cleanupCtx, queueName, nil)
	}()

	client, err := servicebus.NewClient(connString)
	require.NoError(t, err)
	defer client.Close(ctx)

	pub, err := servicebus.NewPublisher(client, queueName, servicebus.PublisherConfig{})
	require.NoError(t, err)
	defer pub.Close()

	const totalMessages = 5
	const limit = 2

	msgs := make([]*message.Message, totalMessages)
	for i := range msgs {
		msgs[i] = message.NewRaw(
			[]byte(fmt.Sprintf(`{"n":%d}`, i)),
			message.Attributes{message.AttrID: fmt.Sprintf("limit-test-msg-%d", i)},
			nil,
		)
	}
	require.NoError(t, pub.PublishBatch(ctx, "test", msgs...))
	t.Logf("Published %d messages to queue %s", totalMessages, queueName)

	outFile, err := os.CreateTemp(t.TempDir(), "sub-limit-*.jsonl")
	require.NoError(t, err)
	require.NoError(t, outFile.Close())

	args := []string{
		"gopipe-azservicebus",
		"--connection", connString,
		"sub",
		"--subscription", queueName,
		"--limit", fmt.Sprintf("%d", limit),
		"--timeout", "8s",
		"--output-file", outFile.Name(),
	}

	runCtx, runCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer runCancel()

	start := time.Now()
	err = rootCmd.Run(runCtx, args)
	elapsed := time.Since(start)
	require.NoError(t, err)
	t.Logf("sub command finished after %s", elapsed)

	lines := readNonEmptyLines(t, outFile.Name())
	require.Lenf(t, lines, limit,
		"sub --limit=%d should stop after %d messages, got %d (queue had %d available)",
		limit, limit, len(lines), totalMessages)
}

func readNonEmptyLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			lines = append(lines, line)
		}
	}
	require.NoError(t, sc.Err())
	return lines
}
