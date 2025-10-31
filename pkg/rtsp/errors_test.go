package rtsp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRTSPErrorCodes tests handling of various RTSP error codes
func TestRTSPErrorCodes(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		expectedError string
	}{
		{
			name:          "400 Bad Request",
			statusCode:    400,
			expectedError: "bad request",
		},
		{
			name:          "401 Unauthorized",
			statusCode:    401,
			expectedError: "unauthorized",
		},
		{
			name:          "404 Not Found",
			statusCode:    404,
			expectedError: "not found",
		},
		{
			name:          "454 Session Not Found",
			statusCode:    454,
			expectedError: "session not found",
		},
		{
			name:          "461 Unsupported Transport",
			statusCode:    461,
			expectedError: "unsupported transport",
		},
		{
			name:          "500 Internal Server Error",
			statusCode:    500,
			expectedError: "internal server error",
		},
		{
			name:          "503 Service Unavailable",
			statusCode:    503,
			expectedError: "service unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewRTSPError(tt.statusCode, tt.name)
			require.NotNil(t, err)
			errMsg := err.Error()
			assert.Contains(t, errMsg, tt.name) // Contains the provided message
		})
	}
}

// TestParse3xxRedirect tests handling of 3xx redirect responses
func TestParse3xxRedirect(t *testing.T) {
	tests := []struct {
		name           string
		response       string
		expectedCode   int
		expectedURL    string
		expectRedirect bool
	}{
		{
			name: "301 Moved Permanently",
			response: "RTSP/1.0 301 Moved Permanently\r\n" +
				"CSeq: 1\r\n" +
				"Location: rtsp://new-server.example.com/stream\r\n\r\n",
			expectedCode:   301,
			expectedURL:    "rtsp://new-server.example.com/stream",
			expectRedirect: true,
		},
		{
			name: "302 Moved Temporarily",
			response: "RTSP/1.0 302 Moved Temporarily\r\n" +
				"CSeq: 2\r\n" +
				"Location: rtsp://backup.example.com/live\r\n\r\n",
			expectedCode:   302,
			expectedURL:    "rtsp://backup.example.com/live",
			expectRedirect: true,
		},
		{
			name: "303 See Other",
			response: "RTSP/1.0 303 See Other\r\n" +
				"CSeq: 3\r\n" +
				"Location: rtsp://alternate.example.com/video\r\n\r\n",
			expectedCode:   303,
			expectedURL:    "rtsp://alternate.example.com/video",
			expectRedirect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statusCode, headers, _, err := parseResponse(tt.response)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedCode, statusCode)

			if tt.expectRedirect {
				location, exists := headers["Location"]
				t.Logf("Headers: %+v", headers)
				require.True(t, exists, "Location header not found")
				assert.Equal(t, tt.expectedURL, location)
				assert.True(t, IsRedirect(statusCode))
			}
		})
	}
}

// TestHandleRedirect tests automatic redirect following
func TestHandleRedirect(t *testing.T) {
	client, err := NewClient("rtsp://192.168.1.100/stream", 0)
	require.NoError(t, err)

	// Test redirect URL update
	newURL := "rtsp://new-server.example.com/stream"
	err = client.HandleRedirect(newURL)
	require.NoError(t, err)

	assert.Equal(t, newURL, client.url)
}

// TestMaxRedirects tests redirect loop prevention
func TestMaxRedirects(t *testing.T) {
	client, err := NewClient("rtsp://192.168.1.100/stream", 0)
	require.NoError(t, err)

	// Try to follow too many redirects
	for i := 0; i < 15; i++ {
		newURL := "rtsp://server" + string(rune(i)) + ".example.com/stream"
		err = client.HandleRedirect(newURL)
		if i >= 10 {
			// Should fail after max redirects
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "redirect")
			break
		}
	}
}

// TestIsRetryableError tests detection of retryable errors
func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		shouldRetry bool
	}{
		{
			name:        "500 is retryable",
			statusCode:  500,
			shouldRetry: true,
		},
		{
			name:        "503 is retryable",
			statusCode:  503,
			shouldRetry: true,
		},
		{
			name:        "408 is retryable",
			statusCode:  408,
			shouldRetry: true,
		},
		{
			name:        "400 is not retryable",
			statusCode:  400,
			shouldRetry: false,
		},
		{
			name:        "404 is not retryable",
			statusCode:  404,
			shouldRetry: false,
		},
		{
			name:        "401 is not retryable (needs auth)",
			statusCode:  401,
			shouldRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryableError(tt.statusCode)
			assert.Equal(t, tt.shouldRetry, result)
		})
	}
}

// TestGetErrorMessage tests getting human-readable error messages
func TestGetErrorMessage(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		expectedSubstr string
	}{
		{
			name:           "400",
			statusCode:     400,
			expectedSubstr: "Bad Request",
		},
		{
			name:           "454",
			statusCode:     454,
			expectedSubstr: "Session Not Found",
		},
		{
			name:           "461",
			statusCode:     461,
			expectedSubstr: "Unsupported Transport",
		},
		{
			name:           "500",
			statusCode:     500,
			expectedSubstr: "Internal Server Error",
		},
		{
			name:           "Unknown",
			statusCode:     999,
			expectedSubstr: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := GetErrorMessage(tt.statusCode)
			assert.Contains(t, msg, tt.expectedSubstr)
		})
	}
}

// TestRTSPError_Type tests RTSPError type checking
func TestRTSPError_Type(t *testing.T) {
	rtspErr := NewRTSPError(454, "Session Not Found")
	require.NotNil(t, rtspErr)

	// Verify it implements error interface
	var err error = rtspErr
	require.Error(t, err)

	assert.Equal(t, 454, rtspErr.StatusCode)
	assert.Equal(t, "Session Not Found", rtspErr.Message)
}

// TestClient_HandleErrorResponse tests client error response handling
func TestClient_HandleErrorResponse(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		shouldRetry    bool
		shouldRedirect bool
	}{
		{
			name:           "500 should trigger retry",
			statusCode:     500,
			shouldRetry:    true,
			shouldRedirect: false,
		},
		{
			name:           "301 should trigger redirect",
			statusCode:     301,
			shouldRetry:    false,
			shouldRedirect: true,
		},
		{
			name:           "400 should not retry",
			statusCode:     400,
			shouldRetry:    false,
			shouldRedirect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.shouldRetry, IsRetryableError(tt.statusCode))
			assert.Equal(t, tt.shouldRedirect, IsRedirect(tt.statusCode))
		})
	}
}

// TestEnhancedErrorMessages tests enhanced error message generation
func TestEnhancedErrorMessages(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		method     string
		url        string
	}{
		{
			name:       "DESCRIBE 404",
			statusCode: 404,
			method:     "DESCRIBE",
			url:        "rtsp://example.com/notfound",
		},
		{
			name:       "SETUP 461",
			statusCode: 461,
			method:     "SETUP",
			url:        "rtsp://example.com/stream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewRTSPErrorWithContext(tt.statusCode, tt.method, tt.url)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.method)
			assert.Contains(t, err.Error(), GetErrorMessage(tt.statusCode))
		})
	}
}
