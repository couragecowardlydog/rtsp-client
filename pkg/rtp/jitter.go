package rtp

import (
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	// ErrBufferFull indicates the jitter buffer is full
	ErrBufferFull = errors.New("jitter buffer is full")
	// ErrBufferEmpty indicates the jitter buffer is empty
	ErrBufferEmpty = errors.New("jitter buffer is empty")
	// ErrDuplicatePacket indicates a duplicate packet was detected
	ErrDuplicatePacket = errors.New("duplicate packet detected")
	// ErrPacketNotReady indicates packet is not ready for retrieval
	ErrPacketNotReady = errors.New("packet not ready (jitter delay)")
)

// BufferedPacket wraps RTP packet with arrival time
type BufferedPacket struct {
	Packet      *Packet
	ArrivalTime time.Time
}

// JitterBuffer manages packet reordering and jitter handling
type JitterBuffer struct {
	mu               sync.RWMutex
	packets          map[uint16]*BufferedPacket
	maxSize          int
	maxDelay         time.Duration
	expectedSeq      uint16
	initialized      bool
	packetsReceived  int
	packetsLost      int
	packetsDuplicate int
	lastTimestamp    uint32
	jitterSum        float64
	jitterSamples    int
}

// BufferStatistics contains jitter buffer statistics
type BufferStatistics struct {
	PacketsReceived  int
	PacketsLost      int
	PacketsDuplicate int
	JitterMs         float64
	BufferSize       int
}

// NewJitterBuffer creates a new jitter buffer
func NewJitterBuffer(bufferSize int, maxDelay time.Duration) *JitterBuffer {
	return &JitterBuffer{
		packets:     make(map[uint16]*BufferedPacket),
		maxSize:     bufferSize,
		maxDelay:    maxDelay,
		initialized: false,
	}
}

// AddPacket adds a packet to the jitter buffer
func (jb *JitterBuffer) AddPacket(packet *Packet) error {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	// Initialize expected sequence on first packet
	if !jb.initialized {
		jb.expectedSeq = packet.SequenceNumber
		jb.initialized = true
		jb.lastTimestamp = packet.Timestamp
	}

	// Check for duplicate
	if _, exists := jb.packets[packet.SequenceNumber]; exists {
		jb.packetsDuplicate++
		return ErrDuplicatePacket
	}

	// Check buffer capacity
	if len(jb.packets) >= jb.maxSize {
		// Drop oldest packet to make room
		jb.dropOldestPacket()
	}

	// Add packet to buffer
	jb.packets[packet.SequenceNumber] = &BufferedPacket{
		Packet:      packet,
		ArrivalTime: time.Now(),
	}

	jb.packetsReceived++

	// Calculate jitter (simplified RFC 3550 algorithm)
	jb.calculateJitter(packet)

	return nil
}

// GetNextPacket retrieves the next packet in sequence
func (jb *JitterBuffer) GetNextPacket() (*Packet, error) {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	if len(jb.packets) == 0 {
		return nil, ErrBufferEmpty
	}

	// If this is the first retrieval, adjust expectedSeq to minimum available
	if !jb.initialized || jb.packetsReceived == len(jb.packets) {
		minSeq := jb.findNextAvailableSequence()
		if minSeq != 0 && sequenceCompare(minSeq, jb.expectedSeq) < 0 {
			jb.expectedSeq = minSeq
		}
	}

	// Look for expected sequence number
	bufferedPacket, exists := jb.packets[jb.expectedSeq]
	if !exists {
		// Expected packet not arrived yet
		// Try to find next available packet (handle loss)
		nextSeq := jb.findNextAvailableSequence()
		if nextSeq == 0 && len(jb.packets) > 0 {
			// Wait for expected packet
			return nil, ErrPacketNotReady
		}

		// Update expected sequence to skip lost packets
		if nextSeq != 0 {
			lostCount := int(sequenceDiff(jb.expectedSeq, nextSeq))
			jb.packetsLost += lostCount
			jb.expectedSeq = nextSeq
			bufferedPacket = jb.packets[nextSeq]
		}
	}

	if bufferedPacket == nil {
		return nil, ErrBufferEmpty
	}

	// Check if packet has been buffered long enough (jitter delay)
	// Only apply delay if we have multiple packets in buffer
	if len(jb.packets) > 1 {
		elapsed := time.Since(bufferedPacket.ArrivalTime)
		minDelay := jb.maxDelay / 4 // Use 1/4 of max delay as minimum
		if elapsed < minDelay {
			// Packet arrived too recently, wait for jitter delay
			return nil, ErrPacketNotReady
		}
	}

	// Remove packet from buffer
	packet := bufferedPacket.Packet
	delete(jb.packets, jb.expectedSeq)

	// Increment expected sequence number
	jb.expectedSeq++

	return packet, nil
}

