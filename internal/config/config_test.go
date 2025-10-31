package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		expected    *Config
	}{
		{
			name: "valid configuration",
			config: &Config{
				RTSPURL:   "rtsp://example.com/stream",
				OutputDir: "./frames",
				Timeout:   10 * time.Second,
			},
			expectError: false,
			expected: &Config{
				RTSPURL:   "rtsp://example.com/stream",
				OutputDir: "./frames",
				Timeout:   10 * time.Second,
			},
		},
		{
			name: "empty URL",
			config: &Config{
				RTSPURL:   "",
				OutputDir: "./frames",
				Timeout:   10 * time.Second,
			},
			expectError: true,
		},
		{
			name: "zero timeout gets default",
			config: &Config{
				RTSPURL:   "rtsp://example.com/stream",
				OutputDir: "./frames",
				Timeout:   0,
			},
			expectError: false,
			expected: &Config{
				RTSPURL:   "rtsp://example.com/stream",
				OutputDir: "./frames",
				Timeout:   10 * time.Second,
			},
		},
		{
			name: "empty output dir gets default",
			config: &Config{
				RTSPURL:   "rtsp://example.com/stream",
				OutputDir: "",
				Timeout:   5 * time.Second,
			},
			expectError: false,
			expected: &Config{
				RTSPURL:   "rtsp://example.com/stream",
				OutputDir: "./frames",
				Timeout:   5 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected.RTSPURL, tt.config.RTSPURL)
				assert.Equal(t, tt.expected.OutputDir, tt.config.OutputDir)
				assert.Equal(t, tt.expected.Timeout, tt.config.Timeout)
			}
		})
	}
}

func TestConfig_String(t *testing.T) {
	config := &Config{
		RTSPURL:   "rtsp://example.com/stream",
		OutputDir: "./frames",
		Timeout:   10 * time.Second,
		Verbose:   true,
		SaveJPEG:  true,
	}

	result := config.String()
	assert.Contains(t, result, "rtsp://example.com/stream")
	assert.Contains(t, result, "./frames")
	assert.Contains(t, result, "10s")
	assert.Contains(t, result, "true")
	assert.Contains(t, result, "Save JPEG")
}
