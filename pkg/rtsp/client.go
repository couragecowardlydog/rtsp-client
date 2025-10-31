package rtsp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rtsp-client/pkg/rtp"
)

var (
	// ErrInvalidURL indicates the RTSP URL is invalid
	ErrInvalidURL = errors.New("invalid RTSP URL")
	// ErrConnectionFailed indicates connection to RTSP server failed
	ErrConnectionFailed = errors.New("failed to connect to RTSP server")
	// ErrRequestFailed indicates RTSP request failed
	ErrRequestFailed = errors.New("RTSP request failed")
	// ErrInvalidResponse indicates invalid RTSP response
	ErrInvalidResponse = errors.New("invalid RTSP response")
)

// Client represents an RTSP client
type Client struct {
	url                 string
	host                string
	port                string
	timeout             time.Duration
	conn                net.Conn
	rtpConn             net.Conn
	rtcpConn            net.Conn
	cseq                int
	session             string
	ctx                 context.Context
	cancel              context.CancelFunc
	clientPorts         []int
	serverPorts         []int
	username            string
	password            string
	authChallenge       *AuthChallenge
	authNonceCount      int
	clientNonce         string
	lastNonce           string
	sessionTimeout      time.Duration
	lastKeepAlive       time.Time
	keepAliveStop       chan struct{}
	serverCapabilities  []string
	retryConfig         *RetryConfig
	recoveryMetrics     *RecoveryMetrics
	redirectCount       int
	transportMode       TransportMode
	rtpChannel          uint8
	rtcpChannel         uint8
	trackIndex          int
	aggregateControl    string
	sdpInfo             *SDPInfo
	expectedPayloadType uint8
	payloadTypeInit     bool
}

// SDPInfo captures parsed SDP metadata for aggregate and track-level details.
type SDPInfo struct {
	AggregateControl string
	Tracks           []SDPTrack
}

// SDPTrack represents an individual media track described inside SDP.
type SDPTrack struct {
	ControlURL  string
	Media       string
	PayloadType int
	Codec       string
	ClockRate   int
	Channels    int
	FMTP        map[string]string
}

// NewClient creates a new RTSP client
func NewClient(rtspURL string, timeout time.Duration) (*Client, error) {
	host, port, username, password, err := parseURLWithAuth(rtspURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	if timeout == 0 {
		timeout = 10 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		url:              rtspURL,
		host:             host,
		port:             port,
		timeout:          timeout,
		cseq:             0,
		ctx:              ctx,
		cancel:           cancel,
		clientPorts:      []int{50000, 50001},
		username:         username,
		password:         password,
		sessionTimeout:   60 * time.Second, // Default 60s
		transportMode:    TransportModeTCP, // Use TCP interleaved mode for better multi-track support
		rtpChannel:       0,
		rtcpChannel:      1,
		trackIndex:       0,
		aggregateControl: "",
		sdpInfo:          nil,
	}, nil
}

// SetCredentials sets authentication credentials
func (c *Client) SetCredentials(username, password string) {
	c.username = username
	c.password = password
	c.authChallenge = nil
	c.resetDigestState("")
}

// hasCredentials checks if credentials are available
func (c *Client) hasCredentials() bool {
	return c.username != "" && c.password != ""
}

// GetSession returns the current session ID
func (c *Client) GetSession() string {
	return c.session
}

// Connect establishes connection to the RTSP server
func (c *Client) Connect() error {
	address := net.JoinHostPort(c.host, c.port)
	conn, err := net.DialTimeout("tcp", address, c.timeout)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrConnectionFailed, err)
	}

	c.conn = conn
	return nil
}

// Describe sends DESCRIBE request and returns SDP content
func (c *Client) Describe() (string, error) {
	c.cseq++
	request := c.buildRequestWithAuth("DESCRIBE", c.url, c.cseq, "")

	if err := c.sendRequest(request); err != nil {
		return "", err
	}

	statusCode, headers, body, err := c.readResponse()
	if err != nil {
		return "", err
	}

	// Handle 401 Unauthorized - retry with authentication
	if statusCode == 401 && c.hasCredentials() {
		return c.retryWithAuth("DESCRIBE", headers)
	}

	if statusCode != 200 {
		return "", fmt.Errorf("%w: status code %d", ErrRequestFailed, statusCode)
	}

	contentBase := headers["Content-Base"]
	info := parseSDPInfo(body, contentBase, c.url)
	if info != nil {
		c.sdpInfo = info
		c.aggregateControl = info.AggregateControl
		c.trackIndex = 0
	}

	return body, nil
}

