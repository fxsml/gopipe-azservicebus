package azservicebus

import (
	"testing"
	"time"
)

func TestClientConfig_setDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   ClientConfig
		expected ClientConfig
	}{
		{
			name:   "empty config should use defaults",
			config: ClientConfig{},
			expected: ClientConfig{
				MaxRetries:    5,
				RetryDelay:    time.Second,
				MaxRetryDelay: 60 * time.Second,
			},
		},
		{
			name: "custom values should be preserved",
			config: ClientConfig{
				MaxRetries:    10,
				RetryDelay:    2 * time.Second,
				MaxRetryDelay: 120 * time.Second,
			},
			expected: ClientConfig{
				MaxRetries:    10,
				RetryDelay:    2 * time.Second,
				MaxRetryDelay: 120 * time.Second,
			},
		},
		{
			name: "partial config should fill missing defaults",
			config: ClientConfig{
				MaxRetries: 3,
			},
			expected: ClientConfig{
				MaxRetries:    3,
				RetryDelay:    time.Second,
				MaxRetryDelay: 60 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.setDefaults()

			if tt.config.MaxRetries != tt.expected.MaxRetries {
				t.Errorf("MaxRetries = %v, want %v", tt.config.MaxRetries, tt.expected.MaxRetries)
			}
			if tt.config.RetryDelay != tt.expected.RetryDelay {
				t.Errorf("RetryDelay = %v, want %v", tt.config.RetryDelay, tt.expected.RetryDelay)
			}
			if tt.config.MaxRetryDelay != tt.expected.MaxRetryDelay {
				t.Errorf("MaxRetryDelay = %v, want %v", tt.config.MaxRetryDelay, tt.expected.MaxRetryDelay)
			}
		})
	}
}

func TestClientConfig_toAzureOptions(t *testing.T) {
	config := ClientConfig{
		MaxRetries:    3,
		RetryDelay:    2 * time.Second,
		MaxRetryDelay: 30 * time.Second,
	}

	opts := config.toAzureOptions()

	if opts == nil {
		t.Fatal("toAzureOptions returned nil")
	}

	if opts.RetryOptions.MaxRetries != 3 {
		t.Errorf("RetryOptions.MaxRetries = %v, want 3", opts.RetryOptions.MaxRetries)
	}
	if opts.RetryOptions.RetryDelay != 2*time.Second {
		t.Errorf("RetryOptions.RetryDelay = %v, want 2s", opts.RetryOptions.RetryDelay)
	}
	if opts.RetryOptions.MaxRetryDelay != 30*time.Second {
		t.Errorf("RetryOptions.MaxRetryDelay = %v, want 30s", opts.RetryOptions.MaxRetryDelay)
	}
}

func TestClientConfig_toAzureOptions_AppliesDefaults(t *testing.T) {
	config := ClientConfig{} // Empty config

	opts := config.toAzureOptions()

	if opts == nil {
		t.Fatal("toAzureOptions returned nil")
	}

	// Verify defaults were applied
	if opts.RetryOptions.MaxRetries != 5 {
		t.Errorf("RetryOptions.MaxRetries = %v, want 5 (default)", opts.RetryOptions.MaxRetries)
	}
	if opts.RetryOptions.RetryDelay != time.Second {
		t.Errorf("RetryOptions.RetryDelay = %v, want 1s (default)", opts.RetryOptions.RetryDelay)
	}
	if opts.RetryOptions.MaxRetryDelay != 60*time.Second {
		t.Errorf("RetryOptions.MaxRetryDelay = %v, want 60s (default)", opts.RetryOptions.MaxRetryDelay)
	}
}
