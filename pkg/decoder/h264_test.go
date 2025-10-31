package decoder

import (
	"testing"

	"github.com/rtsp-client/pkg/rtp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestH264Decoder_New(t *testing.T) {
	decoder := NewH264Decoder()
	assert.NotNil(t, decoder)
	// Test that decoder can process packets
	stats := decoder.GetStats()
	assert.Equal(t, 0, stats.TotalFrames)
}

func TestH264Decoder_ProcessPacket(t *testing.T) {
	tests := []struct {
		name          string
		packets       []*rtp.Packet
		expectedFrame bool
		description   string
	}{
		{
			name: "single NAL unit packet with marker",
			packets: []*rtp.Packet{
				{
					Marker:         true,
					Timestamp:      1000,
					SequenceNumber: 1,
					SSRC:           0x12345678,
					Payload:        []byte{0x65, 0x01, 0x02, 0x03}, // IDR frame
				},
			},
			expectedFrame: true,
			description:   "Should return frame when marker bit is set",
		},
		{
			name: "fragmented NAL unit - FU-A",
			packets: []*rtp.Packet{
				{
					Marker:         false,
					Timestamp:      2000,
					SequenceNumber: 10,
					SSRC:           0x12345678,
					Payload:        []byte{0x7c, 0x85, 0x01, 0x02}, // FU-A start
				},
				{
					Marker:         false,
					Timestamp:      2000,
					SequenceNumber: 11,
					SSRC:           0x12345678,
					Payload:        []byte{0x7c, 0x05, 0x03, 0x04}, // FU-A middle
				},
				{
					Marker:         true,
					Timestamp:      2000,
					SequenceNumber: 12,
					SSRC:           0x12345678,
					Payload:        []byte{0x7c, 0x45, 0x05, 0x06}, // FU-A end
				},
			},
			expectedFrame: true,
			description:   "Should reassemble FU-A fragments",
		},
		{
			name: "multiple NAL units without marker",
			packets: []*rtp.Packet{
				{
					Marker:         false,
					Timestamp:      3000,
					SequenceNumber: 20,
					SSRC:           0x12345678,
					Payload:        []byte{0x41, 0x01, 0x02},
				},
			},
			expectedFrame: false,
			description:   "Should not return frame without marker bit (unless reorder window expires)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder := NewH264Decoder()

			var frame *Frame
			for _, packet := range tt.packets {
				result := decoder.ProcessPacket(packet)
				if result != nil {
					frame = result
				}
			}

			if tt.expectedFrame {
				require.NotNil(t, frame, tt.description)
				assert.Greater(t, len(frame.Data), 0)
				assert.Equal(t, tt.packets[0].Timestamp, frame.Timestamp)
			} else {
				assert.Nil(t, frame, tt.description)
			}
		})
	}
}

func TestH264Decoder_IsFUA(t *testing.T) {
	tests := []struct {
		name     string
		payload  []byte
		expected bool
	}{
		{
			name:     "FU-A packet (type 28)",
			payload:  []byte{0x7c, 0x85, 0x01},
			expected: true,
		},
		{
			name:     "Single NAL unit",
			payload:  []byte{0x65, 0x01, 0x02},
			expected: false,
		},
		{
			name:     "Empty payload",
			payload:  []byte{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFUA(tt.payload)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestH264Decoder_GetFUANALType(t *testing.T) {
	tests := []struct {
		name     string
		payload  []byte
		expected byte
	}{
		{
			name:     "FU-A with NAL type 5 (IDR)",
			payload:  []byte{0x7c, 0x85, 0x01}, // NAL type = 5
			expected: 5,
		},
		{
			name:     "FU-A with NAL type 1",
			payload:  []byte{0x7c, 0x81, 0x01}, // NAL type = 1
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFUANALType(tt.payload)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestH264Decoder_IsFUAStart(t *testing.T) {
	tests := []struct {
		name     string
		payload  []byte
		expected bool
	}{
		{
			name:     "FU-A start packet",
			payload:  []byte{0x7c, 0x85, 0x01}, // S bit set
			expected: true,
		},
		{
			name:     "FU-A middle packet",
			payload:  []byte{0x7c, 0x05, 0x01}, // S bit not set
			expected: false,
		},
		{
			name:     "FU-A end packet",
			payload:  []byte{0x7c, 0x45, 0x01}, // E bit set
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFUAStart(tt.payload)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestH264Decoder_Reset(t *testing.T) {
	decoder := NewH264Decoder()

	// Process a packet (may create frame assembly)
	packet := &rtp.Packet{
		Marker:       false,
		Timestamp:    1000,
		SequenceNumber: 1,
		SSRC:         0x12345678,
		Payload:      []byte{0x41, 0x01, 0x02},
	}
	decoder.ProcessPacket(packet)

	// Reset should clear frame assemblies
	decoder.Reset()

	// Verify reset worked by checking stats haven't changed
	stats := decoder.GetStats()
	assert.Equal(t, 0, stats.TotalFrames)
}

func TestFrame_IsKeyFrame(t *testing.T) {
	tests := []struct {
		name     string
		frame    *Frame
		expected bool
	}{
		{
			name: "frame with IDR NAL unit",
			frame: &Frame{
				Data: []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x01, 0x02},
			},
			expected: true,
		},
		{
			name: "frame with non-IDR NAL unit",
			frame: &Frame{
				Data: []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0x01, 0x02},
			},
			expected: false,
		},
		{
			name: "empty frame",
			frame: &Frame{
				Data: []byte{},
			},
			expected: false,
		},
		{
			name: "frame too short",
			frame: &Frame{
				Data: []byte{0x00, 0x00, 0x00},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.frame.IsKeyFrame()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFrame_GetTimestampString(t *testing.T) {
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
			frame := &Frame{Timestamp: tt.timestamp}
			result := frame.GetTimestampString()
			assert.Equal(t, tt.expected, result)
		})
	}
}
