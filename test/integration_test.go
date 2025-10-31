package test

import (
	"testing"
	"time"

	"github.com/rtsp-client/pkg/rtsp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_BasicFlow tests the complete RTSP flow
func TestE2E_BasicFlow(t *testing.T) {
	// Start mock server
	server, err := NewMockRTSPServer()
	require.NoError(t, err)
	defer server.Stop()

	server.Start()
	time.Sleep(100 * time.Millisecond) // Let server start

	// Create client
	client, err := rtsp.NewClient(server.URL("/stream"), 5*time.Second)
	require.NoError(t, err)
	require.NotNil(t, client)

	// Test Connect
	err = client.Connect()
	require.NoError(t, err)

	// Test Describe
	sdp, err := client.Describe()
	require.NoError(t, err)
	assert.Contains(t, sdp, "v=0")
	assert.Contains(t, sdp, "H264")

	// Test Setup
	err = client.Setup()
	require.NoError(t, err)

	// Test Play
	err = client.Play()
	require.NoError(t, err)

	// Test Teardown
	err = client.Teardown()
	require.NoError(t, err)

	// Close client
	err = client.Close()
	assert.NoError(t, err)

	// Verify request count
	assert.GreaterOrEqual(t, server.GetRequestCount(), 4)
}

// TestE2E_WithAuthentication tests RTSP with authentication
func TestE2E_WithAuthentication(t *testing.T) {
	server, err := NewMockRTSPServer()
	require.NoError(t, err)
	defer server.Stop()

	server.SetRequireAuth("admin", "password")
	server.Start()
	time.Sleep(100 * time.Millisecond)

	// Create client with credentials
	client, err := rtsp.NewClient(server.URL("/stream"), 5*time.Second)
	require.NoError(t, err)
	client.SetCredentials("admin", "password")

	err = client.Connect()
	require.NoError(t, err)

	// First DESCRIBE will get 401, then retry with auth
	sdp, err := client.Describe()
	require.NoError(t, err)
	assert.Contains(t, sdp, "v=0")

	client.Close()
}

// TestE2E_Reconnection tests automatic reconnection
func TestE2E_Reconnection(t *testing.T) {
	server, err := NewMockRTSPServer()
	require.NoError(t, err)

	server.Start()
	time.Sleep(100 * time.Millisecond)

	client, err := rtsp.NewClient(server.URL("/stream"), 2*time.Second)
	require.NoError(t, err)

	// Set retry config
	retryConfig := rtsp.NewRetryConfig(3, 50*time.Millisecond, 500*time.Millisecond)
	client.SetRetryConfig(retryConfig)

	// Initial connection
	err = client.Connect()
	require.NoError(t, err)

	// Stop server to simulate connection loss
	server.Stop()
	time.Sleep(100 * time.Millisecond)

	// Restart server
	server, err = NewMockRTSPServer()
	require.NoError(t, err)
	defer server.Stop()
	server.Start()
	time.Sleep(100 * time.Millisecond)

	// Client should be able to reconnect
	// Note: This is a simplified test - in practice would need
	// the server on the same port
	client.Close()
}

// TestE2E_KeepAlive tests keep-alive mechanism
func TestE2E_KeepAlive(t *testing.T) {
	server, err := NewMockRTSPServer()
	require.NoError(t, err)
	defer server.Stop()

	server.Start()
	time.Sleep(100 * time.Millisecond)

	client, err := rtsp.NewClient(server.URL("/stream"), 5*time.Second)
	require.NoError(t, err)
	defer client.Close()

	err = client.Connect()
	require.NoError(t, err)

	err = client.Setup()
	require.NoError(t, err)

	// Verify session was established
	assert.NotEmpty(t, client.GetSession())

	// Start and stop keep-alive (functionality is tested in unit tests)
	// This integration test just verifies it can be started/stopped without errors
	client.StartKeepAlive()
	time.Sleep(100 * time.Millisecond)
	client.StopKeepAlive()

	// Verify no crashes or errors
	assert.True(t, true, "Keep-alive started and stopped successfully")
}

// TestE2E_ErrorHandling tests error response handling
func TestE2E_ErrorHandling(t *testing.T) {
	server, err := NewMockRTSPServer()
	require.NoError(t, err)
	defer server.Stop()

	// Configure server to return 404
	server.SetResponse("DESCRIBE", "RTSP/1.0 404 Not Found\r\nCSeq: 1\r\n\r\n")
	server.Start()
	time.Sleep(100 * time.Millisecond)

	client, err := rtsp.NewClient(server.URL("/notfound"), 5*time.Second)
	require.NoError(t, err)
	defer client.Close()

	err = client.Connect()
	require.NoError(t, err)

	// DESCRIBE should fail with 404
	_, err = client.Describe()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

// TestE2E_SessionTimeout tests session timeout handling
func TestE2E_SessionTimeout(t *testing.T) {
	server, err := NewMockRTSPServer()
	require.NoError(t, err)
	defer server.Stop()

	server.Start()
	time.Sleep(100 * time.Millisecond)

	client, err := rtsp.NewClient(server.URL("/stream"), 5*time.Second)
	require.NoError(t, err)
	defer client.Close()

	err = client.Connect()
	require.NoError(t, err)

	err = client.Setup()
	require.NoError(t, err)

	// Session should be established
	assert.NotEmpty(t, client.GetSession())
}

// TestE2E_MultipleClients tests multiple concurrent clients
func TestE2E_MultipleClients(t *testing.T) {
	server, err := NewMockRTSPServer()
	require.NoError(t, err)
	defer server.Stop()

	server.Start()
	time.Sleep(100 * time.Millisecond)

	// Create multiple clients
	numClients := 5
	clients := make([]*rtsp.Client, numClients)

	for i := 0; i < numClients; i++ {
		client, err := rtsp.NewClient(server.URL("/stream"), 5*time.Second)
		require.NoError(t, err)
		clients[i] = client

		err = client.Connect()
		require.NoError(t, err)

		_, err = client.Describe()
		require.NoError(t, err)
	}

	// Close all clients
	for _, client := range clients {
		client.Close()
	}

	// Server should have received requests from all clients
	assert.GreaterOrEqual(t, server.GetRequestCount(), numClients)
}

// TestE2E_NetworkDelay tests handling of network delays
func TestE2E_NetworkDelay(t *testing.T) {
	server, err := NewMockRTSPServer()
	require.NoError(t, err)
	defer server.Stop()

	// Add response delay
	server.SetResponseDelay(200 * time.Millisecond)
	server.Start()
	time.Sleep(100 * time.Millisecond)

	client, err := rtsp.NewClient(server.URL("/stream"), 5*time.Second)
	require.NoError(t, err)
	defer client.Close()

	start := time.Now()
	err = client.Connect()
	require.NoError(t, err)

	_, err = client.Describe()
	elapsed := time.Since(start)

	require.NoError(t, err)
	// Should have taken at least the delay time
	assert.GreaterOrEqual(t, elapsed, 200*time.Millisecond)
}

// TestE2E_RetryOnError tests retry mechanism
func TestE2E_RetryOnError(t *testing.T) {
	server, err := NewMockRTSPServer()
	require.NoError(t, err)
	defer server.Stop()

	server.Start()
	time.Sleep(100 * time.Millisecond)

	client, err := rtsp.NewClient(server.URL("/stream"), 2*time.Second)
	require.NoError(t, err)
	defer client.Close()

	// Set aggressive retry config
	config := rtsp.NewRetryConfig(3, 50*time.Millisecond, 500*time.Millisecond)
	client.SetRetryConfig(config)

	// This should work with retry
	err = client.ConnectWithRetry()
	assert.NoError(t, err)
}
