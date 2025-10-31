package rtsp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseURLWithCredentials tests parsing URLs with embedded credentials
func TestParseURLWithCredentials(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expectError bool
		username    string
		password    string
		host        string
		port        string
	}{
		{
			name:        "URL with username and password",
			url:         "rtsp://admin:password123@192.168.1.100:554/stream",
			expectError: false,
			username:    "admin",
			password:    "password123",
			host:        "192.168.1.100",
			port:        "554",
		},
		{
			name:        "URL with special characters in password",
			url:         "rtsp://user:p@ss:w0rd!@example.com/live",
			expectError: false,
			username:    "user",
			password:    "p@ss:w0rd!",
			host:        "example.com",
			port:        "554",
		},
		{
			name:        "URL with username only (no password)",
			url:         "rtsp://admin@192.168.1.100/stream",
			expectError: false,
			username:    "admin",
			password:    "",
			host:        "192.168.1.100",
			port:        "554",
		},
		{
			name:        "URL without credentials",
			url:         "rtsp://192.168.1.100/stream",
			expectError: false,
			username:    "",
			password:    "",
			host:        "192.168.1.100",
			port:        "554",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, username, password, err := parseURLWithAuth(tt.url)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.host, host)
				assert.Equal(t, tt.port, port)
				assert.Equal(t, tt.username, username)
				assert.Equal(t, tt.password, password)
			}
		})
	}
}

// TestBasicAuthHeader tests Basic Authentication header generation
func TestBasicAuthHeader(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		expected string
	}{
		{
			name:     "simple credentials",
			username: "admin",
			password: "password",
			expected: "Basic YWRtaW46cGFzc3dvcmQ=", // base64 of "admin:password"
		},
		{
			name:     "empty password",
			username: "user",
			password: "",
			expected: "Basic dXNlcjo=", // base64 of "user:"
		},
		{
			name:     "special characters",
			username: "test@user",
			password: "p@ss:123!",
			expected: "Basic dGVzdEB1c2VyOnBAc3M6MTIzIQ==",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateBasicAuthHeader(tt.username, tt.password)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParse401Response tests parsing 401 Unauthorized responses
func TestParse401Response(t *testing.T) {
	tests := []struct {
		name             string
		response         string
		expectError      bool
		expectedAuthType string
		expectedRealm    string
		expectedNonce    string
	}{
		{
			name: "Digest authentication",
			response: `RTSP/1.0 401 Unauthorized
CSeq: 1
WWW-Authenticate: Digest realm="RTSP Server", nonce="a1b2c3d4e5f6", stale=FALSE

`,
			expectError:      false,
			expectedAuthType: "Digest",
			expectedRealm:    "RTSP Server",
			expectedNonce:    "a1b2c3d4e5f6",
		},
		{
			name: "Basic authentication",
			response: `RTSP/1.0 401 Unauthorized
CSeq: 1
WWW-Authenticate: Basic realm="Camera"

`,
			expectError:      false,
			expectedAuthType: "Basic",
			expectedRealm:    "Camera",
			expectedNonce:    "",
		},
		{
			name: "Digest with multiple parameters",
			response: `RTSP/1.0 401 Unauthorized
CSeq: 2
WWW-Authenticate: Digest realm="IP Camera", nonce="abc123xyz", algorithm=MD5, qop="auth"

`,
			expectError:      false,
			expectedAuthType: "Digest",
			expectedRealm:    "IP Camera",
			expectedNonce:    "abc123xyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authInfo, err := parseAuthChallenge(tt.response)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedAuthType, authInfo.AuthType)
				assert.Equal(t, tt.expectedRealm, authInfo.Realm)
				if tt.expectedNonce != "" {
					assert.Equal(t, tt.expectedNonce, authInfo.Nonce)
				}
			}
		})
	}
}

// TestDigestAuthResponse tests Digest authentication response generation
func TestDigestAuthResponse(t *testing.T) {
	tests := []struct {
		name        string
		username    string
		password    string
		realm       string
		nonce       string
		uri         string
		method      string
		expectError bool
	}{
		{
			name:        "valid digest computation",
			username:    "admin",
			password:    "password",
			realm:       "RTSP Server",
			nonce:       "a1b2c3d4",
			uri:         "rtsp://192.168.1.100/stream",
			method:      "DESCRIBE",
			expectError: false,
		},
		{
			name:        "with special characters",
			username:    "test@user",
			password:    "p@ss:123",
			realm:       "Camera Realm",
			nonce:       "xyz789",
			uri:         "rtsp://example.com/live",
			method:      "SETUP",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := generateDigestResponse(
				tt.username,
				tt.password,
				tt.realm,
				tt.nonce,
				tt.uri,
				tt.method,
			)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, response)
				// Response should be a 32-character MD5 hex string
				assert.Len(t, response, 32)
				assert.Regexp(t, "^[a-f0-9]{32}$", response)
			}
		})
	}
}

