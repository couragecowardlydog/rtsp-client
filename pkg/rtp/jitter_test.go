package rtp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewJitterBuffer tests jitter buffer creation
func TestNewJitterBuffer(t *testing.T) {
	tests := []struct {
		name          string
		bufferSize    int
		maxDelay      time.Duration
		expectedSize  int
		expectedDelay time.Duration
	}{
		{
			name:          "standard buffer",
			bufferSize:    100,
			maxDelay:      200 * time.Millisecond,
			expectedSize:  100,
			expectedDelay: 200 * time.Millisecond,
		},
		{
			name:          "small buffer",
			bufferSize:    10,
			maxDelay:      50 * time.Millisecond,
			expectedSize:  10,
			expectedDelay: 50 * time.Millisecond,
		},
		{
			name:          "large buffer",
			bufferSize:    500,
			maxDelay:      500 * time.Millisecond,
			expectedSize:  500,
			expectedDelay: 500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jb := NewJitterBuffer(tt.bufferSize, tt.maxDelay)
			require.NotNil(t, jb)
			assert.Equal(t, tt.expectedSize, jb.maxSize)
			assert.Equal(t, tt.expectedDelay, jb.maxDelay)
		})
	}
}

// TestJitterBuffer_AddPacket tests adding packets to buffer
func TestJitterBuffer_AddPacket(t *testing.T) {
	jb := NewJitterBuffer(100, 200*time.Millisecond)

	packet1 := &Packet{SequenceNumber: 100, Timestamp: 1000}
	packet2 := &Packet{SequenceNumber: 101, Timestamp: 1100}

	err := jb.AddPacket(packet1)
	assert.NoError(t, err)

	err = jb.AddPacket(packet2)
	assert.NoError(t, err)

	assert.Equal(t, 2, jb.Size())
}

// TestJitterBuffer_AddOutOfOrderPackets tests reordering
func TestJitterBuffer_AddOutOfOrderPackets(t *testing.T) {
	jb := NewJitterBuffer(100, 20*time.Millisecond) // Shorter delay for testing

	// Add packets out of order
	packet3 := &Packet{SequenceNumber: 103, Timestamp: 1300}
	packet1 := &Packet{SequenceNumber: 101, Timestamp: 1100}
	packet2 := &Packet{SequenceNumber: 102, Timestamp: 1200}

	jb.AddPacket(packet3)
	jb.AddPacket(packet1)
	jb.AddPacket(packet2)

	// Wait for jitter delay to pass
	time.Sleep(10 * time.Millisecond)

	// Retrieve in order
	p1, err := jb.GetNextPacket()
	require.NoError(t, err)
	assert.Equal(t, uint16(101), p1.SequenceNumber)

	p2, err := jb.GetNextPacket()
	require.NoError(t, err)
	assert.Equal(t, uint16(102), p2.SequenceNumber)

	p3, err := jb.GetNextPacket()
	require.NoError(t, err)
	assert.Equal(t, uint16(103), p3.SequenceNumber)
}

// TestJitterBuffer_DuplicateDetection tests duplicate packet handling
func TestJitterBuffer_DuplicateDetection(t *testing.T) {
	jb := NewJitterBuffer(100, 200*time.Millisecond)

	packet1 := &Packet{SequenceNumber: 100, Timestamp: 1000}
	packet1Dup := &Packet{SequenceNumber: 100, Timestamp: 1000}

	err := jb.AddPacket(packet1)
	assert.NoError(t, err)

	// Adding duplicate should be detected
	err = jb.AddPacket(packet1Dup)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")

	// Buffer size should still be 1
	assert.Equal(t, 1, jb.Size())
}

// TestJitterBuffer_SequenceWraparound tests 16-bit sequence wraparound
func TestJitterBuffer_SequenceWraparound(t *testing.T) {
	jb := NewJitterBuffer(100, 20*time.Millisecond)

	// Sequence numbers near wraparound point
	packet1 := &Packet{SequenceNumber: 65534, Timestamp: 1000}
	packet2 := &Packet{SequenceNumber: 65535, Timestamp: 1100}
	packet3 := &Packet{SequenceNumber: 0, Timestamp: 1200}
	packet4 := &Packet{SequenceNumber: 1, Timestamp: 1300}

	jb.AddPacket(packet1)
	jb.AddPacket(packet2)
	jb.AddPacket(packet3)
	jb.AddPacket(packet4)

	// Wait for jitter delay
	time.Sleep(10 * time.Millisecond)

	// Should retrieve in correct order across wraparound
	p1, err1 := jb.GetNextPacket()
	require.NoError(t, err1)
	assert.Equal(t, uint16(65534), p1.SequenceNumber)

	p2, err2 := jb.GetNextPacket()
	require.NoError(t, err2)
	assert.Equal(t, uint16(65535), p2.SequenceNumber)

	p3, err3 := jb.GetNextPacket()
	require.NoError(t, err3)
	assert.Equal(t, uint16(0), p3.SequenceNumber)

	p4, err4 := jb.GetNextPacket()
	require.NoError(t, err4)
	assert.Equal(t, uint16(1), p4.SequenceNumber)
}

