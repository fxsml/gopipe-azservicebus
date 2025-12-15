package azservicebus

import (
	"testing"
	"time"
)

func TestPublisherConfig_setDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   PublisherConfig
		expected PublisherConfig
	}{
		{
			name:   "empty config should use defaults",
			config: PublisherConfig{},
			expected: PublisherConfig{
				BatchSize:    10,
				BatchTimeout: 100 * time.Millisecond,
				SendTimeout:  30 * time.Second,
				CloseTimeout: 30 * time.Second,
			},
		},
		{
			name: "custom values should be preserved",
			config: PublisherConfig{
				BatchSize:    5,
				BatchTimeout: 200 * time.Millisecond,
				SendTimeout:  60 * time.Second,
				CloseTimeout: 60 * time.Second,
			},
			expected: PublisherConfig{
				BatchSize:    5,
				BatchTimeout: 200 * time.Millisecond,
				SendTimeout:  60 * time.Second,
				CloseTimeout: 60 * time.Second,
			},
		},
		{
			name: "partial config should fill missing defaults",
			config: PublisherConfig{
				BatchSize: 20,
			},
			expected: PublisherConfig{
				BatchSize:    20,
				BatchTimeout: 100 * time.Millisecond,
				SendTimeout:  30 * time.Second,
				CloseTimeout: 30 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config
			config.setDefaults()

			if config.BatchSize != tt.expected.BatchSize {
				t.Errorf("BatchSize = %d, want %d", config.BatchSize, tt.expected.BatchSize)
			}
			if config.BatchTimeout != tt.expected.BatchTimeout {
				t.Errorf("BatchTimeout = %v, want %v", config.BatchTimeout, tt.expected.BatchTimeout)
			}
			if config.SendTimeout != tt.expected.SendTimeout {
				t.Errorf("SendTimeout = %v, want %v", config.SendTimeout, tt.expected.SendTimeout)
			}
			if config.CloseTimeout != tt.expected.CloseTimeout {
				t.Errorf("CloseTimeout = %v, want %v", config.CloseTimeout, tt.expected.CloseTimeout)
			}
		})
	}
}

func TestMultiPublisherConfig_setDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   MultiPublisherConfig
		expected MultiPublisherConfig
	}{
		{
			name:   "empty config should use defaults",
			config: MultiPublisherConfig{},
			expected: MultiPublisherConfig{
				PublisherConfig: PublisherConfig{
					BatchSize:    10,
					BatchTimeout: 100 * time.Millisecond,
					SendTimeout:  30 * time.Second,
					CloseTimeout: 30 * time.Second,
				},
				Concurrency: 1,
			},
		},
		{
			name: "custom concurrency should be preserved",
			config: MultiPublisherConfig{
				Concurrency: 5,
			},
			expected: MultiPublisherConfig{
				PublisherConfig: PublisherConfig{
					BatchSize:    10,
					BatchTimeout: 100 * time.Millisecond,
					SendTimeout:  30 * time.Second,
					CloseTimeout: 30 * time.Second,
				},
				Concurrency: 5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config
			config.setDefaults()

			if config.Concurrency != tt.expected.Concurrency {
				t.Errorf("Concurrency = %d, want %d", config.Concurrency, tt.expected.Concurrency)
			}
			if config.BatchSize != tt.expected.BatchSize {
				t.Errorf("BatchSize = %d, want %d", config.BatchSize, tt.expected.BatchSize)
			}
		})
	}
}
