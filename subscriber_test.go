package azservicebus

import (
	"testing"
	"time"
)

func TestSubscriberConfig_setDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   SubscriberConfig
		expected SubscriberConfig
	}{
		{
			name:   "empty config should use defaults",
			config: SubscriberConfig{},
			expected: SubscriberConfig{
				ReceiveTimeout:  30 * time.Second,
				AckTimeout:      30 * time.Second,
				CloseTimeout:    30 * time.Second,
				MaxMessageCount: 10,
				BufferSize:      100,
				PollInterval:    1 * time.Second,
			},
		},
		{
			name: "custom values should be preserved",
			config: SubscriberConfig{
				ReceiveTimeout:  60 * time.Second,
				AckTimeout:      60 * time.Second,
				CloseTimeout:    60 * time.Second,
				MaxMessageCount: 20,
				BufferSize:      200,
				PollInterval:    2 * time.Second,
			},
			expected: SubscriberConfig{
				ReceiveTimeout:  60 * time.Second,
				AckTimeout:      60 * time.Second,
				CloseTimeout:    60 * time.Second,
				MaxMessageCount: 20,
				BufferSize:      200,
				PollInterval:    2 * time.Second,
			},
		},
		{
			name: "partial config should fill missing defaults",
			config: SubscriberConfig{
				ReceiveTimeout:  45 * time.Second,
				MaxMessageCount: 5,
			},
			expected: SubscriberConfig{
				ReceiveTimeout:  45 * time.Second,
				AckTimeout:      30 * time.Second,
				CloseTimeout:    30 * time.Second,
				MaxMessageCount: 5,
				BufferSize:      100,
				PollInterval:    1 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config
			config.setDefaults()

			if config.ReceiveTimeout != tt.expected.ReceiveTimeout {
				t.Errorf("ReceiveTimeout = %v, want %v", config.ReceiveTimeout, tt.expected.ReceiveTimeout)
			}
			if config.AckTimeout != tt.expected.AckTimeout {
				t.Errorf("AckTimeout = %v, want %v", config.AckTimeout, tt.expected.AckTimeout)
			}
			if config.CloseTimeout != tt.expected.CloseTimeout {
				t.Errorf("CloseTimeout = %v, want %v", config.CloseTimeout, tt.expected.CloseTimeout)
			}
			if config.MaxMessageCount != tt.expected.MaxMessageCount {
				t.Errorf("MaxMessageCount = %d, want %d", config.MaxMessageCount, tt.expected.MaxMessageCount)
			}
			if config.BufferSize != tt.expected.BufferSize {
				t.Errorf("BufferSize = %d, want %d", config.BufferSize, tt.expected.BufferSize)
			}
			if config.PollInterval != tt.expected.PollInterval {
				t.Errorf("PollInterval = %v, want %v", config.PollInterval, tt.expected.PollInterval)
			}
		})
	}
}

func TestMultiSubscriberConfig_setDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   MultiSubscriberConfig
		expected MultiSubscriberConfig
	}{
		{
			name:   "empty config should use defaults",
			config: MultiSubscriberConfig{},
			expected: MultiSubscriberConfig{
				SubscriberConfig: SubscriberConfig{
					ReceiveTimeout:  30 * time.Second,
					AckTimeout:      30 * time.Second,
					CloseTimeout:    30 * time.Second,
					MaxMessageCount: 10,
					BufferSize:      100,
					PollInterval:    1 * time.Second,
				},
				MergeBufferSize: 1000,
			},
		},
		{
			name: "custom merge buffer should be preserved",
			config: MultiSubscriberConfig{
				MergeBufferSize: 500,
			},
			expected: MultiSubscriberConfig{
				SubscriberConfig: SubscriberConfig{
					ReceiveTimeout:  30 * time.Second,
					AckTimeout:      30 * time.Second,
					CloseTimeout:    30 * time.Second,
					MaxMessageCount: 10,
					BufferSize:      100,
					PollInterval:    1 * time.Second,
				},
				MergeBufferSize: 500,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config
			config.setDefaults()

			if config.MergeBufferSize != tt.expected.MergeBufferSize {
				t.Errorf("MergeBufferSize = %d, want %d", config.MergeBufferSize, tt.expected.MergeBufferSize)
			}
			if config.ReceiveTimeout != tt.expected.ReceiveTimeout {
				t.Errorf("ReceiveTimeout = %v, want %v", config.ReceiveTimeout, tt.expected.ReceiveTimeout)
			}
		})
	}
}

func TestErrSubscriberClosed(t *testing.T) {
	err := ErrSubscriberClosed

	if err == nil {
		t.Error("ErrSubscriberClosed should not be nil")
	}

	expected := "subscriber is closed"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestSubscriberError(t *testing.T) {
	err := newError("test error")

	if err == nil {
		t.Error("newError should not return nil")
	}

	if err.Error() != "test error" {
		t.Errorf("Error() = %q, want %q", err.Error(), "test error")
	}
}