// Setup sends SETUP request and establishes RTP/RTCP connections
func (c *Client) Setup() error {
	c.cseq++
	setupURL := c.nextSetupURL()
	
	// Calculate channel numbers for this track (each track gets 2 channels: RTP and RTCP)
	channelBase := uint8(c.trackIndex * 2)
	c.rtpChannel = channelBase
	c.rtcpChannel = channelBase + 1
	
	var request string
	if c.transportMode == TransportModeTCP {
		request = buildRequestWithTCPTransportAndChannels("SETUP", setupURL, c.cseq, "", c.rtpChannel, c.rtcpChannel)
		// Add authentication if needed
		if c.authChallenge != nil && c.hasCredentials() {
			authHeader := c.generateAuthHeader("SETUP", setupURL)
			if authHeader != "" {
				request = strings.TrimSuffix(request, "\r\n")
				request += fmt.Sprintf("Authorization: %s\r\n\r\n", authHeader)
			}
		}
	} else {
		request = c.buildRequestWithAuth("SETUP", setupURL, c.cseq, "")
	}

	if err := c.sendRequest(request); err != nil {
		return err
	}

	statusCode, headers, _, err := c.readResponse()
	if err != nil {
		return err
	}

	// Handle 401 Unauthorized - retry with authentication
	if statusCode == 401 && c.hasCredentials() {
		_, retryHeaders, _, err := c.retryRequestWithAuth("SETUP", setupURL, headers)
		if err != nil {
			return err
		}
		headers = retryHeaders
		statusCode = 200 // If retry succeeded, update status
	}

	if statusCode != 200 {
		return fmt.Errorf("%w: status code %d", ErrRequestFailed, statusCode)
	}

	// Extract session ID and timeout
	if sessionHeader, ok := headers["Session"]; ok {
		c.session, c.sessionTimeout = parseSessionTimeout(sessionHeader)
	}

	// Extract transport information
	if transport, ok := headers["Transport"]; ok {
		transportInfo := ParseTransportHeader(transport)
		if transportInfo.IsTCP || c.transportMode == TransportModeTCP {
			c.transportMode = TransportModeTCP
			if transportInfo.RTPChannel != 0 || transportInfo.RTCPChannel != 0 {
				c.rtpChannel = transportInfo.RTPChannel
				c.rtcpChannel = transportInfo.RTCPChannel
			}
		} else {
			c.serverPorts = extractServerPorts(transport)
		}
	}

	// Setup RTP and RTCP listeners when using UDP
	if c.transportMode != TransportModeTCP {
		if err := c.setupRTPConnection(); err != nil {
			return err
		}
	}

	if c.sdpInfo != nil && len(c.sdpInfo.Tracks) > 0 && c.trackIndex < len(c.sdpInfo.Tracks)-1 {
		c.trackIndex++
	}

	return nil
}

// Play sends PLAY request to start streaming
func (c *Client) Play() error {
	c.cseq++
	playURL := c.sessionControlURL()
	request := c.buildRequestWithAuth("PLAY", playURL, c.cseq, c.session)

	if err := c.sendRequest(request); err != nil {
		return err
	}

	statusCode, headers, _, err := c.readResponse()
	if err != nil {
		return err
	}

	// Handle 401 Unauthorized - retry with authentication
	if statusCode == 401 && c.hasCredentials() {
		statusCode, _, _, err = c.retryRequestWithAuth("PLAY", playURL, headers)
		if err != nil {
			return err
		}
	}

	if statusCode != 200 {
		return fmt.Errorf("%w: status code %d", ErrRequestFailed, statusCode)
	}

	return nil
}

// ReadPacket reads an RTP packet from the stream
func (c *Client) ReadPacket() (*rtp.Packet, error) {
	if c.transportMode == TransportModeTCP {
		if c.conn == nil {
			return nil, fmt.Errorf("not connected")
		}

		if err := c.conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
			return nil, err
		}

		for {
			frame, err := c.ReadInterleavedPacket()
			if err != nil {
				return nil, err
			}

			channel := frame.Channel
			// Accept RTP packets from any track (even-numbered channels: 0, 2, 4, ...)
			// RTCP packets are on odd-numbered channels: 1, 3, 5, ...
			if channel%2 == 0 {
				// This is an RTP channel
				packet, err := rtp.ParsePacket(frame.Payload)
				if err != nil {
					return nil, fmt.Errorf("failed to parse RTP packet: %w", err)
				}
				c.validatePayloadType(packet)
				return packet, nil
			}

			// Skip RTCP frames (odd channels)
			continue
		}
	}

	if c.rtpConn == nil {
		return nil, errors.New("RTP connection not established")
	}

	buffer := make([]byte, 2048)
	c.rtpConn.SetReadDeadline(time.Now().Add(c.timeout))

	n, err := c.rtpConn.Read(buffer)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return nil, err
		}
		return nil, err
	}

	packet, err := rtp.ParsePacket(buffer[:n])
	if err != nil {
		return nil, fmt.Errorf("failed to parse RTP packet: %w", err)
	}

	c.validatePayloadType(packet)

	return packet, nil
}

