# Known Issues

## Critical

### 1. Ack/Nack on Closed Receiver

**Severity:** Critical
**Impact:** Silent ack failure → duplicate message processing
**Status:** ✓ Mitigated

When context is cancelled or `Close()` is called, the receiver is closed. However, messages already delivered to the output channel still hold a reference to the (now closed) receiver in their ack/nack callbacks. When the consumer calls `Ack()` or `Nack()`, the operation fails silently (error is logged but not returned). ServiceBus will redeliver the message after lock expires.

```
Timeline:
  Message received → sent to channel → context cancelled → receiver closed → consumer calls Ack() → FAILS
```

**Mitigation:** Error code check distinguishes expected failures (CodeLockLost, CodeClosed, CodeConnectionLost) from unexpected errors. Semaphore tracks in-flight messages for graceful shutdown. Clear warning logged when message will be redelivered.

### 2. Ack/Nack on Stale Receiver (after reconnect)

**Severity:** Critical
**Impact:** Silent ack failure → duplicate message processing
**Status:** ✓ Mitigated

When a connection is lost and `recreateReceiver()` is called, the old receiver is closed and a new one is created. Messages received before the reconnect still hold a reference to the old (closed) receiver. Same impact as issue #1.

```
Timeline:
  Message received with receiver A → connection lost → receiver A closed, B created → consumer calls Ack() with A → FAILS
```

