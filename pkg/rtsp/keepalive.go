package rtsp

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseSessionTimeout extracts session ID and timeout from Session header
// Example: "12345678;timeout=60" returns ("12345678", 60*time.Second)
func parseSessionTimeout(sessionHeader string) (string, time.Duration) {
	parts := strings.Split(sessionHeader, ";")
	session := strings.TrimSpace(parts[0])

	var timeout time.Duration
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "timeout=") {
			timeoutStr := strings.TrimPrefix(part, "timeout=")
			if seconds, err := strconv.Atoi(timeoutStr); err == nil {
				timeout = time.Duration(seconds) * time.Second
			}
		}
	}

	return session, timeout
}

// calculateKeepAliveInterval calculates appropriate interval for keep-alive
// Typically sends keep-alive at half the timeout period, or 30s default
func calculateKeepAliveInterval(sessionTimeout time.Duration) time.Duration {
	if sessionTimeout == 0 {
		// Default to 30 seconds if no timeout specified
		return 30 * time.Second
	}

	// Send keep-alive at half the timeout period (conservative)
	interval := sessionTimeout / 2

	// Minimum 10 seconds, maximum 30 seconds
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}

	return interval
}

// selectKeepAliveMethod selects preferred keep-alive method based on server capabilities
// Preference: GET_PARAMETER > OPTIONS
func selectKeepAliveMethod(capabilities []string) string {
	hasGetParameter := false
	hasOptions := false

	for _, cap := range capabilities {
		cap = strings.TrimSpace(strings.ToUpper(cap))
		if cap == "GET_PARAMETER" {
			hasGetParameter = true
		}
		if cap == "OPTIONS" {
			hasOptions = true
		}
	}

	// Prefer GET_PARAMETER as it's designed for keep-alive
	if hasGetParameter {
		return "GET_PARAMETER"
	}

	// Fallback to OPTIONS
	if hasOptions {
		return "OPTIONS"
	}

	// Default to OPTIONS if neither found
	return "OPTIONS"
}

// parsePublicHeader parses Public header to extract server capabilities
// Example: "OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN"
func parsePublicHeader(publicHeader string) []string {
	if publicHeader == "" {
		return []string{}
	}

	parts := strings.Split(publicHeader, ",")
	capabilities := make([]string, 0, len(parts))

	for _, part := range parts {
		capability := strings.TrimSpace(part)
		if capability != "" {
			capabilities = append(capabilities, strings.ToUpper(capability))
		}
	}

	return capabilities
}

// Options sends OPTIONS request to discover server capabilities or keep-alive
func (c *Client) Options() error {
	c.cseq++
	request := c.buildRequestWithAuth("OPTIONS", c.url, c.cseq, c.session)

	if err := c.sendRequest(request); err != nil {
		return err
	}

	statusCode, headers, _, err := c.readResponse()
	if err != nil {
		return err
	}

	// Handle 401 Unauthorized
	if statusCode == 401 && c.hasCredentials() {
		statusCode, headers, _, err = c.retryRequestWithAuth("OPTIONS", c.url, headers)
		if err != nil {
			return err
		}
	}

	if statusCode != 200 {
		return fmt.Errorf("%w: OPTIONS status code %d", ErrRequestFailed, statusCode)
	}

	// Parse Public header for server capabilities (if present)
	if publicHeader, ok := headers["Public"]; ok {
		c.serverCapabilities = parsePublicHeader(publicHeader)
	}

	// Update last keep-alive time
	c.lastKeepAlive = time.Now()

	return nil
}

// GetParameter sends GET_PARAMETER request (used for keep-alive)
func (c *Client) GetParameter() error {
	c.cseq++
	request := c.buildRequestWithAuth("GET_PARAMETER", c.url, c.cseq, c.session)

	if err := c.sendRequest(request); err != nil {
		return err
	}

	statusCode, headers, _, err := c.readResponse()
	if err != nil {
		return err
	}

	// Handle 401 Unauthorized
	if statusCode == 401 && c.hasCredentials() {
		statusCode, _, _, err = c.retryRequestWithAuth("GET_PARAMETER", c.url, headers)
		if err != nil {
			return err
		}
	}

	if statusCode != 200 && statusCode != 451 {
		// 451 Parameter Not Understood is acceptable for keep-alive
		return fmt.Errorf("%w: GET_PARAMETER status code %d", ErrRequestFailed, statusCode)
	}

	// Update last keep-alive time
	c.lastKeepAlive = time.Now()

	return nil
}

// StartKeepAlive starts keep-alive goroutine to maintain session
func (c *Client) StartKeepAlive() {
	if c.keepAliveStop != nil {
		return // Already started
	}

	c.keepAliveStop = make(chan struct{})
	interval := calculateKeepAliveInterval(c.sessionTimeout)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Send keep-alive using preferred method
				method := selectKeepAliveMethod(c.serverCapabilities)

				var err error
				if method == "GET_PARAMETER" {
					err = c.GetParameter()
				} else {
					err = c.Options()
				}

				if err != nil {
					// Log error but continue trying
					// In production, you might want to trigger reconnect here
					continue
				}

			case <-c.keepAliveStop:
				return

			case <-c.ctx.Done():
				return
			}
		}
	}()
}

// StopKeepAlive stops the keep-alive goroutine
func (c *Client) StopKeepAlive() {
	if c.keepAliveStop != nil {
		close(c.keepAliveStop)
		c.keepAliveStop = nil
	}
}

// IsKeepAliveRunning reports whether the keep-alive goroutine is active.
func (c *Client) IsKeepAliveRunning() bool {
	return c.keepAliveStop != nil
}

// isSessionExpired checks if session has expired based on timeout
func (c *Client) isSessionExpired() bool {
	if c.sessionTimeout == 0 {
		return false // No timeout configured
	}

	if c.lastKeepAlive.IsZero() {
		return false // No keep-alive sent yet
	}

	elapsed := time.Since(c.lastKeepAlive)
	return elapsed >= c.sessionTimeout
}