// Teardown sends TEARDOWN request to stop streaming
func (c *Client) Teardown() error {
	c.cseq++
	controlURL := c.sessionControlURL()
	request := buildRequest("TEARDOWN", controlURL, c.cseq, c.session)

	if err := c.sendRequest(request); err != nil {
		return err
	}

	statusCode, _, _, err := c.readResponse()
	if err != nil {
		return err
	}

	if statusCode != 200 {
		return fmt.Errorf("%w: status code %d", ErrRequestFailed, statusCode)
	}

	return nil
}

// Close closes all connections
// GetNumTracks returns the number of tracks in the SDP
func (c *Client) GetNumTracks() int {
	if c.sdpInfo != nil {
		return len(c.sdpInfo.Tracks)
	}
	return 0
}

func (c *Client) Close() error {
	// Stop keep-alive first
	c.StopKeepAlive()

	if c.cancel != nil {
		c.cancel()
	}

	var errs []error

	if c.rtpConn != nil {
		if err := c.rtpConn.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if c.rtcpConn != nil {
		if err := c.rtcpConn.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing connections: %v", errs)
	}

	return nil
}

// Helper functions

func parseURL(rtspURL string) (string, string, error) {
	if rtspURL == "" {
		return "", "", errors.New("empty URL")
	}

	u, err := url.Parse(rtspURL)
	if err != nil {
		return "", "", err
	}

	if u.Scheme != "rtsp" {
		return "", "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "554" // Default RTSP port
	}

	return host, port, nil
}

func buildRequest(method, url string, cseq int, session string) string {
	request := fmt.Sprintf("%s %s RTSP/1.0\r\n", method, url)
	request += fmt.Sprintf("CSeq: %d\r\n", cseq)

	switch method {
	case "DESCRIBE":
		request += "Accept: application/sdp\r\n"
	case "SETUP":
		request += "Transport: RTP/AVP;unicast;client_port=50000-50001\r\n"
	case "PLAY":
		request += fmt.Sprintf("Session: %s\r\n", session)
		request += "Range: npt=0.000-\r\n"
	case "TEARDOWN":
		request += fmt.Sprintf("Session: %s\r\n", session)
	case "OPTIONS":
		if session != "" {
			request += fmt.Sprintf("Session: %s\r\n", session)
		}
	case "GET_PARAMETER":
		if session != "" {
			request += fmt.Sprintf("Session: %s\r\n", session)
		}
	}

	request += "User-Agent: RTSP-Client/1.0\r\n"
	request += "\r\n"

	return request
}

func (c *Client) sendRequest(request string) error {
	c.conn.SetWriteDeadline(time.Now().Add(c.timeout))
	_, err := c.conn.Write([]byte(request))
	return err
}

func (c *Client) readResponse() (int, map[string]string, string, error) {
	c.conn.SetReadDeadline(time.Now().Add(c.timeout))

	reader := bufio.NewReader(c.conn)

	// Read status line
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return 0, nil, "", err
	}

	response := statusLine

	// Read headers
	headers := make(map[string]string)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return 0, nil, "", err
		}

		response += line
		line = strings.TrimSpace(line)

		if line == "" {
			break
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			headers[key] = value
		}
	}

	// Read body if Content-Length is present
	body := ""
	if contentLength, ok := headers["Content-Length"]; ok {
		length, err := strconv.Atoi(contentLength)
		if err == nil && length > 0 {
			bodyBytes := make([]byte, length)
			_, err := io.ReadFull(reader, bodyBytes)
			if err != nil {
				return 0, nil, "", err
			}
			body = string(bodyBytes)
		}
	}

	return parseResponse(response + body)
}

