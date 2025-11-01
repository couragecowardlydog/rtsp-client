package test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rtsp-client/pkg/decoder"
	"github.com/rtsp-client/pkg/rtsp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findSampleVideo finds the sample video file in common locations
// Returns the absolute path if found, empty string if not found
func findSampleVideo() string {
	// Try to find sample.mp4 in common locations
	locations := []string{
		"sample.mp4",
		"./sample.mp4",
		filepath.Join("..", "sample.mp4"),
		filepath.Join(".", "sample.mp4"),
		filepath.Join("test", "sample.mp4"),
		filepath.Join("..", "test", "sample.mp4"),
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			absPath, err := filepath.Abs(loc)
			if err == nil {
				return absPath
			}
		}
	}

	return ""
}

// TestRealWorld_MediaMTX_BasicStream tests connecting to a real MediaMTX server
// with ffmpeg streaming a video file
func TestRealWorld_MediaMTX_BasicStream(t *testing.T) {
	// Find sample video
	sampleVideo := findSampleVideo()
	if sampleVideo == "" {
		t.Skip("Skipping test: sample.mp4 not found in project root or common locations")
	}
	t.Logf("Using sample video: %s", sampleVideo)

	// Setup MediaMTX server
	mediamtx := NewMediaMTXServer("mediamtx-test", 8554)
	defer mediamtx.Stop()

	// Start MediaMTX
	err := mediamtx.Start()
	require.NoError(t, err, "Failed to start MediaMTX server")
	t.Log("MediaMTX server started")

	// Setup ffmpeg stream
	streamPath := "/mystream"
	streamURL := mediamtx.URL(streamPath)
	ffmpegStream, err := NewFFmpegStream(sampleVideo, streamURL, true)
	require.NoError(t, err, "Failed to create ffmpeg stream")
	defer ffmpegStream.Stop()

	// Start streaming
	err = ffmpegStream.Start()
	require.NoError(t, err, "Failed to start ffmpeg stream")
	t.Log("FFmpeg stream started")

	// Give stream time to initialize
	time.Sleep(3 * time.Second)

	// Create RTSP client
	client, err := rtsp.NewClient(streamURL, 10*time.Second)
	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	// Connect to server
	err = client.Connect()
	require.NoError(t, err, "Failed to connect to RTSP server")
	t.Log("Connected to RTSP server")

	// Get stream description
	sdp, err := client.Describe()
	require.NoError(t, err, "Failed to DESCRIBE stream")
	assert.Contains(t, sdp, "v=0", "SDP should contain version")
	t.Logf("Received SDP (length: %d bytes)", len(sdp))

	// Setup stream
	err = client.Setup()
	require.NoError(t, err, "Failed to SETUP stream")
	t.Log("Stream setup complete")

	// Start playing
	err = client.Play()
	require.NoError(t, err, "Failed to PLAY stream")
	t.Log("Stream playback started")

	// Read packets for a short duration
	decoder := decoder.NewH264Decoder()
	framesReceived := 0
	keyframesReceived := 0
	startTime := time.Now()
	testDuration := 5 * time.Second

	t.Log("Reading packets from stream...")
	for time.Since(startTime) < testDuration {
		packet, err := client.ReadPacket()
		if err != nil {
			// Timeout is acceptable
			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				continue
			}
			t.Logf("Packet read error (might be timeout): %v", err)
			continue
		}

		// Process packet
		frame := decoder.ProcessPacket(packet)
		if frame != nil {
			framesReceived++
			if frame.IsKey {
				keyframesReceived++
			}
		}
	}

	// Verify we received frames
	assert.Greater(t, framesReceived, 0, "Should receive at least one frame")
	assert.Greater(t, keyframesReceived, 0, "Should receive at least one keyframe")
	t.Logf("Received %d frames (%d keyframes) in %v", framesReceived, keyframesReceived, testDuration)

	// Teardown
	err = client.Teardown()
	assert.NoError(t, err, "TEARDOWN should succeed")
	t.Log("Stream teardown complete")
}

