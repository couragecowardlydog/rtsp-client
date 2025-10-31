package rtsp

import (
	"fmt"
)

// RTSPError represents an RTSP protocol error
type RTSPError struct {
	StatusCode int
	Message    string
	Method     string // RTSP method that caused the error
	URL        string // URL of the request
}

// Error implements the error interface
func (e *RTSPError) Error() string {
	if e.Method != "" && e.URL != "" {
		return fmt.Sprintf("RTSP %s %s failed: %d %s", e.Method, e.URL, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("RTSP error %d: %s", e.StatusCode, e.Message)
}

// NewRTSPError creates a new RTSP error
func NewRTSPError(statusCode int, message string) *RTSPError {
	return &RTSPError{
		StatusCode: statusCode,
		Message:    message,
	}
}

// NewRTSPErrorWithContext creates a new RTSP error with method and URL context
func NewRTSPErrorWithContext(statusCode int, method, url string) *RTSPError {
	return &RTSPError{
		StatusCode: statusCode,
		Message:    GetErrorMessage(statusCode),
		Method:     method,
		URL:        url,
	}
}

// GetErrorMessage returns a human-readable message for an RTSP status code
func GetErrorMessage(statusCode int) string {
	messages := map[int]string{
		// 1xx Informational
		100: "Continue",

		// 2xx Success
		200: "OK",

		// 3xx Redirection
		301: "Moved Permanently",
		302: "Moved Temporarily",
		303: "See Other",
		304: "Not Modified",
		305: "Use Proxy",

		// 4xx Client Error
		400: "Bad Request",
		401: "Unauthorized",
		402: "Payment Required",
		403: "Forbidden",
		404: "Not Found",
		405: "Method Not Allowed",
		406: "Not Acceptable",
		407: "Proxy Authentication Required",
		408: "Request Timeout",
		410: "Gone",
		411: "Length Required",
		412: "Precondition Failed",
		413: "Request Entity Too Large",
		414: "Request-URI Too Long",
		415: "Unsupported Media Type",
		451: "Parameter Not Understood",
		452: "Conference Not Found",
		453: "Not Enough Bandwidth",
		454: "Session Not Found",
		455: "Method Not Valid in This State",
		456: "Header Field Not Valid for Resource",
		457: "Invalid Range",
		458: "Parameter Is Read-Only",
		459: "Aggregate Operation Not Allowed",
		460: "Only Aggregate Operation Allowed",
		461: "Unsupported Transport",
		462: "Destination Unreachable",

		// 5xx Server Error
		500: "Internal Server Error",
		501: "Not Implemented",
		502: "Bad Gateway",
		503: "Service Unavailable",
		504: "Gateway Timeout",
		505: "RTSP Version Not Supported",
		551: "Option Not Supported",
	}

	if msg, ok := messages[statusCode]; ok {
		return msg
	}
	return fmt.Sprintf("Unknown Error %d", statusCode)
}

// IsRedirect checks if a status code indicates a redirect
func IsRedirect(statusCode int) bool {
	return statusCode >= 300 && statusCode < 400
}

// IsRetryableError checks if an error is retryable
func IsRetryableError(statusCode int) bool {
	// Server errors (5xx) and certain client errors are retryable
	retryable := map[int]bool{
		408: true, // Request Timeout
		500: true, // Internal Server Error
		502: true, // Bad Gateway
		503: true, // Service Unavailable
		504: true, // Gateway Timeout
	}

	return retryable[statusCode]
}

// IsClientError checks if status code is a client error (4xx)
func IsClientError(statusCode int) bool {
	return statusCode >= 400 && statusCode < 500
}

// IsServerError checks if status code is a server error (5xx)
func IsServerError(statusCode int) bool {
	return statusCode >= 500 && statusCode < 600
}

// HandleRedirect updates the client URL for a redirect
func (c *Client) HandleRedirect(newURL string) error {
	if c.redirectCount >= 10 {
		return fmt.Errorf("too many redirects (max 10)")
	}

	// Parse new URL to validate
	host, port, username, password, err := parseURLWithAuth(newURL)
	if err != nil {
		return fmt.Errorf("invalid redirect URL: %w", err)
	}

	// Update client configuration
	c.url = newURL
	c.host = host
	c.port = port
	if username != "" {
		c.username = username
		c.password = password
	}

	c.redirectCount++
	return nil
}

// ResetRedirectCount resets the redirect counter
func (c *Client) ResetRedirectCount() {
	c.redirectCount = 0
}

// HandleErrorResponse handles RTSP error responses intelligently
func (c *Client) HandleErrorResponse(statusCode int, method string, headers map[string]string) error {
	// Handle redirects (3xx)
	if IsRedirect(statusCode) {
		location, ok := headers["Location"]
		if !ok {
			return fmt.Errorf("redirect response without Location header")
		}
		if err := c.HandleRedirect(location); err != nil {
			return err
		}
		return fmt.Errorf("redirect to %s", location)
	}

	// Handle authentication (401)
	if statusCode == 401 {
		// This is handled separately by retry with auth logic
		return NewRTSPErrorWithContext(401, method, c.url)
	}

	// Handle session errors (454)
	if statusCode == 454 {
		// Session expired or not found
		c.session = "" // Clear invalid session
		return NewRTSPErrorWithContext(454, method, c.url)
	}

	// Handle transport errors (461)
	if statusCode == 461 {
		// Unsupported transport - client may want to fallback to TCP
		return NewRTSPErrorWithContext(461, method, c.url)
	}

	// Generic error
	return NewRTSPErrorWithContext(statusCode, method, c.url)
}
