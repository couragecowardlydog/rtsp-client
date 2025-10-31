package test

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// MockRTSPServer is a mock RTSP server for testing
type MockRTSPServer struct {
	listener      net.Listener
	port          int
	running       bool
	mu            sync.Mutex
	requestCount  int
	responses     map[string]string // method -> response
	requireAuth   bool
	authUsername  string
	authPassword  string
	sessionID     string
	lastRequest   string
	connections   []net.Conn
	autoRespond   bool
	responseDelay time.Duration
}

// NewMockRTSPServer creates a new mock RTSP server
func NewMockRTSPServer() (*MockRTSPServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	port := listener.Addr().(*net.TCPAddr).Port

	server := &MockRTSPServer{
		listener:    listener,
		port:        port,
		responses:   make(map[string]string),
		sessionID:   "12345678",
		autoRespond: true,
	}

	return server, nil
}

// Start starts the mock server
func (s *MockRTSPServer) Start() {
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	go s.acceptConnections()
}

// Stop stops the mock server
func (s *MockRTSPServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.running = false

	// Close all connections
	for _, conn := range s.connections {
		conn.Close()
	}

	if s.listener != nil {
		s.listener.Close()
	}
}

// Port returns the server port
func (s *MockRTSPServer) Port() int {
	return s.port
}

// URL returns the server RTSP URL
func (s *MockRTSPServer) URL(path string) string {
	return fmt.Sprintf("rtsp://127.0.0.1:%d%s", s.port, path)
}

// SetRequireAuth enables authentication requirement
func (s *MockRTSPServer) SetRequireAuth(username, password string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.requireAuth = true
	s.authUsername = username
	s.authPassword = password
}

// SetResponse sets a custom response for a method
func (s *MockRTSPServer) SetResponse(method, response string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.responses[method] = response
}

// SetResponseDelay sets delay before responding
func (s *MockRTSPServer) SetResponseDelay(delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.responseDelay = delay
}

// GetRequestCount returns number of requests received
func (s *MockRTSPServer) GetRequestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.requestCount
}

// GetLastRequest returns the last request received
func (s *MockRTSPServer) GetLastRequest() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.lastRequest
}

func (s *MockRTSPServer) acceptConnections() {
	for {
		s.mu.Lock()
		if !s.running {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()

		conn, err := s.listener.Accept()
		if err != nil {
			// Check if server was stopped
			s.mu.Lock()
			running := s.running
			s.mu.Unlock()

			if !running {
				return
			}
			continue
		}

		s.mu.Lock()
		s.connections = append(s.connections, conn)
		s.mu.Unlock()

		go s.handleConnection(conn)
	}
}

func (s *MockRTSPServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	for {
		// Read request
		request, err := s.readRequest(reader)
		if err != nil {
			return
		}

		s.mu.Lock()
		s.requestCount++
		s.lastRequest = request
		s.mu.Unlock()

		// Apply delay if configured
		if s.responseDelay > 0 {
			time.Sleep(s.responseDelay)
		}

		// Parse method
		lines := strings.Split(request, "\r\n")
		if len(lines) == 0 {
			continue
		}

		parts := strings.Fields(lines[0])
		if len(parts) < 2 {
			continue
		}

		method := parts[0]
		cseq := s.extractCSeq(request)

		// Check for authorization
		if s.requireAuth && !s.hasValidAuth(request) {
			response := s.build401Response(cseq)
			conn.Write([]byte(response))
			continue
		}

		// Get or build response
		var response string
		if customResp, ok := s.responses[method]; ok {
			response = customResp
		} else {
			response = s.buildResponse(method, cseq)
		}

		conn.Write([]byte(response))
	}
}

func (s *MockRTSPServer) readRequest(reader *bufio.Reader) (string, error) {
	var request strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}

		request.WriteString(line)

		// End of headers
		if line == "\r\n" {
			break
		}
	}

	return request.String(), nil
}

func (s *MockRTSPServer) extractCSeq(request string) int {
	lines := strings.Split(request, "\r\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "CSeq:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				var cseq int
				fmt.Sscanf(parts[1], "%d", &cseq)
				return cseq
			}
		}
	}
	return 1
}

func (s *MockRTSPServer) hasValidAuth(request string) bool {
	// Simple check - just look for Authorization header
	// In real implementation, would verify the digest
	return strings.Contains(request, "Authorization:")
}

func (s *MockRTSPServer) build401Response(cseq int) string {
	return fmt.Sprintf("RTSP/1.0 401 Unauthorized\r\n"+
		"CSeq: %d\r\n"+
		"WWW-Authenticate: Digest realm=\"RTSP Server\", nonce=\"abc123\"\r\n"+
		"\r\n", cseq)
}

func (s *MockRTSPServer) buildResponse(method string, cseq int) string {
	switch method {
	case "OPTIONS":
		return fmt.Sprintf("RTSP/1.0 200 OK\r\n"+
			"CSeq: %d\r\n"+
			"Public: OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN\r\n"+
			"\r\n", cseq)

	case "DESCRIBE":
		sdp := "v=0\r\n" +
			"o=- 0 0 IN IP4 127.0.0.1\r\n" +
			"s=Test Stream\r\n" +
			"t=0 0\r\n" +
			"m=video 0 RTP/AVP 96\r\n" +
			"a=rtpmap:96 H264/90000\r\n"

		return fmt.Sprintf("RTSP/1.0 200 OK\r\n"+
			"CSeq: %d\r\n"+
			"Content-Type: application/sdp\r\n"+
			"Content-Length: %d\r\n"+
			"\r\n"+
			"%s", cseq, len(sdp), sdp)

	case "SETUP":
		return fmt.Sprintf("RTSP/1.0 200 OK\r\n"+
			"CSeq: %d\r\n"+
			"Session: %s;timeout=60\r\n"+
			"Transport: RTP/AVP;unicast;client_port=50000-50001;server_port=60000-60001\r\n"+
			"\r\n", cseq, s.sessionID)

	case "PLAY":
		return fmt.Sprintf("RTSP/1.0 200 OK\r\n"+
			"CSeq: %d\r\n"+
			"Session: %s\r\n"+
			"RTP-Info: url=rtsp://127.0.0.1:%d/stream;seq=1;rtptime=0\r\n"+
			"\r\n", cseq, s.sessionID, s.port)

	case "TEARDOWN":
		return fmt.Sprintf("RTSP/1.0 200 OK\r\n"+
			"CSeq: %d\r\n"+
			"Session: %s\r\n"+
			"\r\n", cseq, s.sessionID)

	case "GET_PARAMETER":
		return fmt.Sprintf("RTSP/1.0 200 OK\r\n"+
			"CSeq: %d\r\n"+
			"Session: %s\r\n"+
			"\r\n", cseq, s.sessionID)

	default:
		return fmt.Sprintf("RTSP/1.0 200 OK\r\n"+
			"CSeq: %d\r\n"+
			"\r\n", cseq)
	}
}
