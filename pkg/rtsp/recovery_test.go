package rtsp

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetryConfig tests retry configuration
func TestRetryConfig(t *testing.T) {
	tests := []struct {
		name            string
		maxRetries      int
		initialDelay    time.Duration
		maxDelay        time.Duration
		expectedRetries int
		expectedInitial time.Duration
		expectedMax     time.Duration
	}{
		{
			name:            "standard config",
			maxRetries:      3,
			initialDelay:    100 * time.Millisecond,
			maxDelay:        5 * time.Second,
			expectedRetries: 3,
			expectedInitial: 100 * time.Millisecond,
			expectedMax:     5 * time.Second,
		},
		{
			name:            "aggressive retry",
			maxRetries:      5,
			initialDelay:    50 * time.Millisecond,
			maxDelay:        2 * time.Second,
			expectedRetries: 5,
			expectedInitial: 50 * time.Millisecond,
			expectedMax:     2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := NewRetryConfig(tt.maxRetries, tt.initialDelay, tt.maxDelay)
			require.NotNil(t, config)
			assert.Equal(t, tt.expectedRetries, config.MaxRetries)
			assert.Equal(t, tt.expectedInitial, config.InitialDelay)
			assert.Equal(t, tt.expectedMax, config.MaxDelay)
		})
	}
}

// TestCalculateBackoff tests exponential backoff calculation
func TestCalculateBackoff(t *testing.T) {
	tests := []struct {
		name         string
		attempt      int
		initialDelay time.Duration
		maxDelay     time.Duration
		expectedMin  time.Duration
		expectedMax  time.Duration
	}{
		{
			name:         "first attempt",
			attempt:      0,
			initialDelay: 100 * time.Millisecond,
			maxDelay:     10 * time.Second,
			expectedMin:  100 * time.Millisecond,
			expectedMax:  100 * time.Millisecond,
		},
		{
			name:         "second attempt - double",
			attempt:      1,
			initialDelay: 100 * time.Millisecond,
			maxDelay:     10 * time.Second,
			expectedMin:  200 * time.Millisecond,
			expectedMax:  200 * time.Millisecond,
		},
		{
			name:         "third attempt - quadruple",
			attempt:      2,
			initialDelay: 100 * time.Millisecond,
			maxDelay:     10 * time.Second,
			expectedMin:  400 * time.Millisecond,
			expectedMax:  400 * time.Millisecond,
		},
		{
			name:         "exceeds max delay",
			attempt:      10,
			initialDelay: 100 * time.Millisecond,
			maxDelay:     1 * time.Second,
			expectedMin:  1 * time.Second,
			expectedMax:  1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay := calculateBackoff(tt.attempt, tt.initialDelay, tt.maxDelay)
			assert.GreaterOrEqual(t, delay, tt.expectedMin)
			assert.LessOrEqual(t, delay, tt.expectedMax)
		})
	}
}

// TestRetryWithBackoff tests retry mechanism with backoff
func TestRetryWithBackoff(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		attempts := 0
		fn := func() error {
			attempts++
			return nil // Success
		}

		config := NewRetryConfig(3, 10*time.Millisecond, 100*time.Millisecond)
		err := retryWithBackoff(config, fn)

		assert.NoError(t, err)
		assert.Equal(t, 1, attempts)
	})

	t.Run("succeeds on second attempt", func(t *testing.T) {
		attempts := 0
		fn := func() error {
			attempts++
			if attempts < 2 {
				return errors.New("temporary error")
			}
			return nil
		}

		config := NewRetryConfig(3, 10*time.Millisecond, 100*time.Millisecond)
		err := retryWithBackoff(config, fn)

		assert.NoError(t, err)
		assert.Equal(t, 2, attempts)
	})

	t.Run("fails after max retries", func(t *testing.T) {
		attempts := 0
		fn := func() error {
			attempts++
			return errors.New("persistent error")
		}

		config := NewRetryConfig(3, 10*time.Millisecond, 100*time.Millisecond)
		err := retryWithBackoff(config, fn)

		assert.Error(t, err)
		assert.Equal(t, 3, attempts)
		assert.Contains(t, err.Error(), "max retries exceeded")
	})
}

// TestClient_SetRetryConfig tests setting retry configuration on client
func TestClient_SetRetryConfig(t *testing.T) {
	client, err := NewClient("rtsp://192.168.1.100/stream", 10*time.Second)
	require.NoError(t, err)

	config := NewRetryConfig(5, 200*time.Millisecond, 10*time.Second)
	client.SetRetryConfig(config)

	assert.NotNil(t, client.retryConfig)
	assert.Equal(t, 5, client.retryConfig.MaxRetries)
	assert.Equal(t, 200*time.Millisecond, client.retryConfig.InitialDelay)
}