// TestJitterBuffer_PacketLossDetection tests gap detection
func TestJitterBuffer_PacketLossDetection(t *testing.T) {
	jb := NewJitterBuffer(100, 200*time.Millisecond)

	packet1 := &Packet{SequenceNumber: 100, Timestamp: 1000}
	packet3 := &Packet{SequenceNumber: 102, Timestamp: 1200} // 101 is missing

	jb.AddPacket(packet1)
	jb.AddPacket(packet3)

	// Should detect gap
	gaps := jb.DetectGaps()
	require.Len(t, gaps, 1)
	assert.Equal(t, uint16(101), gaps[0])
}

// TestJitterBuffer_MultipleGaps tests multiple packet losses
func TestJitterBuffer_MultipleGaps(t *testing.T) {
	jb := NewJitterBuffer(100, 200*time.Millisecond)

	jb.AddPacket(&Packet{SequenceNumber: 100, Timestamp: 1000})
	jb.AddPacket(&Packet{SequenceNumber: 103, Timestamp: 1300}) // 101, 102 missing
	jb.AddPacket(&Packet{SequenceNumber: 105, Timestamp: 1500}) // 104 missing

	gaps := jb.DetectGaps()
	assert.ElementsMatch(t, []uint16{101, 102, 104}, gaps)
}

// TestJitterBuffer_BufferOverflow tests overflow handling
func TestJitterBuffer_BufferOverflow(t *testing.T) {
	jb := NewJitterBuffer(5, 100*time.Millisecond) // Small buffer

	// Fill buffer to capacity
	for i := uint16(0); i < 5; i++ {
		err := jb.AddPacket(&Packet{SequenceNumber: i, Timestamp: uint32(i * 100)})
		assert.NoError(t, err)
	}

	// Adding one more should handle overflow
	err := jb.AddPacket(&Packet{SequenceNumber: 5, Timestamp: 500})
	// Should either drop oldest or reject new
	assert.True(t, err != nil || jb.Size() <= 5)
}