func (c *Client) validatePayloadType(packet *rtp.Packet) {
	if !c.payloadTypeInit {
		c.expectedPayloadType = packet.PayloadType
		c.payloadTypeInit = true
		fmt.Printf("📺 Stream payload type: %d\n", packet.PayloadType)
		return
	}

	if packet.PayloadType != c.expectedPayloadType {
		fmt.Printf("⚠️  Payload type changed: %d → %d (codec may have changed)\n",
			c.expectedPayloadType, packet.PayloadType)
		c.expectedPayloadType = packet.PayloadType
	}
}

func parseResponse(response string) (int, map[string]string, string, error) {
	lines := strings.Split(response, "\r\n")
	if len(lines) < 1 {
		return 0, nil, "", ErrInvalidResponse
	}

	// Parse status line: RTSP/1.0 200 OK
	statusParts := strings.Fields(lines[0])
	if len(statusParts) < 2 {
		return 0, nil, "", ErrInvalidResponse
	}

	statusCode, err := strconv.Atoi(statusParts[1])
	if err != nil {
		return 0, nil, "", fmt.Errorf("%w: invalid status code", ErrInvalidResponse)
	}

	// Parse headers
	headers := make(map[string]string)
	bodyStartIdx := -1
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			bodyStartIdx = i + 1
			break
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			headers[key] = value
		}
	}

	// Extract body
	body := ""
	if bodyStartIdx > 0 && bodyStartIdx < len(lines) {
		body = strings.Join(lines[bodyStartIdx:], "\r\n")
	}

	return statusCode, headers, body, nil
}

func (c *Client) setupRTPConnection() error {
	// Listen on client ports for RTP
	rtpAddr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: c.clientPorts[0],
	}

	rtpConn, err := net.ListenUDP("udp", rtpAddr)
	if err != nil {
		return fmt.Errorf("failed to setup RTP listener: %w", err)
	}

	c.rtpConn = rtpConn

	// Listen on client ports for RTCP
	rtcpAddr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: c.clientPorts[1],
	}

	rtcpConn, err := net.ListenUDP("udp", rtcpAddr)
	if err != nil {
		c.rtpConn.Close()
		return fmt.Errorf("failed to setup RTCP listener: %w", err)
	}

	c.rtcpConn = rtcpConn

	return nil
}

func extractServerPorts(transport string) []int {
	// Example: RTP/AVP;unicast;client_port=50000-50001;server_port=60000-60001
	parts := strings.Split(transport, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "server_port=") {
			portRange := strings.TrimPrefix(part, "server_port=")
			portParts := strings.Split(portRange, "-")
			if len(portParts) == 2 {
				rtpPort, err1 := strconv.Atoi(portParts[0])
				rtcpPort, err2 := strconv.Atoi(portParts[1])
				if err1 == nil && err2 == nil {
					return []int{rtpPort, rtcpPort}
				}
			}
		}
	}
	return []int{}
}

func (c *Client) nextSetupURL() string {
	if c.sdpInfo != nil && len(c.sdpInfo.Tracks) > 0 {
		idx := c.trackIndex
		if idx >= len(c.sdpInfo.Tracks) {
			idx = len(c.sdpInfo.Tracks) - 1
		}
		if idx >= 0 {
			if control := c.sdpInfo.Tracks[idx].ControlURL; control != "" {
				return control
			}
		}
	}

	return c.url
}

func (c *Client) sessionControlURL() string {
	// If we have an explicit aggregate control URL, use it
	if c.aggregateControl != "" {
		return c.aggregateControl
	}

	// Otherwise, always use the original request URL for session-level commands (PLAY, TEARDOWN)
	// Track control URLs are only for SETUP
	return c.url
}