// TestDigestAuthHeader tests full Digest authentication header generation
func TestDigestAuthHeader(t *testing.T) {
	authInfo := &AuthChallenge{
		AuthType:  "Digest",
		Realm:     "RTSP Server",
		Nonce:     "abc123",
		Opaque:    "xyz789",
		Algorithm: "MD5",
	}

	username := "admin"
	password := "password"
	uri := "rtsp://192.168.1.100/stream"
	method := "DESCRIBE"

	header, err := generateDigestAuthHeader(authInfo, username, password, uri, method)
	require.NoError(t, err)
	assert.NotEmpty(t, header)

	// Header should contain required fields
	assert.Contains(t, header, "Digest")
	assert.Contains(t, header, `username="admin"`)
	assert.Contains(t, header, `realm="RTSP Server"`)
	assert.Contains(t, header, `nonce="abc123"`)
	assert.Contains(t, header, `uri="rtsp://192.168.1.100/stream"`)
	assert.Contains(t, header, `response=`)
}

func TestDigestAuthHeaderWithQOP(t *testing.T) {
	authInfo := &AuthChallenge{
		AuthType:  "Digest",
		Realm:     "testrealm@host.com",
		Nonce:     "dcd98b7102dd2f0e8b11d0f600bfb0c093",
		Algorithm: "MD5",
		Qop:       "auth",
	}

	username := "Mufasa"
	password := "Circle Of Life"
	uri := "/dir/index.html"
	method := "GET"

	header, err := generateDigestAuthHeader(authInfo, username, password, uri, method)
	require.NoError(t, err)
	assert.NotEmpty(t, header)

	params := parseDigestHeader(header)

	require.Contains(t, params, "cnonce", "Digest header must include cnonce when qop is present")
	require.Contains(t, params, "nc", "Digest header must include nonce count when qop is present")
	assert.Equal(t, "00000001", params["nc"], "First nonce count must be initialised to 1")

	require.Contains(t, params, "qop")
	assert.Equal(t, "auth", params["qop"], "qop must match challenge")

	expected := computeDigestResponseWithQOP(username, password, authInfo.Realm, authInfo.Nonce, uri, method, params["cnonce"], params["nc"], params["qop"])
	assert.Equal(t, expected, params["response"], "Digest response must include qop components")
}

func parseDigestHeader(header string) map[string]string {
	result := make(map[string]string)

	header = strings.TrimSpace(header)
	if strings.HasPrefix(header, "Digest ") {
		header = header[len("Digest "):]
	}

	parts := strings.Split(header, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}

		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		value = strings.Trim(value, "\"")
		result[key] = value
	}

	return result
}

func computeDigestResponseWithQOP(username, password, realm, nonce, uri, method, cnonce, nc, qop string) string {
	ha1 := md5Hash(username + ":" + realm + ":" + password)
	ha2 := md5Hash(method + ":" + uri)

	responseInput := strings.Join([]string{ha1, nonce, nc, cnonce, qop, ha2}, ":")
	return md5Hash(responseInput)
}

// TestAuthChallenge tests the AuthChallenge struct
func TestAuthChallenge(t *testing.T) {
	challenge := &AuthChallenge{
		AuthType:  "Digest",
		Realm:     "Test Realm",
		Nonce:     "test-nonce",
		Opaque:    "test-opaque",
		Algorithm: "MD5",
		Qop:       "auth",
	}

	assert.Equal(t, "Digest", challenge.AuthType)
	assert.Equal(t, "Test Realm", challenge.Realm)
	assert.Equal(t, "test-nonce", challenge.Nonce)
	assert.Equal(t, "MD5", challenge.Algorithm)
}

// TestClient_WithAuthentication tests client methods with authentication
func TestClient_WithAuthentication(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expectError bool
	}{
		{
			name:        "client with embedded credentials",
			url:         "rtsp://admin:password@192.168.1.100/stream",
			expectError: false,
		},
		{
			name:        "client without credentials",
			url:         "rtsp://192.168.1.100/stream",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.url, 0)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, client)
				// Client should have parsed credentials
				if tt.url == "rtsp://admin:password@192.168.1.100/stream" {
					assert.Equal(t, "admin", client.username)
					assert.Equal(t, "password", client.password)
				}
			}
		})
	}
}

// TestClient_SetCredentials tests setting credentials after client creation
func TestClient_SetCredentials(t *testing.T) {
	client, err := NewClient("rtsp://192.168.1.100/stream", 0)
	require.NoError(t, err)

	client.SetCredentials("testuser", "testpass")

	assert.Equal(t, "testuser", client.username)
	assert.Equal(t, "testpass", client.password)
}

// TestRetryWith401 tests automatic retry after 401 response
func TestRetryWith401(t *testing.T) {
	// This test will validate that when a 401 is received,
	// the client automatically retries with authentication
	t.Skip("Integration test - requires mock server")
}