// DetectGaps detects missing sequence numbers (packet loss)
func (jb *JitterBuffer) DetectGaps() []uint16 {
	jb.mu.RLock()
	defer jb.mu.RUnlock()

	if len(jb.packets) == 0 {
		return nil
	}

	// Get all sequence numbers and sort
	sequences := make([]uint16, 0, len(jb.packets))
	for seq := range jb.packets {
		sequences = append(sequences, seq)
	}

	sort.Slice(sequences, func(i, j int) bool {
		return sequenceCompare(sequences[i], sequences[j]) < 0
	})

	// Find gaps
	gaps := []uint16{}
	for i := 0; i < len(sequences)-1; i++ {
		current := sequences[i]
		next := sequences[i+1]

		// Check for gaps between consecutive packets
		diff := sequenceDiff(current, next)
		if diff > 1 {
			// There's a gap
			for j := uint16(1); j < diff; j++ {
				gaps = append(gaps, current+j)
			}
		}
	}

	return gaps
}

// Size returns current buffer size
func (jb *JitterBuffer) Size() int {
	jb.mu.RLock()
	defer jb.mu.RUnlock()
	return len(jb.packets)
}

// Reset clears the buffer
func (jb *JitterBuffer) Reset() {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	jb.packets = make(map[uint16]*BufferedPacket)
	jb.initialized = false
	jb.packetsReceived = 0
	jb.packetsLost = 0
	jb.packetsDuplicate = 0
	jb.jitterSum = 0
	jb.jitterSamples = 0
}

// GetStatistics returns buffer statistics
func (jb *JitterBuffer) GetStatistics() BufferStatistics {
	jb.mu.RLock()
	defer jb.mu.RUnlock()

	avgJitter := 0.0
	if jb.jitterSamples > 0 {
		avgJitter = jb.jitterSum / float64(jb.jitterSamples)
	}

	return BufferStatistics{
		PacketsReceived:  jb.packetsReceived,
		PacketsLost:      jb.packetsLost,
		PacketsDuplicate: jb.packetsDuplicate,
		JitterMs:         avgJitter,
		BufferSize:       len(jb.packets),
	}
}

// Helper functions

// dropOldestPacket removes the packet with the oldest sequence number
func (jb *JitterBuffer) dropOldestPacket() {
	if len(jb.packets) == 0 {
		return
	}

	var oldestSeq uint16
	var oldestTime time.Time
	first := true

	for seq, bp := range jb.packets {
		if first || bp.ArrivalTime.Before(oldestTime) {
			oldestSeq = seq
			oldestTime = bp.ArrivalTime
			first = false
		}
	}

	delete(jb.packets, oldestSeq)
}

// findNextAvailableSequence finds the next available packet sequence
func (jb *JitterBuffer) findNextAvailableSequence() uint16 {
	if len(jb.packets) == 0 {
		return 0
	}

	var minSeq uint16
	first := true

	for seq := range jb.packets {
		if first || sequenceCompare(seq, minSeq) < 0 {
			minSeq = seq
			first = false
		}
	}

	return minSeq
}

// calculateJitter calculates inter-arrival jitter (RFC 3550)
func (jb *JitterBuffer) calculateJitter(packet *Packet) {
	// Simplified jitter calculation based on timestamp difference
	if jb.lastTimestamp != 0 {
		// Calculate timestamp delta (in RTP timestamp units)
		timestampDelta := int64(packet.Timestamp) - int64(jb.lastTimestamp)
		if timestampDelta < 0 {
			timestampDelta = -timestampDelta
		}

		// Convert to milliseconds (assuming 90kHz for H.264)
		jitterMs := float64(timestampDelta) / 90.0

		jb.jitterSum += jitterMs
		jb.jitterSamples++
	}

	jb.lastTimestamp = packet.Timestamp
}

// sequenceCompare compares two sequence numbers with wraparound handling
// Returns: -1 if seq1 < seq2, 0 if seq1 == seq2, 1 if seq1 > seq2
func sequenceCompare(seq1, seq2 uint16) int {
	if seq1 == seq2 {
		return 0
	}

	// Handle wraparound using RFC 1982 serial number arithmetic
	diff := int32(seq1) - int32(seq2)

	// If difference is greater than half the sequence space,
	// assume wraparound occurred
	if diff > 32768 {
		return -1
	} else if diff < -32768 {
		return 1
	}

	if diff < 0 {
		return -1
	}
	return 1
}

// sequenceDiff calculates difference between two sequence numbers
func sequenceDiff(seq1, seq2 uint16) uint16 {
	// Handle wraparound
	if seq2 >= seq1 {
		return seq2 - seq1
	}
	// Wraparound case
	return (65535 - seq1) + seq2 + 1
}

// isSequenceNewer checks if seq is newer than expected
func isSequenceNewer(seq, expected uint16) bool {
	return sequenceCompare(seq, expected) > 0
}

// calculatePacketLossRate calculates packet loss rate as percentage
func calculatePacketLossRate(received, lost int) float64 {
	total := received + lost
	if total == 0 {
		return 0.0
	}
	return (float64(lost) / float64(total)) * 100.0
}
