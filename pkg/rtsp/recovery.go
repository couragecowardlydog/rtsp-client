package rtsp

import (
	"fmt"
	"sync"
	"time"
)

// RetryConfig holds configuration for retry/recovery logic
type RetryConfig struct {
	MaxRetries   int           // Maximum number of retry attempts
	InitialDelay time.Duration // Initial delay before first retry
	MaxDelay     time.Duration // Maximum delay between retries
	Multiplier   float64       // Backoff multiplier (default: 2.0)
}

// RecoveryMetrics tracks recovery statistics
type RecoveryMetrics struct {
	mu                   sync.RWMutex
	TotalRetries         int
	SuccessfulRecoveries int
	FailedRecoveries     int
	LastRecoveryAttempt  time.Time
	LastRecoverySuccess  time.Time
}

// NewRetryConfig creates a new retry configuration
func NewRetryConfig(maxRetries int, initialDelay, maxDelay time.Duration) *RetryConfig {
	// Validate and set defaults
	if maxRetries <= 0 {
		maxRetries = 3 // Default to 3 retries
	}
	if initialDelay <= 0 {
		initialDelay = 100 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = 30 * time.Second
	}

	return &RetryConfig{
		MaxRetries:   maxRetries,
		InitialDelay: initialDelay,
		MaxDelay:     maxDelay,
		Multiplier:   2.0, // Exponential backoff with factor of 2
	}
}

// DefaultRetryConfig returns a default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return NewRetryConfig(3, 100*time.Millisecond, 30*time.Second)
}

// SetRetryConfig sets the retry configuration for the client
func (c *Client) SetRetryConfig(config *RetryConfig) {
	c.retryConfig = config
}

// GetRetryConfig returns the current retry configuration
func (c *Client) GetRetryConfig() *RetryConfig {
	if c.retryConfig == nil {
		return DefaultRetryConfig()
	}
	return c.retryConfig
}

// IsConnected checks if the client is currently connected
func (c *Client) IsConnected() bool {
	return c.conn != nil
}

// HealthCheck performs a health check on the connection
func (c *Client) HealthCheck() bool {
	if !c.IsConnected() {
		return false
	}

	// Try to send OPTIONS request as ping
	c.cseq++
	request := buildRequest("OPTIONS", c.url, c.cseq, c.session)

	err := c.sendRequest(request)
	if err != nil {
		return false
	}

	// Try to read response
	statusCode, _, _, err := c.readResponse()
	if err != nil {
		return false
	}

	return statusCode == 200
}

// ConnectWithRetry attempts to connect with retry logic
func (c *Client) ConnectWithRetry() error {
	config := c.GetRetryConfig()

	return retryWithBackoff(config, func() error {
		return c.Connect()
	})
}

// Reconnect attempts to reconnect to the server
func (c *Client) Reconnect() error {
	// Close existing connection if any
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	// Try to reconnect with retry logic
	return c.ConnectWithRetry()
}

// GetRecoveryMetrics returns current recovery metrics
func (c *Client) GetRecoveryMetrics() *RecoveryMetrics {
	if c.recoveryMetrics == nil {
		c.recoveryMetrics = &RecoveryMetrics{}
	}
	return c.recoveryMetrics
}

// recordRetryAttempt records a retry attempt in metrics
func (c *Client) recordRetryAttempt(success bool) {
	metrics := c.GetRecoveryMetrics()
	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	metrics.TotalRetries++
	metrics.LastRecoveryAttempt = time.Now()

	if success {
		metrics.SuccessfulRecoveries++
		metrics.LastRecoverySuccess = time.Now()
	} else {
		metrics.FailedRecoveries++
	}
}

// retryWithBackoff executes a function with exponential backoff retry logic
func retryWithBackoff(config *RetryConfig, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt < config.MaxRetries; attempt++ {
		// Execute the function
		err := fn()
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// If this is the last attempt, don't wait
		if attempt == config.MaxRetries-1 {
			break
		}

		// Calculate backoff delay
		delay := calculateBackoff(attempt, config.InitialDelay, config.MaxDelay)

		// Wait before next retry
		time.Sleep(delay)
	}

	return fmt.Errorf("max retries exceeded after %d attempts: %w", config.MaxRetries, lastErr)
}

// calculateBackoff calculates the backoff delay for a given attempt
func calculateBackoff(attempt int, initialDelay, maxDelay time.Duration) time.Duration {
	// Exponential backoff: initialDelay * 2^attempt
	delay := initialDelay * time.Duration(1<<uint(attempt))

	// Cap at max delay
	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

// RecoverSession attempts to recover an existing session after connection loss
func (c *Client) RecoverSession() error {
	if c.session == "" {
		return fmt.Errorf("no session to recover")
	}

	// Reconnect to server
	if err := c.Reconnect(); err != nil {
		c.recordRetryAttempt(false)
		return fmt.Errorf("failed to reconnect: %w", err)
	}

	// Try to resume PLAY with existing session
	c.cseq++
	request := c.buildRequestWithAuth("PLAY", c.url, c.cseq, c.session)

	if err := c.sendRequest(request); err != nil {
		c.recordRetryAttempt(false)
		return fmt.Errorf("failed to send PLAY: %w", err)
	}

	statusCode, _, _, err := c.readResponse()
	if err != nil {
		c.recordRetryAttempt(false)
		return fmt.Errorf("failed to read PLAY response: %w", err)
	}

	if statusCode == 454 {
		// Session Not Found - need to re-establish
		c.recordRetryAttempt(false)
		return fmt.Errorf("session expired, need to re-establish")
	}

	if statusCode != 200 {
		c.recordRetryAttempt(false)
		return fmt.Errorf("PLAY failed with status %d", statusCode)
	}

	c.recordRetryAttempt(true)
	return nil
}

// AutoReconnect starts an automatic reconnection loop
func (c *Client) AutoReconnect(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if !c.HealthCheck() {
					// Connection lost, attempt recovery
					if err := c.RecoverSession(); err != nil {
						// Log error (in production, use proper logger)
						fmt.Printf("Auto-reconnect failed: %v\n", err)
					}
				}
			case <-c.ctx.Done():
				return
			}
		}
	}()
}