func parseSDPInfo(sdp, contentBase, requestURL string) *SDPInfo {
	scanner := bufio.NewScanner(strings.NewReader(sdp))
	info := &SDPInfo{Tracks: make([]SDPTrack, 0)}

	var current *SDPTrack
	currentPayloadType := -1

	flushCurrent := func() {
		if current == nil {
			return
		}
		if current.ControlURL != "" {
			info.Tracks = append(info.Tracks, *current)
		}
		current = nil
		currentPayloadType = -1
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "a=control:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "a=control:"))
			if value == "" {
				continue
			}
			if value == "*" {
				info.AggregateControl = resolveControlURL(contentBase, requestURL, value)
				continue
			}
			if current == nil {
				info.AggregateControl = resolveControlURL(contentBase, requestURL, value)
				continue
			}
			current.ControlURL = resolveControlURL(contentBase, requestURL, value)
			continue
		}

		if strings.HasPrefix(line, "m=") {
			flushCurrent()
			parts := strings.Fields(strings.TrimPrefix(line, "m="))
			if len(parts) < 4 {
				continue
			}
			media := strings.ToLower(parts[0])
			pt, err := strconv.Atoi(parts[3])
			if err != nil {
				pt = -1
			}
			current = &SDPTrack{
				Media:       media,
				PayloadType: pt,
				FMTP:        make(map[string]string),
			}
			currentPayloadType = pt
			continue
		}

		if current == nil {
			continue
		}

		if strings.HasPrefix(line, "a=rtpmap:") {
			payloadAndCodec := strings.TrimPrefix(line, "a=rtpmap:")
			parts := strings.SplitN(payloadAndCodec, " ", 2)
			if len(parts) == 2 {
				pt, err := strconv.Atoi(parts[0])
				if err == nil && (currentPayloadType == -1 || pt == currentPayloadType) {
					codecParts := strings.Split(parts[1], "/")
					current.Codec = codecParts[0]
					if len(codecParts) > 1 {
						if clock, err := strconv.Atoi(codecParts[1]); err == nil {
							current.ClockRate = clock
						}
					}
					if len(codecParts) > 2 {
						if ch, err := strconv.Atoi(codecParts[2]); err == nil {
							current.Channels = ch
						}
					}
				}
			}
			continue
		}

		if strings.HasPrefix(line, "a=fmtp:") {
			fmtpLine := strings.TrimPrefix(line, "a=fmtp:")
			parts := strings.SplitN(fmtpLine, " ", 2)
			if len(parts) == 2 {
				pt, err := strconv.Atoi(parts[0])
				if err == nil && (currentPayloadType == -1 || pt == currentPayloadType) {
					pairs := strings.Split(parts[1], ";")
					for _, pair := range pairs {
						pair = strings.TrimSpace(pair)
						if pair == "" {
							continue
						}
						kv := strings.SplitN(pair, "=", 2)
						if len(kv) == 2 {
							current.FMTP[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
						}
					}
				}
			}
			continue
		}
	}

	flushCurrent()

	if info.AggregateControl != "" {
		info.AggregateControl = resolveControlURL(contentBase, requestURL, info.AggregateControl)
	}

	if len(info.Tracks) == 0 && info.AggregateControl == "" {
		return nil
	}

	return info
}

func resolveControlURL(contentBase, requestURL, control string) string {
	lowerControl := strings.ToLower(control)
	if control == "*" {
		// For aggregate control (*), use the original request URL without modification
		// Adding a trailing slash can cause path mismatches with some servers (e.g., MediaMTX)
		if contentBase != "" {
			return contentBase
		}
		if requestURL != "" {
			return requestURL
		}
		return ""
	}
	if strings.HasPrefix(lowerControl, "rtsp://") || strings.HasPrefix(lowerControl, "rtspu://") {
		return control
	}

	base := contentBase
	if base == "" {
		base = requestURL
	}

	if base == "" {
		return control
	}

	// Only add trailing slash for relative path resolution (track control URLs)
	base = ensureTrailingSlash(base)

	baseURL, err := url.Parse(base)
	if err != nil {
		return control
	}

	ref, err := url.Parse(control)
	if err != nil {
		return control
	}

	resolved := baseURL.ResolveReference(ref)
	return resolved.String()
}

func ensureTrailingSlash(uri string) string {
	if uri == "" {
		return uri
	}

	if strings.HasSuffix(uri, "/") {
		return uri
	}

	return uri + "/"
}

// buildRequestWithAuth builds RTSP request with authentication if needed
func (c *Client) buildRequestWithAuth(method, url string, cseq int, session string) string {
	var request string
	if method == "SETUP" && c.transportMode == TransportModeTCP {
		request = buildRequestWithTCPTransport(method, url, cseq, session)
	} else {
		request = buildRequest(method, url, cseq, session)
	}

	// Add authentication header if we have credentials and challenge
	if c.authChallenge != nil && c.hasCredentials() {
		authHeader := c.generateAuthHeader(method, url)
		if authHeader != "" {
			// Insert auth header before the final \r\n
			request = strings.TrimSuffix(request, "\r\n")
			request += fmt.Sprintf("Authorization: %s\r\n\r\n", authHeader)
		}
	}

	return request
}

