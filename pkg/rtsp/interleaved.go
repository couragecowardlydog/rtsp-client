package rtsp

import (
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// TransportMode defines the transport mode for RTSP
type TransportMode int

const (
	// TransportModeUDP uses UDP for RTP/RTCP
	TransportModeUDP TransportMode = iota
	// TransportModeTCP uses TCP interleaved mode for RTP/RTCP
	TransportModeTCP
)

// InterleavedFrame represents a $ prefixed RTP/RTCP frame
type InterleavedFrame struct {
	Channel uint8  // Channel ID (0 = RTP, 1 = RTCP typically)
	Length  uint16 // Payload length
	Payload []byte // RTP or RTCP data
}

// TransportInfo contains parsed transport header information
type TransportInfo struct {
	IsTCP       bool
	RTPChannel  uint8
	RTCPChannel uint8
	ClientPorts []int
	ServerPorts []int
}

// RTPHandler handles RTP frames
type RTPHandler func(payload []byte)

// RTCPHandler handles RTCP frames
type RTCPHandler func(payload []byte)

// InterleavedReader reads interleaved frames from a stream
type InterleavedReader struct {
	reader io.Reader
}

// ChannelDemux demultiplexes interleaved channels to RTP/RTCP handlers
type ChannelDemux struct {
	rtpHandler  RTPHandler
	rtcpHandler RTCPHandler
	rtpChannel  uint8
	rtcpChannel uint8
}

// NewInterleavedReader creates a new interleaved frame reader
func NewInterleavedReader(reader io.Reader) *InterleavedReader {
	return &InterleavedReader{
		reader: reader,
	}
}

// ReadFrame reads a single interleaved frame
func (r *InterleavedReader) ReadFrame() (*InterleavedFrame, error) {
	// Read header: $ + channel (1 byte) + length (2 bytes big-endian)
	header := make([]byte, 4)
	_, err := io.ReadFull(r.reader, header)
	if err != nil {
		return nil, err
	}

	// Check for $ prefix
	if header[0] != '$' {
		return nil, fmt.Errorf("invalid interleaved frame: missing $ prefix")
	}

	channel := header[1]
	length := binary.BigEndian.Uint16(header[2:4])

	// Read payload
	payload := make([]byte, length)
	if length > 0 {
		_, err = io.ReadFull(r.reader, payload)
		if err != nil {
			return nil, fmt.Errorf("failed to read payload: %w", err)
		}
	}

	return &InterleavedFrame{
		Channel: channel,
		Length:  length,
		Payload: payload,
	}, nil
}

// ParseInterleavedFrame parses an interleaved frame from raw bytes
func ParseInterleavedFrame(data []byte) (*InterleavedFrame, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("frame too short: need at least 4 bytes")
	}

	// Check for $ prefix
	if data[0] != '$' {
		return nil, fmt.Errorf("invalid interleaved frame: missing $ prefix")
	}

	channel := data[1]
	length := binary.BigEndian.Uint16(data[2:4])

	// Verify payload length
	if len(data) < int(4+length) {
		return nil, fmt.Errorf("truncated payload: expected %d bytes, got %d", length, len(data)-4)
	}

	payload := data[4 : 4+length]

	return &InterleavedFrame{
		Channel: channel,
		Length:  length,
		Payload: payload,
	}, nil
}

// BuildInterleavedFrame builds an interleaved frame
func BuildInterleavedFrame(channel uint8, payload []byte) []byte {
	length := uint16(len(payload))
	frame := make([]byte, 4+length)

	frame[0] = '$'
	frame[1] = channel
	binary.BigEndian.PutUint16(frame[2:4], length)
	copy(frame[4:], payload)

	return frame
}

// NewChannelDemux creates a new channel demultiplexer
func NewChannelDemux() *ChannelDemux {
	return &ChannelDemux{
		rtpChannel:  0, // Default RTP channel
		rtcpChannel: 1, // Default RTCP channel
	}
}

// SetRTPHandler sets the RTP frame handler
func (d *ChannelDemux) SetRTPHandler(handler RTPHandler) {
	d.rtpHandler = handler
}

