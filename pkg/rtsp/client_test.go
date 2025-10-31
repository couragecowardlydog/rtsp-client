package rtsp

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expectError bool
		host        string
		port        string
		path        string
	}{
		{
			name:        "valid RTSP URL with port",
			url:         "rtsp://192.168.1.100:554/stream",
			expectError: false,
			host:        "192.168.1.100",
			port:        "554",
			path:        "/stream",
		},
		{
			name:        "valid RTSP URL without port",
			url:         "rtsp://example.com/live",
			expectError: false,
			host:        "example.com",
			port:        "554",
			path:        "/live",
		},
		{
			name:        "valid RTSP URL with authentication",
			url:         "rtsp://user:pass@192.168.1.100/stream",
			expectError: false,
			host:        "192.168.1.100",
			port:        "554",
			path:        "/stream",
		},
		{
			name:        "invalid scheme",
			url:         "http://example.com/stream",
			expectError: true,
		},
		{
			name:        "empty URL",
			url:         "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, err := parseURL(tt.url)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.host, host)
				assert.Equal(t, tt.port, port)
			}
		})
	}
}

func TestBuildRequest(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		url      string
		cseq     int
		session  string
		expected string
	}{
		{
			name:     "DESCRIBE request",
			method:   "DESCRIBE",
			url:      "rtsp://example.com/stream",
			cseq:     1,
			session:  "",
			expected: "DESCRIBE rtsp://example.com/stream RTSP/1.0\r\nCSeq: 1\r\nAccept: application/sdp\r\nUser-Agent: RTSP-Client/1.0\r\n\r\n",
		},
		{
			name:     "SETUP request",
			method:   "SETUP",
			url:      "rtsp://example.com/stream",
			cseq:     2,
			session:  "",
			expected: "SETUP rtsp://example.com/stream RTSP/1.0\r\nCSeq: 2\r\nTransport: RTP/AVP;unicast;client_port=50000-50001\r\nUser-Agent: RTSP-Client/1.0\r\n\r\n",
		},
		{
			name:     "PLAY request with session",
			method:   "PLAY",
			url:      "rtsp://example.com/stream",
			cseq:     3,
			session:  "12345678",
			expected: "PLAY rtsp://example.com/stream RTSP/1.0\r\nCSeq: 3\r\nSession: 12345678\r\nRange: npt=0.000-\r\nUser-Agent: RTSP-Client/1.0\r\n\r\n",
		},
		{
			name:     "TEARDOWN request with session",
			method:   "TEARDOWN",
			url:      "rtsp://example.com/stream",
			cseq:     4,
			session:  "12345678",
			expected: "TEARDOWN rtsp://example.com/stream RTSP/1.0\r\nCSeq: 4\r\nSession: 12345678\r\nUser-Agent: RTSP-Client/1.0\r\n\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildRequest(tt.method, tt.url, tt.cseq, tt.session)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name        string
		response    string
		expectError bool
		statusCode  int
		session     string
		contentType string
	}{
		{
			name: "200 OK with session",
			response: "RTSP/1.0 200 OK\r\n" +
				"CSeq: 2\r\n" +
				"Session: 12345678;timeout=60\r\n" +
				"Transport: RTP/AVP;unicast;client_port=50000-50001;server_port=60000-60001\r\n\r\n",
			expectError: false,
			statusCode:  200,
			session:     "12345678",
		},
		{
			name: "200 OK with content",
			response: "RTSP/1.0 200 OK\r\n" +
				"CSeq: 1\r\n" +
				"Content-Type: application/sdp\r\n" +
				"Content-Length: 10\r\n\r\n" +
				"test data",
			expectError: false,
			statusCode:  200,
			contentType: "application/sdp",
		},
		{
			name: "404 Not Found",
			response: "RTSP/1.0 404 Not Found\r\n" +
				"CSeq: 1\r\n\r\n",
			expectError: false,
			statusCode:  404,
		},
		{
			name:        "invalid response",
			response:    "invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statusCode, headers, body, err := parseResponse(tt.response)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.statusCode, statusCode)

				if tt.session != "" {
					assert.Contains(t, headers["Session"], tt.session)
				}

				if tt.contentType != "" {
					assert.Equal(t, tt.contentType, headers["Content-Type"])
					assert.Equal(t, "test data", body)
				}
			}
		})
	}
}