**Note:** This is an AMQP limitation - messages cannot be settled on a different receiver/link than the one that received them. See [Microsoft Docs](https://learn.microsoft.com/en-us/azure/service-bus-messaging/message-transfers-locks-settlement).

**Mitigation:** Error code check logs clear warning instead of error. Message will be redelivered by ServiceBus.

## Minor

### 3. Double-close of Receivers

**Severity:** Minor
**Impact:** Error log noise
**Status:** ✓ Fixed

If `Close()` times out waiting for subscription goroutines, it proceeds to close all receivers. When the goroutine eventually exits, its `defer unregisterReceiver()` also tries to close the receiver.

**Fix:** Delete receiver from map before closing in `unregisterReceiver()`. No duplicate close attempts.

### 4. No Error Propagation from Ack/Nack

**Severity:** Minor (design limitation)
**Impact:** Consumer cannot detect or handle ack failures
**Status:** By design

The ack/nack callbacks log errors but don't propagate them to the consumer. The consumer has no way to know if acknowledgment succeeded or failed.

```go
if err := receiver.CompleteMessage(...); err != nil {
    s.logger.Error(...)  // Logged only - consumer can't know
}
```

**Note:** This is a gopipe design limitation. The `Acking` interface doesn't support error returns from Ack/Nack.

### 5. ServiceBus Message Lock Expiry

**Severity:** Minor (operational)
**Impact:** Duplicate processing if message processing exceeds lock duration
**Status:** ✓ Mitigated

ServiceBus message locks have a default duration (typically 30 seconds). If message processing takes longer, the lock expires, ServiceBus makes the message available to other consumers, and the eventual `Ack()` call fails.

**Mitigation:** `expirytime` attribute is set to earliest of LockedUntil, CE expirytime, or GracePeriod. Use `middleware.Deadline()` to enforce processing timeout. Configure appropriate lock duration in ServiceBus for long-running processing.

## By Design

### 6. Messages in Channel Not Nacked on Shutdown

**Severity:** N/A (by design)
**Impact:** Messages redelivered if consumer doesn't drain channel
**Status:** ✓ Mitigated

When context is cancelled, messages already in the output channel are not automatically nacked. The consumer is responsible for draining the channel and deciding how to handle remaining messages. If not handled, messages will be redelivered after lock expires.

**Mitigation:** Semaphore tracks unsettled messages. `Close()` waits up to GracePeriod for all messages to settle before proceeding.

## Unlikely

### 7. Race Condition in Closing Flag Check (Subscriber)

**Severity:** Unlikely
**Impact:** Theoretical only

Small window between checking `s.done` channel and calling `s.wg.Add(1)` in `Subscribe()`. In practice, the goroutine would still clean up properly via defers.

### 8. Race Condition in Closing Flag Check (Publisher)

**Severity:** Unlikely
**Impact:** Theoretical only

Small window between checking `p.done` channel and calling `p.wg.Add(1)` in `Publish()`. In practice, the operation would still complete properly.

---

# Publisher Issues

## Minor

### P1. Retry Logic Doesn't Check Context

**Severity:** Minor
**Impact:** Unnecessary retry attempts after context cancellation

When connection is lost during `NewMessageBatch` or `SendMessageBatch`, the retry logic would continue even if context was cancelled.

**Status:** ✓ Fixed - now checks `ctx.Done()` and `p.done` before retry attempts.

### P2. Inconsistent Shutdown Pattern

**Severity:** Minor
**Impact:** Code inconsistency between Publisher and Subscriber

Publisher used `closing` bool while Subscriber used `done` channel pattern.

**Status:** ✓ Fixed - Publisher now uses same `done` channel + `doneOnce` pattern as Subscriber.

## Benign (Shutdown Noise)

### P3. IdleTimerExpired on Sender Close

**Severity:** Benign
**Impact:** Error log noise during shutdown
**Status:** Expected behavior

When closing the publisher after a period of inactivity, the sender close operation fails with:
```
amqp:link:detach-forced
Description: The link '...' is force detached. Code: publisher(...). Details: IdleTimerExpired: Idle timeout: 00:10:00.
```

**Root cause:** Azure ServiceBus disconnects AMQP links after 10 minutes of inactivity. If the app sits idle (e.g., leader lock held, waiting for next seeding window) and then shuts down, the sender was already disconnected by ServiceBus.

**Why benign:** The link was already closed by ServiceBus - nothing to do. The explicit `Close()` call fails because there's no link to close.

**No fix needed:** This only affects shutdown and doesn't indicate data loss.

### P4. "Publisher is closing" During Active Publishing

**Severity:** Bug (was misclassified as benign)
**Impact:** Pending messages not sent during shutdown
**Status:** ✓ Fixed

During shutdown, batch publishing failed with:
```
Publishing batch failed error="publisher is closing" batch_size=100
```

**Root cause:** The seeding goroutine exited immediately on context cancellation without waiting for the batch pipes to drain. The app then closed the publisher while batches were still queued.

**Sequence (before fix):**
```
Ctrl+C → leaderCtx cancelled → goroutine exits immediately → seedingTracker.Stop()
→ app closes publisher → batch pipe still draining → PublishBatch hits p.done check → ERROR
```

**Fix:** Wait for `fetchPublishDone` (ERP data) after context cancellation. Seeds (`publishDone`) can be dropped - they're cheap and regenerated periodically.

**Sequence (after fix):**
```
Ctrl+C → leaderCtx cancelled → ERP batch pipe drains → wait for fetchPublishDone
→ expensive ERP data published → seedingTracker.Stop() → app closes publisher → clean shutdown
```

**Design rationale:**
- Seeds (article IDs): Disposable, regenerated every seeding cycle
- ERP data (prices/availability): Expensive API calls, must not lose after fetch

---

# TODOs: Issue Mitigations

## 1. Simplify Timeout Configuration

**Addresses:** All timeout-related complexity, issue #6

Replace fragmented timeouts with two clear settings:

```go
type SubscriberConfig struct {
    // GracePeriod is the maximum time allowed for message processing.
    // Used for: graceful shutdown wait, ack/nack operations, expirytime on messages.
    // Defaults to 30 seconds.
    GracePeriod time.Duration

    // OperationTimeout is the timeout for quick ServiceBus operations
    // (closing receivers during reconnect/shutdown).
    // Defaults to 5 seconds.
    OperationTimeout time.Duration
}
```

**Migration:**
| Old | New |
|-----|-----|
| `CompleteTimeout` | `GracePeriod` |
| `AbandonTimeout` | `GracePeriod` |
| `CloseTimeout` | `GracePeriod` |
| `RecreateTimeout` | `OperationTimeout` |

Same for `PublisherConfig`.

## 2. Track In-Flight Messages with Semaphore

**Addresses:** Issue #1, #6

Use `golang.org/x/sync/semaphore` to track unsettled messages. This provides:
1. **Concurrency limit** - total unsettled messages capped at MaxInFlight
2. **Graceful shutdown** - Acquire all slots with context timeout

```go
import "golang.org/x/sync/semaphore"

type Subscriber struct {
    // ... existing fields ...
    settlement *semaphore.Weighted  // capacity = MaxInFlight
}

func NewSubscriber(...) {
    // ...
    settlement: semaphore.NewWeighted(int64(config.MaxInFlight)),
}
```

Acquire before sending to channel, release in ack/nack:

```go
// Before sending to channel:
if err := s.settlement.Acquire(ctx, 1); err != nil {
    return  // context cancelled
}
msgChan <- msg

// In ack/nack callbacks:
defer s.settlement.Release(1)
```

Update `Close()` to wait for settlements with context timeout:

```go
func (s *Subscriber) Close() error {
    s.closing = true

    // Phase 1: Wait for receive goroutines to exit
    // ... existing wg.Wait() logic ...

    // Phase 2: Wait for in-flight messages to settle
    ctx, cancel := context.WithTimeout(context.Background(), s.config.GracePeriod)
    defer cancel()
    if err := s.settlement.Acquire(ctx, int64(s.config.MaxInFlight)); err != nil {
        // context.DeadlineExceeded - some messages not settled
        s.logger.Warn("Closing with unsettled messages",
            "note", "messages will be redelivered by ServiceBus")
    }

    // Phase 3: Close receivers
    // ...
}
```

## 3. Improve Error Handling in Ack/Nack Callbacks

**Addresses:** Issue #1, #2

Use SDK error codes to distinguish expected failures (shutdown/reconnect) from unexpected errors:

```go
func() {
    completeCtx, cancel := context.WithTimeout(context.Background(), s.config.GracePeriod)
    defer cancel()

    if err := receiver.CompleteMessage(completeCtx, sbMsg, nil); err != nil {
        // Check for expected errors during shutdown/reconnect
        var sbErr *azservicebus.Error
        if errors.As(err, &sbErr) {
            switch sbErr.Code {
            case azservicebus.CodeLockLost, azservicebus.CodeClosed, azservicebus.CodeConnectionLost:
                // Expected - message will be redelivered by ServiceBus
                s.logger.Warn("Message will be redelivered",
                    "reason", sbErr.Code, "topic", topic, "message_id", msgID)
                return
            }
        }
        // Unexpected error
        s.logger.Error("Failed to complete message", "error", err, "topic", topic, "message_id", msgID)
    }
}
```

**No generation tracking needed** - the SDK already tells us when the receiver is invalid.

## 4. Set expirytime from GracePeriod

**Addresses:** Issue #5, context propagation

When receiving messages, set `expirytime` to the earliest deadline:

```go
func (s *Subscriber) toGopipeAttrs(sbMsg *azservicebus.ReceivedMessage) message.Attributes {
    attrs := message.Attributes{}

    // ... existing attribute mapping ...

    // Set expirytime to earliest deadline (as time.Time, not string)
    // Priority: earliest of cloudEvents:expirytime, LockedUntil, GracePeriod
    expiry := time.Now().Add(s.config.GracePeriod)

    if ceExpiry, hasExpiry := attrs[message.AttrExpiryTime]; hasExpiry {
        if expiryStr, ok := ceExpiry.(string); ok {
            if parsed, err := time.Parse(time.RFC3339, expiryStr); err == nil && parsed.Before(expiry) {
                expiry = parsed
            }
        }
    }

    // Use LockedUntil (not ExpiresAt) because:
    // 1. Lock protects message from expiration during processing
    // 2. LockedUntil is when we lose ability to Ack/Nack
    if sbMsg.LockedUntil != nil && !sbMsg.LockedUntil.IsZero() && sbMsg.LockedUntil.Before(expiry) {
        expiry = *sbMsg.LockedUntil
    }

    attrs[message.AttrExpiryTime] = expiry  // time.Time, not string

    return attrs
}
```

This enables `middleware.Deadline()` to enforce processing timeout.

## 5. Fix Double-Close of Receivers

**Addresses:** Issue #3

Delete receiver from map before closing to prevent double-close:

```go
func (s *Subscriber) unregisterReceiver(queueOrTopic string) {
    s.receiverLock.Lock()
    receiver, exists := s.receiver[queueOrTopic]
    if !exists {
        s.receiverLock.Unlock()
        return
    }
    delete(s.receiver, queueOrTopic)  // Remove from map first
    s.receiverLock.Unlock()

    // Close outside lock
    ctx, cancel := context.WithTimeout(context.Background(), s.config.OperationTimeout)
    defer cancel()
    if err := receiver.Close(ctx); err != nil {
        s.logger.Warn("Failed to close receiver", "error", err, "topic", queueOrTopic)
    }
}
```

## Implementation Order

1. ~~**Improve error handling**~~ ✓ (done)
2. ~~**Simplify timeout config**~~ ✓ (done) - GracePeriod + OperationTimeout
3. ~~**Semaphore for settlement tracking**~~ ✓ (done)
4. ~~**Set expirytime from GracePeriod**~~ ✓ (done) - uses earliest of LockedUntil, CE expirytime, or GracePeriod
5. ~~**Fix double-close**~~ ✓ (done) - replaced `closing` bool with `done` channel for graceful shutdown
6. ~~**Publisher consistency**~~ ✓ (done) - same `done` channel pattern, context checks in retry logic

## Summary

### Subscriber Issues

| Issue | Status | Mitigation | Outcome |
|-------|--------|------------|---------|
| #1 Ack on closed receiver | ✓ Mitigated | Error code check + semaphore | Clear warning, graceful shutdown |
| #2 Ack on stale receiver | ✓ Mitigated | Error code check | Clear warning instead of error |
| #3 Double-close | ✓ Fixed | Delete from map before close | No duplicate close attempts |
| #4 No error propagation | By design | N/A (gopipe limitation) | Document in godoc |
| #5 Lock expiry | ✓ Mitigated | LockedUntil → expirytime | Deadline middleware works |
| #6 Messages in channel | ✓ Mitigated | Semaphore + Acquire(max) | Graceful shutdown |
| #7 Race condition | Unlikely | N/A | No change needed |

### Publisher Issues

| Issue | Status | Mitigation | Outcome |
|-------|--------|------------|---------|
| P1 Retry without context check | ✓ Fixed | Check ctx.Done() + p.done | No unnecessary retries |
| P2 Inconsistent shutdown pattern | ✓ Fixed | Use done channel + doneOnce | Consistent with Subscriber |
| P3 IdleTimerExpired on close | Benign | N/A (ServiceBus behavior) | Log noise only |
| P4 "Publisher is closing" | ✓ Fixed | Wait for batch pipes to drain | Clean shutdown |