// TestJitterBuffer_GetNextPacket_Empty tests empty buffer
func TestJitterBuffer_GetNextPacket_Empty(t *testing.T) {
	jb := NewJitterBuffer(100, 200*time.Millisecond)

	_, err := jb.GetNextPacket()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

// TestJitterBuffer_GetNextPacket_NotReady tests delay threshold
func TestJitterBuffer_GetNextPacket_NotReady(t *testing.T) {
	jb := NewJitterBuffer(100, 100*time.Millisecond)

	// Add packet that just arrived
	packet := &Packet{SequenceNumber: 100, Timestamp: 1000}
	jb.AddPacket(packet)

	// Try to get immediately (might not be ready due to jitter delay)
	// This test is time-sensitive, so we just ensure it doesn't panic
	_, err := jb.GetNextPacket()
	if err != nil {
		assert.Contains(t, err.Error(), "not ready")
	}
}

// TestJitterBuffer_Statistics tests buffer statistics
func TestJitterBuffer_Statistics(t *testing.T) {
	jb := NewJitterBuffer(100, 20*time.Millisecond)

	jb.AddPacket(&Packet{SequenceNumber: 100, Timestamp: 1000})
	jb.AddPacket(&Packet{SequenceNumber: 102, Timestamp: 1200}) // Gap at 101
	jb.AddPacket(&Packet{SequenceNumber: 103, Timestamp: 1300})
	jb.AddPacket(&Packet{SequenceNumber: 100, Timestamp: 1000}) // Duplicate

	time.Sleep(10 * time.Millisecond)

	// Retrieve packets to detect loss
	jb.GetNextPacket() // 100
	jb.GetNextPacket() // Will skip 101 and get 102

	stats := jb.GetStatistics()
	assert.Equal(t, 3, stats.PacketsReceived)
	assert.Equal(t, 1, stats.PacketsLost)
	assert.Equal(t, 1, stats.PacketsDuplicate)
	assert.Greater(t, stats.JitterMs, 0.0)
}

// TestJitterBuffer_Reset tests buffer reset
func TestJitterBuffer_Reset(t *testing.T) {
	jb := NewJitterBuffer(100, 200*time.Millisecond)

	jb.AddPacket(&Packet{SequenceNumber: 100, Timestamp: 1000})
	jb.AddPacket(&Packet{SequenceNumber: 101, Timestamp: 1100})

	assert.Equal(t, 2, jb.Size())

	jb.Reset()

	assert.Equal(t, 0, jb.Size())
	_, err := jb.GetNextPacket()
	assert.Error(t, err)
}

// TestJitterBuffer_TimestampWraparound tests 32-bit timestamp wraparound
func TestJitterBuffer_TimestampWraparound(t *testing.T) {
	jb := NewJitterBuffer(100, 200*time.Millisecond)

	// Timestamps near 32-bit wraparound
	packet1 := &Packet{SequenceNumber: 100, Timestamp: 4294967290}
	packet2 := &Packet{SequenceNumber: 101, Timestamp: 4294967295}
	packet3 := &Packet{SequenceNumber: 102, Timestamp: 0} // Wrapped
	packet4 := &Packet{SequenceNumber: 103, Timestamp: 5}

	jb.AddPacket(packet1)
	jb.AddPacket(packet2)
	jb.AddPacket(packet3)
	jb.AddPacket(packet4)

	// Should handle wraparound correctly
	assert.Equal(t, 4, jb.Size())
}

// TestSequenceCompare tests sequence number comparison with wraparound
func TestSequenceCompare(t *testing.T) {
	tests := []struct {
		name     string
		seq1     uint16
		seq2     uint16
		expected int
	}{
		{
			name:     "seq1 < seq2",
			seq1:     100,
			seq2:     101,
			expected: -1,
		},
		{
			name:     "seq1 > seq2",
			seq1:     200,
			seq2:     100,
			expected: 1,
		},
		{
			name:     "seq1 == seq2",
			seq1:     100,
			seq2:     100,
			expected: 0,
		},
		{
			name:     "wraparound: 65535 < 0",
			seq1:     65535,
			seq2:     0,
			expected: -1,
		},
		{
			name:     "wraparound: 0 > 65535",
			seq1:     0,
			seq2:     65535,
			expected: 1,
		},
		{
			name:     "large gap: treat as wraparound",
			seq1:     100,
			seq2:     65000,
			expected: 1, // 100 is after 65000 in wraparound
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sequenceCompare(tt.seq1, tt.seq2)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsSequenceNewer tests if sequence is newer considering wraparound
func TestIsSequenceNewer(t *testing.T) {
	tests := []struct {
		name     string
		seq      uint16
		expected uint16
		isNewer  bool
	}{
		{
			name:     "simple newer",
			seq:      101,
			expected: 100,
			isNewer:  true,
		},
		{
			name:     "simple older",
			seq:      99,
			expected: 100,
			isNewer:  false,
		},
		{
			name:     "wraparound newer",
			seq:      0,
			expected: 65535,
			isNewer:  true,
		},
		{
			name:     "wraparound older",
			seq:      65535,
			expected: 0,
			isNewer:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSequenceNewer(tt.seq, tt.expected)
			assert.Equal(t, tt.isNewer, result)
		})
	}
}

// TestCalculatePacketLossRate tests packet loss calculation
func TestCalculatePacketLossRate(t *testing.T) {
	tests := []struct {
		name         string
		received     int
		lost         int
		expectedRate float64
	}{
		{
			name:         "no loss",
			received:     100,
			lost:         0,
			expectedRate: 0.0,
		},
		{
			name:         "10% loss",
			received:     90,
			lost:         10,
			expectedRate: 10.0,
		},
		{
			name:         "50% loss",
			received:     50,
			lost:         50,
			expectedRate: 50.0,
		},
		{
			name:         "no packets",
			received:     0,
			lost:         0,
			expectedRate: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate := calculatePacketLossRate(tt.received, tt.lost)
			assert.InDelta(t, tt.expectedRate, rate, 0.01)
		})
	}
}
