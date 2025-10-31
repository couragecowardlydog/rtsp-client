package rtp

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	// ErrPacketTooShort indicates the packet is too short to be valid
	ErrPacketTooShort = errors.New("packet too short")
	// ErrInvalidVersion indicates the RTP version is not 2
	ErrInvalidVersion = errors.New("invalid RTP version, expected 2")
)

// Packet represents an RTP packet
type Packet struct {
	Version        uint8
	Padding        bool
	Extension      bool
	Marker         bool
	PayloadType    uint8
	SequenceNumber uint16
	Timestamp      uint32
	SSRC           uint32
	Payload        []byte
}

// ParsePacket parses raw bytes into an RTP packet
func ParsePacket(data []byte) (*Packet, error) {
	if len(data) < 12 {
		return nil, ErrPacketTooShort
	}

	packet := &Packet{}

	// Parse first byte: V(2), P(1), X(1), CC(4)
	packet.Version = (data[0] >> 6) & 0x03
	if packet.Version != 2 {
		return nil, ErrInvalidVersion
	}

	packet.Padding = (data[0]>>5)&0x01 == 1
	packet.Extension = (data[0]>>4)&0x01 == 1
	csrcCount := data[0] & 0x0F

	// Parse second byte: M(1), PT(7)
	packet.Marker = (data[1]>>7)&0x01 == 1
	packet.PayloadType = data[1] & 0x7F

	// Parse sequence number (2 bytes)
	packet.SequenceNumber = binary.BigEndian.Uint16(data[2:4])

	// Parse timestamp (4 bytes)
	packet.Timestamp = binary.BigEndian.Uint32(data[4:8])

	// Parse SSRC (4 bytes)
	packet.SSRC = binary.BigEndian.Uint32(data[8:12])

	// Calculate header size
	headerSize := 12 + int(csrcCount)*4

	if len(data) < headerSize {
		return nil, ErrPacketTooShort
	}

	// Handle extension if present
	if packet.Extension {
		if len(data) < headerSize+4 {
			return nil, ErrPacketTooShort
		}
		extensionLength := binary.BigEndian.Uint16(data[headerSize+2:headerSize+4]) * 4
		headerSize += 4 + int(extensionLength)
	}

	if len(data) < headerSize {
		return nil, ErrPacketTooShort
	}

	// Extract payload
	payload := data[headerSize:]

	// Handle padding if present
	if packet.Padding && len(payload) > 0 {
		paddingLength := int(payload[len(payload)-1])
		if paddingLength <= len(payload) {
			payload = payload[:len(payload)-paddingLength]
		}
	}

	packet.Payload = payload

	return packet, nil
}

// IsKeyFrame checks if the packet contains a keyframe (IDR frame for H.264)
// H.264 NAL unit type 5 indicates an IDR (Instantaneous Decoder Refresh) frame
func (p *Packet) IsKeyFrame() bool {
	if len(p.Payload) == 0 {
		return false
	}

	// Extract NAL unit type from first 5 bits
	nalType := p.Payload[0] & 0x1F

	// NAL type 5 is IDR frame (keyframe)
	return nalType == 5
}

// GetTimestampString returns the timestamp as a string for filename
func (p *Packet) GetTimestampString() string {
	return fmt.Sprintf("%d", p.Timestamp)
}

// String returns a string representation of the packet for debugging
func (p *Packet) String() string {
	return fmt.Sprintf(
		"RTP Packet [Version: %d, PT: %d, Seq: %d, TS: %d, SSRC: 0x%x, Marker: %t, Payload: %d bytes]",
		p.Version, p.PayloadType, p.SequenceNumber, p.Timestamp, p.SSRC, p.Marker, len(p.Payload),
	)
}