func TestClient_New(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		timeout     time.Duration
		expectError bool
	}{
		{
			name:        "valid configuration",
			url:         "rtsp://example.com/stream",
			timeout:     10 * time.Second,
			expectError: false,
		},
		{
			name:        "invalid URL",
			url:         "http://example.com/stream",
			timeout:     10 * time.Second,
			expectError: true,
		},
		{
			name:        "zero timeout defaults to 10s",
			url:         "rtsp://example.com/stream",
			timeout:     0,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.url, tt.timeout)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, client)
			} else {
				require.NoError(t, err)
				require.NotNil(t, client)
				assert.Equal(t, tt.url, client.url)
				if tt.timeout == 0 {
					assert.Equal(t, 10*time.Second, client.timeout)
				} else {
					assert.Equal(t, tt.timeout, client.timeout)
				}
			}
		})
	}
}

func TestExtractServerPorts(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		expected  []int
	}{
		{
			name:      "valid server ports",
			transport: "RTP/AVP;unicast;client_port=50000-50001;server_port=60000-60001",
			expected:  []int{60000, 60001},
		},
		{
			name:      "no server ports",
			transport: "RTP/AVP;unicast;client_port=50000-50001",
			expected:  []int{},
		},
		{
			name:      "invalid format",
			transport: "RTP/AVP;unicast;server_port=invalid",
			expected:  []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractServerPorts(tt.transport)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClient_Close(t *testing.T) {
	client := &Client{
		cseq: 1,
		ctx:  context.Background(),
	}

	err := client.Close()
	assert.NoError(t, err)
}

func TestParseSDPInfo(t *testing.T) {
	sdp := `v=0
o=- 0 0 IN IP4 127.0.0.1
s=Session streamed by "test"
t=0 0
a=control:*
m=video 0 RTP/AVP 96
a=rtpmap:96 H264/90000
a=fmtp:96 packetization-mode=1;profile-level-id=42e01f
a=control:trackID=0
m=audio 0 RTP/AVP 97
a=rtpmap:97 mpeg4-generic/48000/2
a=fmtp:97 streamtype=5;profile-level-id=15
a=control:trackID=1`

	info := parseSDPInfo(sdp, "rtsp://example.com/stream/", "rtsp://example.com/stream")

	require.NotNil(t, info)
	assert.Equal(t, "rtsp://example.com/stream/", info.AggregateControl)
	require.Len(t, info.Tracks, 2)

	video := info.Tracks[0]
	assert.Equal(t, "video", video.Media)
	assert.Equal(t, 96, video.PayloadType)
	assert.Equal(t, "H264", strings.ToUpper(video.Codec))
	assert.Equal(t, "rtsp://example.com/stream/trackID=0", video.ControlURL)
	assert.Equal(t, "1", video.FMTP["packetization-mode"])

	audio := info.Tracks[1]
	assert.Equal(t, "audio", audio.Media)
	assert.Equal(t, 97, audio.PayloadType)
	assert.Equal(t, "MPEG4-GENERIC", strings.ToUpper(audio.Codec))
	assert.Equal(t, "rtsp://example.com/stream/trackID=1", audio.ControlURL)
	assert.Equal(t, "5", audio.FMTP["streamtype"])
}

func TestParseSDPInfoFallbackToRequestURL(t *testing.T) {
	sdp := `v=0
o=- 0 0 IN IP4 127.0.0.1
s=Session streamed by "test"
t=0 0
m=video 0 RTP/AVP 96
a=control:trackID=2`

	info := parseSDPInfo(sdp, "", "rtsp://example.com/stream")

	require.NotNil(t, info)
	assert.Empty(t, info.AggregateControl)
	require.Len(t, info.Tracks, 1)
	assert.Equal(t, "rtsp://example.com/stream/trackID=2", info.Tracks[0].ControlURL)
}

func TestClient_SetupUsesTCPTransport(t *testing.T) {
	response := []byte("RTSP/1.0 200 OK\r\n" +
		"CSeq: 1\r\n" +
		"Session: 12345678;timeout=60\r\n" +
		"Transport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n\r\n")

	conn := newMockConn(response)

	client := &Client{
		url:         "rtsp://example.com/stream",
		host:        "example.com",
		port:        "554",
		timeout:     time.Second,
		clientPorts: []int{50000, 50001},
		conn:        conn,
		ctx:         context.Background(),
	}

	client.SetTransportMode(TransportModeTCP)

	err := client.Setup()
	require.NoError(t, err)

	assert.Contains(t, conn.writeBuf.String(), "RTP/AVP/TCP;unicast;interleaved=0-1")
	assert.Nil(t, client.rtpConn, "RTP UDP connection should remain unused in TCP mode")
	assert.Nil(t, client.rtcpConn, "RTCP UDP connection should remain unused in TCP mode")
}

func TestClient_ReadPacketTCP(t *testing.T) {
	rtpPayload := []byte{
		0x80, 0x60, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x01,
		0xde, 0xad, 0xbe, 0xef,
		0x65, 0x88, 0x99, 0xaa,
	}

	frame := make([]byte, 4+len(rtpPayload))
	frame[0] = '$'
	frame[1] = 0 // RTP channel
	frame[2] = byte(len(rtpPayload) >> 8)
	frame[3] = byte(len(rtpPayload))
	copy(frame[4:], rtpPayload)

	conn := newMockConn(frame)

	client := &Client{
		conn:          conn,
		timeout:       time.Second,
		transportMode: TransportModeTCP,
		ctx:           context.Background(),
	}

	packet, err := client.ReadPacket()
	require.NoError(t, err)
	require.NotNil(t, packet)
	assert.Equal(t, uint16(1), packet.SequenceNumber)
	assert.Equal(t, uint32(1), packet.Timestamp)
	assert.Equal(t, uint32(0xdeadbeef), packet.SSRC)
	assert.Equal(t, byte(0x60), packet.PayloadType)
}

func TestClient_SetupUsesTrackControlURL(t *testing.T) {
	response := []byte("RTSP/1.0 200 OK\r\n" +
		"CSeq: 1\r\n" +
		"Session: 12345678;timeout=60\r\n" +
		"Transport: RTP/AVP;unicast;client_port=50000-50001;server_port=60000-60001\r\n\r\n")

	conn := newMockConn(response)

	client := &Client{
		url:           "rtsp://example.com/stream",
		host:          "example.com",
		port:          "554",
		timeout:       time.Second,
		clientPorts:   []int{50000, 50001},
		conn:          conn,
		ctx:           context.Background(),
		transportMode: TransportModeTCP,
	}

	client.sdpInfo = &SDPInfo{
		Tracks: []SDPTrack{
			{ControlURL: "rtsp://example.com/stream/trackID=0"},
			{ControlURL: "rtsp://example.com/stream/trackID=1"},
		},
	}

	err := client.Setup()
	require.NoError(t, err)

	request := conn.writeBuf.String()
	require.True(t, strings.HasPrefix(request, "SETUP rtsp://example.com/stream/trackID=0 RTSP/1.0"), "expected SETUP to target first track control URI, got: %s", request)

	assert.Equal(t, 1, client.trackIndex, "track index should advance after successful SETUP")
}

func TestClient_PlayUsesAggregateControl(t *testing.T) {
	response := []byte("RTSP/1.0 200 OK\r\n" +
		"CSeq: 2\r\n" +
		"Session: 12345678\r\n\r\n")

	conn := newMockConn(response)

	client := &Client{
		url:              "rtsp://example.com/stream",
		host:             "example.com",
		port:             "554",
		timeout:          time.Second,
		conn:             conn,
		ctx:              context.Background(),
		session:          "12345678",
		aggregateControl: "rtsp://example.com/stream/",
	}

	err := client.Play()
	require.NoError(t, err)

	request := conn.writeBuf.String()
	require.True(t, strings.HasPrefix(request, "PLAY rtsp://example.com/stream/ RTSP/1.0"), "expected PLAY to target aggregate control URI, got: %s", request)
}

type mockConn struct {
	readBuf  bytes.Buffer
	writeBuf bytes.Buffer
}

func newMockConn(chunks ...[]byte) *mockConn {
	mc := &mockConn{}
	for _, chunk := range chunks {
		mc.readBuf.Write(chunk)
	}
	return mc
}

func (m *mockConn) Read(b []byte) (int, error) {
	if m.readBuf.Len() == 0 {
		return 0, io.EOF
	}
	return m.readBuf.Read(b)
}

func (m *mockConn) Write(b []byte) (int, error) {
	return m.writeBuf.Write(b)
}

func (m *mockConn) Close() error {
	return nil
}

func (m *mockConn) LocalAddr() net.Addr {
	return mockAddr{}
}

func (m *mockConn) RemoteAddr() net.Addr {
	return mockAddr{}
}

func (m *mockConn) SetDeadline(time.Time) error {
	return nil
}

func (m *mockConn) SetReadDeadline(time.Time) error {
	return nil
}

func (m *mockConn) SetWriteDeadline(time.Time) error {
	return nil
}

type mockAddr struct{}

func (mockAddr) Network() string { return "mock" }

func (mockAddr) String() string { return "mock" }