// TestRealWorld_MediaMTX_FrameDecoding tests that frames can be decoded correctly
// from a real stream
func TestRealWorld_MediaMTX_FrameDecoding(t *testing.T) {
	// Find sample video
	sampleVideo := findSampleVideo()
	if sampleVideo == "" {
		t.Skip("Skipping test: sample.mp4 not found in project root or common locations")
	}
	t.Logf("Using sample video: %s", sampleVideo)

	// Setup MediaMTX server
	mediamtx := NewMediaMTXServer("mediamtx-test-decode", 8555)
	defer mediamtx.Stop()

	err := mediamtx.Start()
	require.NoError(t, err)
	t.Log("MediaMTX server started")

	// Setup ffmpeg stream
	streamURL := mediamtx.URL("/testdecode")
	ffmpegStream, err := NewFFmpegStream(sampleVideo, streamURL, true)
	require.NoError(t, err)
	defer ffmpegStream.Stop()

	err = ffmpegStream.Start()
	require.NoError(t, err)
	t.Log("FFmpeg stream started")

	// Give stream time to initialize
	time.Sleep(3 * time.Second)

	// Create and connect client
	client, err := rtsp.NewClient(streamURL, 10*time.Second)
	require.NoError(t, err)
	defer client.Close()

	err = client.Connect()
	require.NoError(t, err)

	_, err = client.Describe()
	require.NoError(t, err)

	err = client.Setup()
	require.NoError(t, err)

	err = client.Play()
	require.NoError(t, err)

	// Decode frames
	decoder := decoder.NewH264Decoder()
	frameCount := 0
	corruptedFrames := 0
	startTime := time.Now()
	maxTestDuration := 10 * time.Second
	minFramesToTest := 10

	t.Log("Decoding frames from stream...")
	for time.Since(startTime) < maxTestDuration && frameCount < minFramesToTest*2 {
		packet, err := client.ReadPacket()
		if err != nil {
			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				continue
			}
			break
		}

		frame := decoder.ProcessPacket(packet)
		if frame != nil {
			frameCount++
			if frame.IsCorrupted {
				corruptedFrames++
			}
			// Verify frame has data
			assert.NotEmpty(t, frame.Data, "Frame should have data")
			assert.Greater(t, len(frame.Data), 0, "Frame data length should be > 0")
		}
	}

	// Verify decoding results
	assert.GreaterOrEqual(t, frameCount, minFramesToTest, "Should decode at least %d frames", minFramesToTest)
	t.Logf("Decoded %d frames (corrupted: %d)", frameCount, corruptedFrames)

	// Corrupted frames should be minimal (ideally 0, but allow some tolerance)
	corruptionRate := float64(corruptedFrames) / float64(frameCount)
	t.Logf("Corruption rate: %.2f%%", corruptionRate*100)
	assert.Less(t, corruptionRate, 0.5, "Corruption rate should be less than 50%%")
}

// TestRealWorld_MediaMTX_StreamReconnection tests reconnection behavior
func TestRealWorld_MediaMTX_StreamReconnection(t *testing.T) {
	// Find sample video
	sampleVideo := findSampleVideo()
	if sampleVideo == "" {
		t.Skip("Skipping test: sample.mp4 not found in project root or common locations")
	}
	t.Logf("Using sample video: %s", sampleVideo)

	// Setup MediaMTX server
	mediamtx := NewMediaMTXServer("mediamtx-test-reconnect", 8556)
	defer mediamtx.Stop()

	err := mediamtx.Start()
	require.NoError(t, err)

	streamURL := mediamtx.URL("/reconnect")
	ffmpegStream, err := NewFFmpegStream(sampleVideo, streamURL, true)
	require.NoError(t, err)
	defer ffmpegStream.Stop()

	err = ffmpegStream.Start()
	require.NoError(t, err)
	time.Sleep(3 * time.Second)

	// Create client with retry config
	client, err := rtsp.NewClient(streamURL, 5*time.Second)
	require.NoError(t, err)
	defer client.Close()

	retryConfig := rtsp.NewRetryConfig(3, 100*time.Millisecond, 1*time.Second)
	client.SetRetryConfig(retryConfig)

	// Initial connection
	err = client.ConnectWithRetry()
	require.NoError(t, err)

	sdp, err := client.Describe()
	require.NoError(t, err)
	assert.Contains(t, sdp, "v=0")

	err = client.Setup()
	require.NoError(t, err)

	err = client.Play()
	require.NoError(t, err)

	// Read some packets
	decoder := decoder.NewH264Decoder()
	framesBeforeReconnect := 0
	startTime := time.Now()
	for time.Since(startTime) < 3*time.Second && framesBeforeReconnect < 5 {
		packet, err := client.ReadPacket()
		if err != nil {
			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				continue
			}
			break
		}
		frame := decoder.ProcessPacket(packet)
		if frame != nil {
			framesBeforeReconnect++
		}
	}

	// Simulate connection loss by stopping and restarting server
	t.Log("Simulating connection loss...")
	err = mediamtx.Stop()
	require.NoError(t, err)
	time.Sleep(1 * time.Second)

	// Restart server
	err = mediamtx.Start()
	require.NoError(t, err)
	time.Sleep(2 * time.Second)

	// Restart stream
	ffmpegStream2, err := NewFFmpegStream(sampleVideo, streamURL, true)
	require.NoError(t, err)
	defer ffmpegStream2.Stop()
	err = ffmpegStream2.Start()
	require.NoError(t, err)
	time.Sleep(2 * time.Second)

	// Attempt recovery
	decoder.Reset()
	err = client.RecoverSession()
	if err != nil {
		// Recovery might fail, try full reconnection
		client.Close()
		client, err = rtsp.NewClient(streamURL, 5*time.Second)
		require.NoError(t, err)
		client.SetRetryConfig(retryConfig)

		err = client.ConnectWithRetry()
		require.NoError(t, err)

		_, err = client.Describe()
		require.NoError(t, err)

		err = client.Setup()
		require.NoError(t, err)

		err = client.Play()
		require.NoError(t, err)
	}

	// Verify we can read packets after reconnection
	framesAfterReconnect := 0
	startTime = time.Now()
	for time.Since(startTime) < 3*time.Second && framesAfterReconnect < 5 {
		packet, err := client.ReadPacket()
		if err != nil {
			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				continue
			}
			break
		}
		frame := decoder.ProcessPacket(packet)
		if frame != nil {
			framesAfterReconnect++
		}
	}

	assert.Greater(t, framesAfterReconnect, 0, "Should receive frames after reconnection")
	t.Logf("Reconnection test: %d frames before, %d frames after reconnect", framesBeforeReconnect, framesAfterReconnect)
}

