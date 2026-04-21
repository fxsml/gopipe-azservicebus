package azservicebus

import (
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/fxsml/gopipe/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers

func newTestMsg(attrs message.Attributes) *message.RawMessage {
	return message.NewRaw([]byte(`{}`), attrs, nil)
}

func staticStr(s string) func(*message.RawMessage) string {
	return func(*message.RawMessage) string { return s }
}

func newTestPublisher(pp PublisherProperties) *Publisher {
	return &Publisher{config: PublisherConfig{Properties: pp}.withDefaults()}
}

func newReceivedMessage() *azservicebus.ReceivedMessage {
	return &azservicebus.ReceivedMessage{}
}

// PublisherProperties unit tests

func TestToServiceBusMessage_PublisherPropertiesStatic(t *testing.T) {
	p := newTestPublisher(PublisherProperties{
		SessionID:        staticStr("sess-1"),
		ReplyTo:          staticStr("reply-queue"),
		ReplyToSessionID: staticStr("reply-sess"),
		To:               staticStr("dest-queue"),
		Subject:          staticStr("my-subject"),
		PartitionKey:     staticStr("part-key"),
	})
	sbMsg := p.toServiceBusMessage(newTestMsg(message.Attributes{message.AttrID: "id"}))

	require.NotNil(t, sbMsg.SessionID)
	assert.Equal(t, "sess-1", *sbMsg.SessionID)
	require.NotNil(t, sbMsg.ReplyTo)
	assert.Equal(t, "reply-queue", *sbMsg.ReplyTo)
	require.NotNil(t, sbMsg.ReplyToSessionID)
	assert.Equal(t, "reply-sess", *sbMsg.ReplyToSessionID)
	require.NotNil(t, sbMsg.To)
	assert.Equal(t, "dest-queue", *sbMsg.To)
	require.NotNil(t, sbMsg.Subject)
	assert.Equal(t, "my-subject", *sbMsg.Subject)
	require.NotNil(t, sbMsg.PartitionKey)
	assert.Equal(t, "part-key", *sbMsg.PartitionKey)
}

func TestToServiceBusMessage_PublisherPropertiesFromMessage(t *testing.T) {
	p := newTestPublisher(PublisherProperties{
		SessionID: func(msg *message.RawMessage) string {
			v, _ := msg.Attributes["sessionid"].(string)
			return v
		},
		Subject: func(msg *message.RawMessage) string {
			v, _ := msg.Attributes["subject"].(string)
			return v
		},
	})
	sbMsg := p.toServiceBusMessage(newTestMsg(message.Attributes{
		message.AttrID: "id",
		"sessionid":    "sess-from-attr",
		"subject":      "subject-from-attr",
	}))

	require.NotNil(t, sbMsg.SessionID)
	assert.Equal(t, "sess-from-attr", *sbMsg.SessionID)
	require.NotNil(t, sbMsg.Subject)
	assert.Equal(t, "subject-from-attr", *sbMsg.Subject)
}

func TestToServiceBusMessage_PublisherPropertiesNilFuncSkipped(t *testing.T) {
	p := newTestPublisher(PublisherProperties{})
	sbMsg := p.toServiceBusMessage(newTestMsg(message.Attributes{message.AttrID: "id"}))

	assert.Nil(t, sbMsg.SessionID)
	assert.Nil(t, sbMsg.ReplyTo)
	assert.Nil(t, sbMsg.ReplyToSessionID)
	assert.Nil(t, sbMsg.To)
	assert.Nil(t, sbMsg.Subject)
	assert.Nil(t, sbMsg.PartitionKey)
	assert.Nil(t, sbMsg.TimeToLive)
	assert.Nil(t, sbMsg.ScheduledEnqueueTime)
}

func TestToServiceBusMessage_PublisherPropertiesEmptyStringSkipped(t *testing.T) {
	p := newTestPublisher(PublisherProperties{
		Subject: func(*message.RawMessage) string { return "" },
	})
	sbMsg := p.toServiceBusMessage(newTestMsg(message.Attributes{message.AttrID: "id"}))
	assert.Nil(t, sbMsg.Subject)
}

func TestToServiceBusMessage_PublisherPropertiesTimeToLive(t *testing.T) {
	p := newTestPublisher(PublisherProperties{
		TimeToLive: func(*message.RawMessage) time.Duration { return 5 * time.Minute },
	})
	sbMsg := p.toServiceBusMessage(newTestMsg(message.Attributes{message.AttrID: "id"}))

	require.NotNil(t, sbMsg.TimeToLive)
	assert.Equal(t, 5*time.Minute, *sbMsg.TimeToLive)
}

func TestToServiceBusMessage_PublisherPropertiesTimeToLiveZeroSkipped(t *testing.T) {
	p := newTestPublisher(PublisherProperties{
		TimeToLive: func(*message.RawMessage) time.Duration { return 0 },
	})
	sbMsg := p.toServiceBusMessage(newTestMsg(message.Attributes{message.AttrID: "id"}))
	assert.Nil(t, sbMsg.TimeToLive)
}

func TestToServiceBusMessage_PublisherPropertiesTimeToLiveFromExpiryTime(t *testing.T) {
	p := newTestPublisher(PublisherProperties{
		TimeToLive: func(msg *message.RawMessage) time.Duration {
			if exp := msg.ExpiryTime(); !exp.IsZero() {
				if d := time.Until(exp); d > 0 {
					return d
				}
			}
			return 0
		},
	})
	expiry := time.Now().Add(10 * time.Minute)
	sbMsg := p.toServiceBusMessage(newTestMsg(message.Attributes{
		message.AttrID:         "id",
		message.AttrExpiryTime: expiry,
	}))

	require.NotNil(t, sbMsg.TimeToLive)
	assert.InDelta(t, (10 * time.Minute).Seconds(), sbMsg.TimeToLive.Seconds(), 1)
}

func TestToServiceBusMessage_ScheduledEnqueueTime(t *testing.T) {
	delay := 5 * time.Minute
	p := newTestPublisher(PublisherProperties{
		ScheduledEnqueueTime: func(*message.RawMessage) time.Time {
			return time.Now().UTC().Add(delay)
		},
	})

	before := time.Now().UTC()
	sbMsg := p.toServiceBusMessage(newTestMsg(message.Attributes{message.AttrID: "id"}))
	after := time.Now().UTC()

	require.NotNil(t, sbMsg.ScheduledEnqueueTime)
	assert.WithinDuration(t, before.Add(delay), *sbMsg.ScheduledEnqueueTime, after.Sub(before)+time.Millisecond)
}

func TestToServiceBusMessage_ScheduledEnqueueTimeZeroSkipped(t *testing.T) {
	p := newTestPublisher(PublisherProperties{
		ScheduledEnqueueTime: func(*message.RawMessage) time.Time { return time.Time{} },
	})
	sbMsg := p.toServiceBusMessage(newTestMsg(message.Attributes{message.AttrID: "id"}))
	assert.Nil(t, sbMsg.ScheduledEnqueueTime)
}

// SubscriberProperties unit tests

func TestSubscriberProperties_NilFuncSkipped(t *testing.T) {
	sbMsg := newReceivedMessage()
	sessID := "sess-1"
	sbMsg.SessionID = &sessID

	msg := message.NewRaw([]byte{}, message.Attributes{}, nil)
	sp := SubscriberProperties{}
	sp.apply(sbMsg, msg)

	_, hasSession := msg.Attributes["sessionid"]
	assert.False(t, hasSession, "nil SessionID func should not write to attributes")
}

func TestSubscriberProperties_NilBrokerFieldSkipped(t *testing.T) {
	sbMsg := newReceivedMessage() // SessionID is nil

	called := false
	msg := message.NewRaw([]byte{}, message.Attributes{}, nil)
	sp := SubscriberProperties{
		SessionID: func(v string, m *message.RawMessage) { called = true },
	}
	sp.apply(sbMsg, msg)

	assert.False(t, called, "func should not be called when broker field is nil")
}

func TestSubscriberProperties_SessionID(t *testing.T) {
	sbMsg := newReceivedMessage()
	sessID := "sess-from-broker"
	sbMsg.SessionID = &sessID

	msg := message.NewRaw([]byte{}, message.Attributes{}, nil)
	sp := SubscriberProperties{
		SessionID: func(v string, m *message.RawMessage) { m.Attributes["sessionid"] = v },
	}
	sp.apply(sbMsg, msg)

	assert.Equal(t, "sess-from-broker", msg.Attributes["sessionid"])
}

func TestSubscriberProperties_AllStringFields(t *testing.T) {
	sbMsg := newReceivedMessage()
	strPtr := func(s string) *string { return &s }
	sbMsg.SessionID = strPtr("sess")
	sbMsg.ReplyTo = strPtr("reply-q")
	sbMsg.ReplyToSessionID = strPtr("reply-sess")
	sbMsg.To = strPtr("dest")
	sbMsg.Subject = strPtr("subj")
	sbMsg.PartitionKey = strPtr("pk")

	msg := message.NewRaw([]byte{}, message.Attributes{}, nil)
	sp := SubscriberProperties{
		SessionID:        func(v string, m *message.RawMessage) { m.Attributes["sessionid"] = v },
		ReplyTo:          func(v string, m *message.RawMessage) { m.Attributes["replyto"] = v },
		ReplyToSessionID: func(v string, m *message.RawMessage) { m.Attributes["replytosessionid"] = v },
		To:               func(v string, m *message.RawMessage) { m.Attributes["to"] = v },
		Subject:          func(v string, m *message.RawMessage) { m.Attributes["subject"] = v },
		PartitionKey:     func(v string, m *message.RawMessage) { m.Attributes["partitionkey"] = v },
	}
	sp.apply(sbMsg, msg)

	assert.Equal(t, "sess", msg.Attributes["sessionid"])
	assert.Equal(t, "reply-q", msg.Attributes["replyto"])
	assert.Equal(t, "reply-sess", msg.Attributes["replytosessionid"])
	assert.Equal(t, "dest", msg.Attributes["to"])
	assert.Equal(t, "subj", msg.Attributes["subject"])
	assert.Equal(t, "pk", msg.Attributes["partitionkey"])
}

func TestSubscriberProperties_TimeToLive(t *testing.T) {
	sbMsg := newReceivedMessage()
	ttl := 10 * time.Minute
	sbMsg.TimeToLive = &ttl

	msg := message.NewRaw([]byte{}, message.Attributes{}, nil)
	sp := SubscriberProperties{
		TimeToLive: func(d time.Duration, m *message.RawMessage) {
			m.Attributes[message.AttrExpiryTime] = time.Now().UTC().Add(d)
		},
	}
	sp.apply(sbMsg, msg)

	exp, ok := msg.Attributes[message.AttrExpiryTime].(time.Time)
	require.True(t, ok, "ExpiryTime should be a time.Time")
	assert.WithinDuration(t, time.Now().UTC().Add(ttl), exp, time.Second)
}

func TestSubscriberProperties_ScheduledEnqueueTime(t *testing.T) {
	sbMsg := newReceivedMessage()
	scheduled := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
	sbMsg.ScheduledEnqueueTime = &scheduled

	msg := message.NewRaw([]byte{}, message.Attributes{}, nil)
	sp := SubscriberProperties{
		ScheduledEnqueueTime: func(t time.Time, m *message.RawMessage) {
			m.Attributes["scheduledenqueuetime"] = t
		},
	}
	sp.apply(sbMsg, msg)

	got, ok := msg.Attributes["scheduledenqueuetime"].(time.Time)
	require.True(t, ok)
	assert.Equal(t, scheduled, got)
}
