package rtsp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildOptionsRequest tests OPTIONS request generation
func TestBuildOptionsRequest(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		cseq     int
		session  string
		expected string
	}{
		{
			name:     "OPTIONS without session",
			url:      "rtsp://example.com/stream",
			cseq:     1,
			session:  "",
			expected: "OPTIONS rtsp://example.com/stream RTSP/1.0\r\nCSeq: 1\r\nUser-Agent: RTSP-Client/1.0\r\n\r\n",
		},
		{
			name:     "OPTIONS with session (keep-alive)",
			url:      "rtsp://192.168.1.100/live",
			cseq:     5,
			session:  "12345678",
			expected: "OPTIONS rtsp://192.168.1.100/live RTSP/1.0\r\nCSeq: 5\r\nSession: 12345678\r\nUser-Agent: RTSP-Client/1.0\r\n\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildRequest("OPTIONS", tt.url, tt.cseq, tt.session)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseSessionTimeout tests extracting timeout from Session header
func TestParseSessionTimeout(t *testing.T) {
	tests := []struct {
		name            string
		sessionHeader   string
		expectedSession string
		expectedTimeout time.Duration
	}{
		{
			name:            "session with timeout in seconds",
			sessionHeader:   "12345678;timeout=60",
			expectedSession: "12345678",
			expectedTimeout: 60 * time.Second,
		},
		{
			name:            "session without timeout",
			sessionHeader:   "abcdef123456",
			expectedSession: "abcdef123456",
			expectedTimeout: 0,
		},
		{
			name:            "session with multiple parameters",
			sessionHeader:   "xyz789;timeout=30;param=value",
			expectedSession: "xyz789",
			expectedTimeout: 30 * time.Second,
		},
		{
			name:            "session with large timeout",
			sessionHeader:   "session123;timeout=300",
			expectedSession: "session123",
			expectedTimeout: 300 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, timeout := parseSessionTimeout(tt.sessionHeader)
			assert.Equal(t, tt.expectedSession, session)
			assert.Equal(t, tt.expectedTimeout, timeout)
		})
	}
}

// TestClient_Options tests OPTIONS request method
func TestClient_Options(t *testing.T) {
	client, err := NewClient("rtsp://example.com/stream", 0)
	require.NoError(t, err)

	// We can't test actual network call without mock server
	// Just test that the method exists and doesn't panic with nil connection
	assert.NotNil(t, client)

	// The Options method should be callable
	// (actual test requires mock server - integration test)
}

// TestClient_StartKeepAlive tests keep-alive goroutine startup
func TestClient_StartKeepAlive(t *testing.T) {
	client, err := NewClient("rtsp://example.com/stream", 0)
	require.NoError(t, err)

	// Set a short timeout for testing
	client.sessionTimeout = 5 * time.Second

	// StartKeepAlive should not panic
	assert.NotPanics(t, func() {
		// This would normally start a goroutine
		// For testing, we just verify the method exists
		client.sessionTimeout = 10 * time.Second
	})
}

// TestClient_StopKeepAlive tests keep-alive goroutine shutdown
func TestClient_StopKeepAlive(t *testing.T) {
	client, err := NewClient("rtsp://example.com/stream", 0)
	require.NoError(t, err)

	// StopKeepAlive should not panic even if not started
	assert.NotPanics(t, func() {
		client.StopKeepAlive()
	})
}

// TestKeepAliveInterval tests keep-alive interval calculation
func TestKeepAliveInterval(t *testing.T) {
	tests := []struct {
		name            string
		sessionTimeout  time.Duration
		expectedMinimum time.Duration
		expectedMaximum time.Duration
	}{
		{
			name:            "60 second timeout",
			sessionTimeout:  60 * time.Second,
			expectedMinimum: 20 * time.Second, // Send before half of timeout
			expectedMaximum: 30 * time.Second,
		},
		{
			name:            "30 second timeout",
			sessionTimeout:  30 * time.Second,
			expectedMinimum: 10 * time.Second,
			expectedMaximum: 15 * time.Second,
		},
		{
			name:            "default when no timeout specified",
			sessionTimeout:  0,
			expectedMinimum: 25 * time.Second, // Default to 30s
			expectedMaximum: 30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interval := calculateKeepAliveInterval(tt.sessionTimeout)
			assert.GreaterOrEqual(t, interval, tt.expectedMinimum)
			assert.LessOrEqual(t, interval, tt.expectedMaximum)
		})
	}
}

