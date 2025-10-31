package rtsp

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseInterleavedFrame tests parsing $ prefixed RTP/RTCP frames
func TestParseInterleavedFrame(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		expectError bool
		channel     uint8
		length      uint16
		payload     []byte
	}{
		{
			name: "valid RTP frame on channel 0",
			data: []byte{
				'$', 0x00, // Channel 0
				0x00, 0x0C, // Length: 12 bytes
				// 12 bytes of RTP payload
				0x80, 0x60, 0x00, 0x01,
				0x00, 0x00, 0x00, 0x00,
				0x12, 0x34, 0x56, 0x78,
			},
			expectError: false,
			channel:     0,
			length:      12,
			payload: []byte{
				0x80, 0x60, 0x00, 0x01,
				0x00, 0x00, 0x00, 0x00,
				0x12, 0x34, 0x56, 0x78,
			},
		},
		{
			name: "valid RTCP frame on channel 1",
			data: []byte{
				'$', 0x01, // Channel 1
				0x00, 0x08, // Length: 8 bytes
				// 8 bytes of RTCP payload
				0x80, 0xC8, 0x00, 0x01,
				0x11, 0x22, 0x33, 0x44,
			},
			expectError: false,
			channel:     1,
			length:      8,
			payload: []byte{
				0x80, 0xC8, 0x00, 0x01,
				0x11, 0x22, 0x33, 0x44,
			},
		},
		{
			name:        "frame too short (no header)",
			data:        []byte{'$', 0x00, 0x00},
			expectError: true,
		},
		{
			name: "frame missing $ prefix",
			data: []byte{
				0x00, 0x00,
				0x00, 0x04,
				0x11, 0x22, 0x33, 0x44,
			},
			expectError: true,
		},
		{
			name: "frame with truncated payload",
			data: []byte{
				'$', 0x00,
				0x00, 0x10, // Says 16 bytes
				// But only 4 bytes follow
				0x11, 0x22, 0x33, 0x44,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := ParseInterleavedFrame(tt.data)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, frame)
			assert.Equal(t, tt.channel, frame.Channel)
			assert.Equal(t, tt.length, frame.Length)
			assert.Equal(t, tt.payload, frame.Payload)
		})
	}
}