// TestClient_ConnectWithRetry tests connection with retry logic
func TestClient_ConnectWithRetry(t *testing.T) {
	t.Skip("Integration test - requires controllable network conditions")

	client, err := NewClient("rtsp://192.168.1.100:554/stream", 5*time.Second)
	require.NoError(t, err)

	config := NewRetryConfig(3, 100*time.Millisecond, 2*time.Second)
	client.SetRetryConfig(config)

	// Attempt connection with retry
	err = client.ConnectWithRetry()
	// Result depends on network availability
	t.Logf("Connection result: %v", err)
}

// TestSessionRecovery tests session recovery after connection loss
func TestSessionRecovery(t *testing.T) {
	t.Skip("Integration test - requires mock server")

	// This would test:
	// 1. Establish session
	// 2. Simulate connection drop
	// 3. Attempt to recover with same session ID
	// 4. Verify PLAY can resume
}

// TestClient_IsConnected tests connection state checking
func TestClient_IsConnected(t *testing.T) {
	client, err := NewClient("rtsp://192.168.1.100/stream", 10*time.Second)
	require.NoError(t, err)

	// Initially not connected
	assert.False(t, client.IsConnected())

	// After successful connect (if possible)
	// assert.True(t, client.IsConnected())
}

// TestClient_Reconnect tests reconnection logic
func TestClient_Reconnect(t *testing.T) {
	client, err := NewClient("rtsp://192.168.1.100/stream", 10*time.Second)
	require.NoError(t, err)

	// Set retry config
	config := NewRetryConfig(2, 50*time.Millisecond, 1*time.Second)
	client.SetRetryConfig(config)

	// Attempt reconnect (will fail without real server, but tests structure)
	err = client.Reconnect()
	assert.Error(t, err) // Expected to fail without server
}

// TestExponentialBackoffTiming tests actual timing of backoff
func TestExponentialBackoffTiming(t *testing.T) {
	attempts := 0
	startTime := time.Now()

	fn := func() error {
		attempts++
		if attempts < 3 {
			return errors.New("retry")
		}
		return nil
	}

	config := NewRetryConfig(5, 50*time.Millisecond, 1*time.Second)
	err := retryWithBackoff(config, fn)

	elapsed := time.Since(startTime)

	assert.NoError(t, err)
	assert.Equal(t, 3, attempts)
	// Should have waited: 50ms + 100ms = 150ms minimum
	assert.GreaterOrEqual(t, elapsed, 150*time.Millisecond)
}

// TestRetryConfig_Validation tests validation of retry configuration
func TestRetryConfig_Validation(t *testing.T) {
	tests := []struct {
		name         string
		maxRetries   int
		initialDelay time.Duration
		maxDelay     time.Duration
		shouldAdjust bool
	}{
		{
			name:         "valid config",
			maxRetries:   3,
			initialDelay: 100 * time.Millisecond,
			maxDelay:     5 * time.Second,
			shouldAdjust: false,
		},
		{
			name:         "zero retries adjusted to default",
			maxRetries:   0,
			initialDelay: 100 * time.Millisecond,
			maxDelay:     5 * time.Second,
			shouldAdjust: true,
		},
		{
			name:         "negative retries adjusted to default",
			maxRetries:   -1,
			initialDelay: 100 * time.Millisecond,
			maxDelay:     5 * time.Second,
			shouldAdjust: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := NewRetryConfig(tt.maxRetries, tt.initialDelay, tt.maxDelay)
			require.NotNil(t, config)

			if tt.shouldAdjust {
				// Should have default value (e.g., 3)
				assert.Greater(t, config.MaxRetries, 0)
			} else {
				assert.Equal(t, tt.maxRetries, config.MaxRetries)
			}
		})
	}
}

// TestClient_HealthCheck tests connection health checking
func TestClient_HealthCheck(t *testing.T) {
	client, err := NewClient("rtsp://192.168.1.100/stream", 10*time.Second)
	require.NoError(t, err)

	// Health check on disconnected client
	healthy := client.HealthCheck()
	assert.False(t, healthy)
}

// TestRecoveryMetrics tests collection of recovery metrics
func TestRecoveryMetrics(t *testing.T) {
	client, err := NewClient("rtsp://192.168.1.100/stream", 10*time.Second)
	require.NoError(t, err)

	metrics := client.GetRecoveryMetrics()
	require.NotNil(t, metrics)

	// Initial state
	assert.Equal(t, 0, metrics.TotalRetries)
	assert.Equal(t, 0, metrics.SuccessfulRecoveries)
	assert.Equal(t, 0, metrics.FailedRecoveries)
}
