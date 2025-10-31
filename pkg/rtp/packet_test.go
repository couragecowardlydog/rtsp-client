package rtp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePacket(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		expected    *Packet
		expectError bool
	}{
		{
			name: "valid RTP packet with H.264 payload",
			data: []byte{
				0x80, 0x60, // V=2, P=0, X=0, CC=0, M=0, PT=96
				0x00, 0x01, // Sequence number: 1
				0x00, 0x00, 0x03, 0xe8, // Timestamp: 1000
				0x12, 0x34, 0x56, 0x78, // SSRC
				0x01, 0x02, 0x03, // Payload
			},
			expected: &Packet{
				Version:        2,
				Padding:        false,
				Extension:      false,
				Marker:         false,
				PayloadType:    96,
				SequenceNumber: 1,
				Timestamp:      1000,
				SSRC:           0x12345678,
				Payload:        []byte{0x01, 0x02, 0x03},
			},
			expectError: false,
		},
		{
			name: "valid RTP packet with marker bit set",
			data: []byte{
				0x80, 0xe0, // V=2, P=0, X=0, CC=0, M=1, PT=96
				0x00, 0x02, // Sequence number: 2
				0x00, 0x00, 0x07, 0xd0, // Timestamp: 2000
				0xaa, 0xbb, 0xcc, 0xdd, // SSRC
				0xaa, 0xbb, // Payload
			},
			expected: &Packet{
				Version:        2,
				Padding:        false,
				Extension:      false,
				Marker:         true,
				PayloadType:    96,
				SequenceNumber: 2,
				Timestamp:      2000,
				SSRC:           0xaabbccdd,
				Payload:        []byte{0xaa, 0xbb},
			},
			expectError: false,
		},
		{
			name:        "packet too short",
			data:        []byte{0x80, 0x60, 0x00},
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid version",
			data:        []byte{0x40, 0x60, 0x00, 0x01, 0x00, 0x00, 0x03, 0xe8, 0x12, 0x34, 0x56, 0x78},
			expected:    nil,
			expectError: true,
		},
		{
			name:        "empty packet",
			data:        []byte{},
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet, err := ParsePacket(tt.data)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, packet)
			} else {
				require.NoError(t, err)
				require.NotNil(t, packet)
				assert.Equal(t, tt.expected.Version, packet.Version)
				assert.Equal(t, tt.expected.Padding, packet.Padding)
				assert.Equal(t, tt.expected.Extension, packet.Extension)
				assert.Equal(t, tt.expected.Marker, packet.Marker)
				assert.Equal(t, tt.expected.PayloadType, packet.PayloadType)
				assert.Equal(t, tt.expected.SequenceNumber, packet.SequenceNumber)
				assert.Equal(t, tt.expected.Timestamp, packet.Timestamp)
				assert.Equal(t, tt.expected.SSRC, packet.SSRC)
				assert.Equal(t, tt.expected.Payload, packet.Payload)
			}
		})
	}
}

func TestPacket_IsKeyFrame(t *testing.T) {
	tests := []struct {
		name     string
		packet   *Packet
		expected bool
	}{
		{
			name: "H.264 IDR frame (NAL type 5)",
			packet: &Packet{
				PayloadType: 96,
				Payload:     []byte{0x65, 0x01, 0x02}, // NAL type 5 (IDR)
			},
			expected: true,
		},
		{
			name: "H.264 non-IDR frame (NAL type 1)",
			packet: &Packet{
				PayloadType: 96,
				Payload:     []byte{0x41, 0x01, 0x02}, // NAL type 1
			},
			expected: false,
		},
		{
			name: "empty payload",
			packet: &Packet{
				PayloadType: 96,
				Payload:     []byte{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.packet.IsKeyFrame()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPacket_GetTimestampString(t *testing.T) {
	tests := []struct {
		name      string
		timestamp uint32
		expected  string
	}{
		{
			name:      "timestamp 1000",
			timestamp: 1000,
			expected:  "1000",
		},
		{
			name:      "timestamp 0",
			timestamp: 0,
			expected:  "0",
		},
		{
			name:      "large timestamp",
			timestamp: 4294967295,
			expected:  "4294967295",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := &Packet{Timestamp: tt.timestamp}
			result := packet.GetTimestampString()
			assert.Equal(t, tt.expected, result)
		})
	}
}