// TestBuildInterleavedFrame tests building $ prefixed frames
func TestBuildInterleavedFrame(t *testing.T) {
	tests := []struct {
		name     string
		channel  uint8
		payload  []byte
		expected []byte
	}{
		{
			name:    "build RTP frame",
			channel: 0,
			payload: []byte{0x80, 0x60, 0x00, 0x01, 0x12, 0x34},
			expected: []byte{
				'$', 0x00, // Channel 0
				0x00, 0x06, // Length: 6
				0x80, 0x60, 0x00, 0x01, 0x12, 0x34,
			},
		},
		{
			name:    "build RTCP frame",
			channel: 1,
			payload: []byte{0x80, 0xC8, 0x00, 0x01},
			expected: []byte{
				'$', 0x01, // Channel 1
				0x00, 0x04, // Length: 4
				0x80, 0xC8, 0x00, 0x01,
			},
		},
		{
			name:    "empty payload",
			channel: 0,
			payload: []byte{},
			expected: []byte{
				'$', 0x00,
				0x00, 0x00,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildInterleavedFrame(tt.channel, tt.payload)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestInterleavedFrameReader tests reading multiple frames from stream
func TestInterleavedFrameReader(t *testing.T) {
	// Create a buffer with multiple frames
	var buf bytes.Buffer

	// Frame 1: RTP on channel 0
	buf.Write([]byte{
		'$', 0x00, 0x00, 0x04,
		0x11, 0x22, 0x33, 0x44,
	})

	// Frame 2: RTCP on channel 1
	buf.Write([]byte{
		'$', 0x01, 0x00, 0x03,
		0xAA, 0xBB, 0xCC,
	})

	// Frame 3: RTP on channel 0
	buf.Write([]byte{
		'$', 0x00, 0x00, 0x02,
		0xFF, 0xEE,
	})

	reader := NewInterleavedReader(&buf)

	// Read first frame
	frame1, err := reader.ReadFrame()
	require.NoError(t, err)
	assert.Equal(t, uint8(0), frame1.Channel)
	assert.Equal(t, []byte{0x11, 0x22, 0x33, 0x44}, frame1.Payload)

	// Read second frame
	frame2, err := reader.ReadFrame()
	require.NoError(t, err)
	assert.Equal(t, uint8(1), frame2.Channel)
	assert.Equal(t, []byte{0xAA, 0xBB, 0xCC}, frame2.Payload)

	// Read third frame
	frame3, err := reader.ReadFrame()
	require.NoError(t, err)
	assert.Equal(t, uint8(0), frame3.Channel)
	assert.Equal(t, []byte{0xFF, 0xEE}, frame3.Payload)
}

// TestInterleavedChannelDemux tests demultiplexing RTP and RTCP channels
func TestInterleavedChannelDemux(t *testing.T) {
	demux := NewChannelDemux()

	// Register handlers
	rtpFrames := [][]byte{}
	rtcpFrames := [][]byte{}

	demux.SetRTPHandler(func(payload []byte) {
		rtpFrames = append(rtpFrames, payload)
	})

	demux.SetRTCPHandler(func(payload []byte) {
		rtcpFrames = append(rtcpFrames, payload)
	})

	// Send RTP frame (channel 0)
	rtpData := []byte{0x80, 0x60, 0x00, 0x01}
	demux.HandleFrame(&InterleavedFrame{
		Channel: 0,
		Length:  4,
		Payload: rtpData,
	})

	// Send RTCP frame (channel 1)
	rtcpData := []byte{0x80, 0xC8, 0x00, 0x01}
	demux.HandleFrame(&InterleavedFrame{
		Channel: 1,
		Length:  4,
		Payload: rtcpData,
	})

	// Send another RTP frame
	rtp2Data := []byte{0x80, 0x60, 0x00, 0x02}
	demux.HandleFrame(&InterleavedFrame{
		Channel: 0,
		Length:  4,
		Payload: rtp2Data,
	})

	// Verify demultiplexing
	assert.Len(t, rtpFrames, 2)
	assert.Len(t, rtcpFrames, 1)
	assert.Equal(t, rtpData, rtpFrames[0])
	assert.Equal(t, rtp2Data, rtpFrames[1])
	assert.Equal(t, rtcpData, rtcpFrames[0])
}

// TestSetupTCPTransport tests SETUP with TCP transport
func TestSetupTCPTransport(t *testing.T) {
	request := buildRequest("SETUP", "rtsp://example.com/stream", 2, "")

	// Default should be UDP
	assert.Contains(t, request, "RTP/AVP;unicast;client_port")

	// TCP interleaved transport
	tcpRequest := buildRequestWithTCPTransport("SETUP", "rtsp://example.com/stream", 2, "")
	assert.Contains(t, tcpRequest, "RTP/AVP/TCP;unicast;interleaved=0-1")
}

// TestInterleavedTransportParsing tests parsing Transport header with interleaved
func TestInterleavedTransportParsing(t *testing.T) {
	tests := []struct {
		name        string
		transport   string
		isTCP       bool
		rtpChannel  uint8
		rtcpChannel uint8
	}{
		{
			name:        "TCP interleaved 0-1",
			transport:   "RTP/AVP/TCP;unicast;interleaved=0-1",
			isTCP:       true,
			rtpChannel:  0,
			rtcpChannel: 1,
		},
		{
			name:        "TCP interleaved 2-3",
			transport:   "RTP/AVP/TCP;unicast;interleaved=2-3",
			isTCP:       true,
			rtpChannel:  2,
			rtcpChannel: 3,
		},
		{
			name:      "UDP transport",
			transport: "RTP/AVP;unicast;client_port=50000-50001",
			isTCP:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := ParseTransportHeader(tt.transport)
			assert.Equal(t, tt.isTCP, info.IsTCP)

			if tt.isTCP {
				assert.Equal(t, tt.rtpChannel, info.RTPChannel)
				assert.Equal(t, tt.rtcpChannel, info.RTCPChannel)
			}
		})
	}
}

// TestClientInterleavedMode tests client with TCP interleaved mode
func TestClientInterleavedMode(t *testing.T) {
	client, err := NewClient("rtsp://example.com/stream", 0)
	require.NoError(t, err)

	// Enable TCP mode
	client.SetTransportMode(TransportModeTCP)

	// Verify mode is set
	assert.Equal(t, TransportModeTCP, client.GetTransportMode())
}