// TestRealWorld_MediaMTX_MultipleClients tests multiple clients connecting
// to the same stream
func TestRealWorld_MediaMTX_MultipleClients(t *testing.T) {
	// Find sample video
	sampleVideo := findSampleVideo()
	if sampleVideo == "" {
		t.Skip("Skipping test: sample.mp4 not found in project root or common locations")
	}
	t.Logf("Using sample video: %s", sampleVideo)

	// Setup MediaMTX server
	mediamtx := NewMediaMTXServer("mediamtx-test-multi", 8557)
	defer mediamtx.Stop()

	err := mediamtx.Start()
	require.NoError(t, err)

	streamURL := mediamtx.URL("/multiclient")
	ffmpegStream, err := NewFFmpegStream(sampleVideo, streamURL, true)
	require.NoError(t, err)
	defer ffmpegStream.Stop()

	err = ffmpegStream.Start()
	require.NoError(t, err)
	time.Sleep(3 * time.Second)

	// Create multiple clients
	numClients := 3
	clients := make([]*rtsp.Client, numClients)
	decoders := make([]*decoder.H264Decoder, numClients)

	for i := 0; i < numClients; i++ {
		client, err := rtsp.NewClient(streamURL, 10*time.Second)
		require.NoError(t, err)
		clients[i] = client
		decoders[i] = decoder.NewH264Decoder()

		err = client.Connect()
		require.NoError(t, err, "Client %d: connect failed", i)

		_, err = client.Describe()
		require.NoError(t, err, "Client %d: describe failed", i)

		err = client.Setup()
		require.NoError(t, err, "Client %d: setup failed", i)

		err = client.Play()
		require.NoError(t, err, "Client %d: play failed", i)
	}

	// Read packets from all clients
	frameCounts := make([]int, numClients)
	startTime := time.Now()
	testDuration := 5 * time.Second

	t.Logf("Reading packets from %d clients...", numClients)
	for time.Since(startTime) < testDuration {
		for i, client := range clients {
			packet, err := client.ReadPacket()
			if err != nil {
				if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
					continue
				}
				continue
			}

			frame := decoders[i].ProcessPacket(packet)
			if frame != nil {
				frameCounts[i]++
			}
		}
	}

	// Verify all clients received frames
	for i, count := range frameCounts {
		assert.Greater(t, count, 0, "Client %d should receive frames", i)
		t.Logf("Client %d received %d frames", i, count)
	}

	// Cleanup
	for _, client := range clients {
		client.Teardown()
		client.Close()
	}
}

// TestRealWorld_MediaMTX_FindSampleVideo checks if sample video exists
// and provides helpful error message if not
func TestRealWorld_MediaMTX_FindSampleVideo(t *testing.T) {
	foundPath := findSampleVideo()

	if foundPath == "" {
		t.Skipf("Sample video not found. Please place sample.mp4 in the project root.")
		return
	}

	info, err := os.Stat(foundPath)
	require.NoError(t, err)

	t.Logf("Found sample video: %s (size: %d bytes)", foundPath, info.Size())
	assert.NotZero(t, info.Size(), "Sample video should not be empty")
}