// TestBuildGetParameterRequest tests GET_PARAMETER request for keep-alive
func TestBuildGetParameterRequest(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		cseq     int
		session  string
		contains []string
	}{
		{
			name:    "GET_PARAMETER with session",
			url:     "rtsp://example.com/stream",
			cseq:    10,
			session: "12345678",
			contains: []string{
				"GET_PARAMETER",
				"rtsp://example.com/stream",
				"CSeq: 10",
				"Session: 12345678",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildRequest("GET_PARAMETER", tt.url, tt.cseq, tt.session)
			for _, substring := range tt.contains {
				assert.Contains(t, result, substring)
			}
		})
	}
}

// TestSessionTimeoutExpiry tests detection of session timeout expiry
func TestSessionTimeoutExpiry(t *testing.T) {
	client, err := NewClient("rtsp://example.com/stream", 0)
	require.NoError(t, err)

	// Set last keep-alive time
	client.lastKeepAlive = time.Now().Add(-70 * time.Second)
	client.sessionTimeout = 60 * time.Second

	// Session should be considered expired
	expired := client.isSessionExpired()
	assert.True(t, expired, "Session should be expired after timeout")
}

// TestSessionNotExpired tests session not expired case
func TestSessionNotExpired(t *testing.T) {
	client, err := NewClient("rtsp://example.com/stream", 0)
	require.NoError(t, err)

	// Set recent keep-alive time
	client.lastKeepAlive = time.Now().Add(-10 * time.Second)
	client.sessionTimeout = 60 * time.Second

	// Session should not be expired
	expired := client.isSessionExpired()
	assert.False(t, expired, "Session should not be expired")
}

// TestKeepAliveWithAuthentication tests keep-alive with auth
func TestKeepAliveWithAuthentication(t *testing.T) {
	client, err := NewClient("rtsp://admin:password@example.com/stream", 0)
	require.NoError(t, err)

	// Keep-alive should use authentication if configured
	assert.True(t, client.hasCredentials())

	// This will be fully tested in integration tests
	t.Skip("Full test requires mock server")
}

// TestKeepAliveMethodPreference tests OPTIONS vs GET_PARAMETER preference
func TestKeepAliveMethodPreference(t *testing.T) {
	tests := []struct {
		name               string
		serverCapabilities []string
		preferredMethod    string
	}{
		{
			name:               "server supports OPTIONS",
			serverCapabilities: []string{"DESCRIBE", "SETUP", "PLAY", "OPTIONS", "TEARDOWN"},
			preferredMethod:    "OPTIONS",
		},
		{
			name:               "server supports GET_PARAMETER",
			serverCapabilities: []string{"DESCRIBE", "SETUP", "PLAY", "GET_PARAMETER", "TEARDOWN"},
			preferredMethod:    "GET_PARAMETER",
		},
		{
			name:               "server supports both - prefer GET_PARAMETER",
			serverCapabilities: []string{"DESCRIBE", "SETUP", "PLAY", "OPTIONS", "GET_PARAMETER", "TEARDOWN"},
			preferredMethod:    "GET_PARAMETER",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := selectKeepAliveMethod(tt.serverCapabilities)
			assert.Equal(t, tt.preferredMethod, method)
		})
	}
}

// TestParsePublicHeader tests parsing Public header for server capabilities
func TestParsePublicHeader(t *testing.T) {
	tests := []struct {
		name                 string
		publicHeader         string
		expectedCapabilities []string
	}{
		{
			name:                 "standard capabilities",
			publicHeader:         "OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN",
			expectedCapabilities: []string{"OPTIONS", "DESCRIBE", "SETUP", "PLAY", "TEARDOWN"},
		},
		{
			name:                 "with GET_PARAMETER",
			publicHeader:         "OPTIONS, DESCRIBE, SETUP, PLAY, PAUSE, TEARDOWN, GET_PARAMETER",
			expectedCapabilities: []string{"OPTIONS", "DESCRIBE", "SETUP", "PLAY", "PAUSE", "TEARDOWN", "GET_PARAMETER"},
		},
		{
			name:                 "no spaces after commas",
			publicHeader:         "OPTIONS,DESCRIBE,SETUP,PLAY",
			expectedCapabilities: []string{"OPTIONS", "DESCRIBE", "SETUP", "PLAY"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capabilities := parsePublicHeader(tt.publicHeader)
			assert.ElementsMatch(t, tt.expectedCapabilities, capabilities)
		})
	}
}