// generateAuthHeader generates appropriate authentication header
func (c *Client) generateAuthHeader(method, uri string) string {
	if c.authChallenge == nil || !c.hasCredentials() {
		return ""
	}

	if c.authChallenge.AuthType == "Basic" {
		return generateBasicAuthHeader(c.username, c.password)
	} else if c.authChallenge.AuthType == "Digest" {
		header, err := c.generateDigestAuthHeader(c.authChallenge, c.username, c.password, uri, method)
		if err != nil {
			return ""
		}
		return header
	}

	return ""
}

func (c *Client) generateDigestAuthHeader(challenge *AuthChallenge, username, password, uri, method string) (string, error) {
	qop := selectQOP(challenge.Qop)

	if qop != "" {
		if c.lastNonce != challenge.Nonce {
			c.resetDigestState(challenge.Nonce)
		}

		if c.clientNonce == "" {
			c.clientNonce = generateClientNonce()
		}

		c.authNonceCount++
		nc := fmt.Sprintf("%08x", c.authNonceCount)
		response := computeDigestResponse(username, password, challenge.Realm, challenge.Nonce, uri, method, qop, nc, c.clientNonce)
		return buildDigestHeader(challenge, username, uri, response, qop, nc, c.clientNonce), nil
	}

	c.lastNonce = challenge.Nonce
	response := computeDigestResponse(username, password, challenge.Realm, challenge.Nonce, uri, method, "", "", "")
	return buildDigestHeader(challenge, username, uri, response, "", "", ""), nil
}

func (c *Client) resetDigestState(nonce string) {
	c.authNonceCount = 0
	c.clientNonce = ""
	c.lastNonce = nonce
}

// retryWithAuth retries DESCRIBE request with authentication
func (c *Client) retryWithAuth(method string, headers map[string]string) (string, error) {
	// Parse authentication challenge from WWW-Authenticate header
	wwwAuth, ok := headers["WWW-Authenticate"]
	if !ok {
		wwwAuth, ok = headers["Www-Authenticate"]
	}
	if !ok {
		return "", fmt.Errorf("missing WWW-Authenticate header in 401 response")
	}

	// Build fake response for parsing
	fakeResponse := fmt.Sprintf("RTSP/1.0 401 Unauthorized\r\nWWW-Authenticate: %s\r\n\r\n", wwwAuth)
	challenge, err := parseAuthChallenge(fakeResponse)
	if err != nil {
		return "", fmt.Errorf("failed to parse auth challenge: %w", err)
	}

	c.authChallenge = challenge
	c.resetDigestState(challenge.Nonce)

	// Retry request with authentication
	c.cseq++
	request := c.buildRequestWithAuth(method, c.url, c.cseq, "")

	if err := c.sendRequest(request); err != nil {
		return "", err
	}

	statusCode, respHeaders, body, err := c.readResponse()
	if err != nil {
		return "", err
	}

	if statusCode != 200 {
		return "", fmt.Errorf("%w: status code %d after authentication", ErrRequestFailed, statusCode)
	}

	contentBase := respHeaders["Content-Base"]
	info := parseSDPInfo(body, contentBase, c.url)
	if info != nil {
		c.sdpInfo = info
		c.aggregateControl = info.AggregateControl
		c.trackIndex = 0
	}

	return body, nil
}

// retryRequestWithAuth retries any request with authentication
func (c *Client) retryRequestWithAuth(method, requestURL string, headers map[string]string) (int, map[string]string, string, error) {
	// Parse authentication challenge
	wwwAuth, ok := headers["WWW-Authenticate"]
	if !ok {
		wwwAuth, ok = headers["Www-Authenticate"]
	}
	if !ok {
		return 0, nil, "", fmt.Errorf("missing WWW-Authenticate header in 401 response")
	}

	fakeResponse := fmt.Sprintf("RTSP/1.0 401 Unauthorized\r\nWWW-Authenticate: %s\r\n\r\n", wwwAuth)
	challenge, err := parseAuthChallenge(fakeResponse)
	if err != nil {
		return 0, nil, "", fmt.Errorf("failed to parse auth challenge: %w", err)
	}

	c.authChallenge = challenge
	c.resetDigestState(challenge.Nonce)

	// Retry request
	c.cseq++
	request := c.buildRequestWithAuth(method, requestURL, c.cseq, c.session)

	if err := c.sendRequest(request); err != nil {
		return 0, nil, "", err
	}

	statusCode, newHeaders, body, err := c.readResponse()
	if err != nil {
		return 0, nil, "", err
	}

	if statusCode != 200 {
		return statusCode, newHeaders, body, fmt.Errorf("%w: status code %d after authentication", ErrRequestFailed, statusCode)
	}

	return statusCode, newHeaders, body, nil
}