// SetRTCPHandler sets the RTCP frame handler
func (d *ChannelDemux) SetRTCPHandler(handler RTCPHandler) {
	d.rtcpHandler = handler
}

// SetChannels sets the RTP and RTCP channel IDs
func (d *ChannelDemux) SetChannels(rtpChannel, rtcpChannel uint8) {
	d.rtpChannel = rtpChannel
	d.rtcpChannel = rtcpChannel
}

// HandleFrame handles an interleaved frame
func (d *ChannelDemux) HandleFrame(frame *InterleavedFrame) {
	if frame.Channel == d.rtpChannel && d.rtpHandler != nil {
		d.rtpHandler(frame.Payload)
	} else if frame.Channel == d.rtcpChannel && d.rtcpHandler != nil {
		d.rtcpHandler(frame.Payload)
	}
}

// SetTransportMode sets the transport mode for the client
func (c *Client) SetTransportMode(mode TransportMode) {
	c.transportMode = mode
}

// GetTransportMode returns the current transport mode
func (c *Client) GetTransportMode() TransportMode {
	return c.transportMode
}

// buildRequestWithTCPTransport builds RTSP SETUP request with TCP transport
func buildRequestWithTCPTransport(method, url string, cseq int, session string) string {
	return buildRequestWithTCPTransportAndChannels(method, url, cseq, session, 0, 1)
}

func buildRequestWithTCPTransportAndChannels(method, url string, cseq int, session string, rtpChannel, rtcpChannel uint8) string {
	request := fmt.Sprintf("%s %s RTSP/1.0\r\n", method, url)
	request += fmt.Sprintf("CSeq: %d\r\n", cseq)

	if method == "SETUP" {
		// TCP interleaved mode with specified channels
		request += fmt.Sprintf("Transport: RTP/AVP/TCP;unicast;interleaved=%d-%d\r\n", rtpChannel, rtcpChannel)
	} else if session != "" {
		request += fmt.Sprintf("Session: %s\r\n", session)
	}

	request += "User-Agent: RTSP-Client/1.0\r\n"
	request += "\r\n"

	return request
}

// ParseTransportHeader parses the Transport header from RTSP response
func ParseTransportHeader(transport string) *TransportInfo {
	info := &TransportInfo{}

	// Check if TCP transport
	if strings.Contains(transport, "RTP/AVP/TCP") || strings.Contains(transport, "interleaved") {
		info.IsTCP = true
	}

	// Parse interleaved channels
	parts := strings.Split(transport, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)

		if strings.HasPrefix(part, "interleaved=") {
			channelStr := strings.TrimPrefix(part, "interleaved=")
			channels := strings.Split(channelStr, "-")

			if len(channels) >= 1 {
				if ch, err := strconv.Atoi(channels[0]); err == nil {
					info.RTPChannel = uint8(ch)
				}
			}
			if len(channels) >= 2 {
				if ch, err := strconv.Atoi(channels[1]); err == nil {
					info.RTCPChannel = uint8(ch)
				}
			}
		}

		if strings.HasPrefix(part, "client_port=") {
			portRange := strings.TrimPrefix(part, "client_port=")
			portParts := strings.Split(portRange, "-")
			if len(portParts) == 2 {
				p1, _ := strconv.Atoi(portParts[0])
				p2, _ := strconv.Atoi(portParts[1])
				info.ClientPorts = []int{p1, p2}
			}
		}

		if strings.HasPrefix(part, "server_port=") {
			portRange := strings.TrimPrefix(part, "server_port=")
			portParts := strings.Split(portRange, "-")
			if len(portParts) == 2 {
				p1, _ := strconv.Atoi(portParts[0])
				p2, _ := strconv.Atoi(portParts[1])
				info.ServerPorts = []int{p1, p2}
			}
		}
	}

	return info
}

// ReadInterleavedPacket reads an interleaved RTP/RTCP packet from TCP connection
func (c *Client) ReadInterleavedPacket() (*InterleavedFrame, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	reader := NewInterleavedReader(c.conn)
	return reader.ReadFrame()
}
