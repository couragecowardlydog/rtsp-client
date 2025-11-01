package decoder

import (
	"fmt"
	"sort"
	"time"

	"github.com/rtsp-client/pkg/logger"
	"github.com/rtsp-client/pkg/rtp"
)

const (
	// H.264 NAL unit types
	nalUnitTypeIDR  = 5  // IDR picture (keyframe)
	nalUnitTypeSPS  = 7  // Sequence Parameter Set
	nalUnitTypePPS  = 8  // Picture Parameter Set
	nalUnitTypeSTAP = 24 // Single-Time Aggregation Packet A
	nalUnitTypeFUA  = 28 // Fragmentation Unit A

	// H.264 start code
	startCodeSize = 4

	// Reordering window - wait up to 50ms for late packets
	reorderWindowMs = 50
)

var (
	// H.264 Annex B start code
	startCode = []byte{0x00, 0x00, 0x00, 0x01}
)

// Frame represents a complete video frame
type Frame struct {
	Data        []byte
	Timestamp   uint32
	IsKey       bool
	IsCorrupted bool
}

// FrameAssembly represents packets belonging to a single frame (same timestamp)
type FrameAssembly struct {
	timestamp      uint32
	packets        map[uint16]*rtp.Packet // Map by sequence number
	seqMin         uint16
	seqMax         uint16
	markerReceived bool
	firstArrival   time.Time
	hasPacketLoss  bool
}

// H264Decoder decodes H.264 RTP packets into complete frames
type H264Decoder struct {
	// Per-timestamp frame assembly
	currentFrame *FrameAssembly
	frameMap     map[uint32]*FrameAssembly // Maps timestamp to frame assembly

	// Sequence tracking
	lastSequence       uint16
	expectedSequence   uint16
	sequenceInit       bool

	// SSRC tracking
	currentSSRC        uint32
	ssrcInitialized    bool

	// SPS/PPS caching
	spsNAL             []byte
	ppsNAL             []byte

	// Statistics
	stats              DecoderStats

	// Options
	dropCorruptedFrames bool // If true, don't return corrupted frames
}

// DecoderStats tracks decoder statistics
type DecoderStats struct {
	TotalFrames      int
	CorruptedFrames  int
	PacketLossEvents int
	SSRCChanges      int
}

// NewH264Decoder creates a new H.264 decoder
func NewH264Decoder() *H264Decoder {
	return &H264Decoder{
		frameMap: make(map[uint32]*FrameAssembly),
	}
}

// SetDropCorruptedFrames sets whether to drop corrupted frames instead of returning them
func (d *H264Decoder) SetDropCorruptedFrames(drop bool) {
	d.dropCorruptedFrames = drop
}

// ProcessPacket processes an RTP packet and returns a frame if complete
func (d *H264Decoder) ProcessPacket(packet *rtp.Packet) *Frame {
	if packet == nil || len(packet.Payload) == 0 {
		return nil
	}

	logger.Debug("[H264Decoder:ProcessPacket] Processing packet: seq=%d, timestamp=%d, marker=%t, payload=%d bytes", 
		packet.SequenceNumber, packet.Timestamp, packet.Marker, len(packet.Payload))

	// Initialize SSRC tracking on first packet
	if !d.ssrcInitialized {
		d.currentSSRC = packet.SSRC
		d.ssrcInitialized = true
	}

	// Detect SSRC change (stream changed or camera rebooted)
	if packet.SSRC != d.currentSSRC {
		logger.Warn("[H264Decoder] SSRC changed: 0x%x → 0x%x (stream changed or camera rebooted)",
			d.currentSSRC, packet.SSRC)

		// Reset decoder state for new stream
		d.Reset()
		d.currentSSRC = packet.SSRC
		d.sequenceInit = false // Re-initialize sequence tracking
		d.stats.SSRCChanges++
		// Clear SPS/PPS on SSRC change - need new ones for new stream
		d.spsNAL = nil
		d.ppsNAL = nil
	}

	// Initialize sequence tracking on first packet
	if !d.sequenceInit {
		d.expectedSequence = packet.SequenceNumber
		d.lastSequence = packet.SequenceNumber
		d.sequenceInit = true
	}

	// Detect packet loss by checking sequence number gap
	if d.detectPacketLoss(packet.SequenceNumber) {
		d.stats.PacketLossEvents++
	}

	// Update sequence tracking
	d.lastSequence = packet.SequenceNumber
	d.expectedSequence = packet.SequenceNumber + 1

	// Get or create frame assembly for this timestamp
	frameAssembly := d.getOrCreateFrameAssembly(packet.Timestamp)

	// Add packet to frame assembly
	frameAssembly.packets[packet.SequenceNumber] = packet

	// Update frame assembly metadata
	if len(frameAssembly.packets) == 1 {
		// First packet for this frame
		frameAssembly.firstArrival = time.Now()
		frameAssembly.seqMin = packet.SequenceNumber
		frameAssembly.seqMax = packet.SequenceNumber
	} else {
		// Update sequence range
		if sequenceBefore(packet.SequenceNumber, frameAssembly.seqMin) {
			frameAssembly.seqMin = packet.SequenceNumber
		}
		if sequenceAfter(packet.SequenceNumber, frameAssembly.seqMax) {
			frameAssembly.seqMax = packet.SequenceNumber
		}
	}

	// Update marker bit status
	if packet.Marker {
		frameAssembly.markerReceived = true
	}

	// Check for packet loss within this frame
	d.checkFramePacketLoss(frameAssembly)

	// Try to finalize old frames (check frames other than current)
	var completedFrame *Frame
		for ts, fa := range d.frameMap {
			if ts != packet.Timestamp {
				if d.shouldFinalizeFrame(fa) {
					logger.Debug("[H264Decoder:ProcessPacket] Old frame should be finalized: timestamp=%d, packets=%d, marker=%t", 
						ts, len(fa.packets), fa.markerReceived)
					if frame := d.finalizeFrame(fa); frame != nil {
						logger.Debug("[H264Decoder:ProcessPacket] Old frame finalized: timestamp=%d, size=%d bytes", 
							frame.Timestamp, len(frame.Data))
						// Another frame completed, return it
						delete(d.frameMap, ts)
						return frame
					} else {
						logger.Warn("[H264Decoder:ProcessPacket] Old frame finalization returned nil: timestamp=%d", ts)
					}
					delete(d.frameMap, ts)
				}
			}
		}

		// Check if current frame should be finalized
		if d.shouldFinalizeFrame(frameAssembly) {
			logger.Debug("[H264Decoder:ProcessPacket] Frame should be finalized: timestamp=%d, packets=%d, marker=%t", 
				packet.Timestamp, len(frameAssembly.packets), frameAssembly.markerReceived)
			completedFrame = d.finalizeFrame(frameAssembly)
			if completedFrame != nil {
				logger.Debug("[H264Decoder:ProcessPacket] Frame finalized: timestamp=%d, size=%d bytes, isKey=%t, corrupted=%t", 
					completedFrame.Timestamp, len(completedFrame.Data), completedFrame.IsKey, completedFrame.IsCorrupted)
			} else {
				logger.Warn("[H264Decoder:ProcessPacket] Frame finalization returned nil: timestamp=%d, packets=%d", 
					packet.Timestamp, len(frameAssembly.packets))
			}
		delete(d.frameMap, packet.Timestamp)
		return completedFrame
	}

	return nil
}

// getOrCreateFrameAssembly gets or creates a frame assembly for a timestamp
func (d *H264Decoder) getOrCreateFrameAssembly(timestamp uint32) *FrameAssembly {
	if fa, ok := d.frameMap[timestamp]; ok {
		return fa
	}

	fa := &FrameAssembly{
		timestamp:    timestamp,
		packets:      make(map[uint16]*rtp.Packet),
		firstArrival: time.Now(),
	}
	d.frameMap[timestamp] = fa
	return fa
}

// shouldFinalizeFrame determines if a frame should be finalized
func (d *H264Decoder) shouldFinalizeFrame(fa *FrameAssembly) bool {
	// If marker bit received, frame is complete
	if fa.markerReceived {
		return true
	}

	// If reordering window expired, finalize even without marker
	elapsed := time.Since(fa.firstArrival)
	if elapsed > reorderWindowMs*time.Millisecond {
		return true
	}

	return false
}

// checkFramePacketLoss checks for missing sequence numbers within a frame
func (d *H264Decoder) checkFramePacketLoss(fa *FrameAssembly) {
	// Check for gaps in sequence numbers
	if len(fa.packets) < 2 {
		return
	}

	// Get sorted sequence numbers
	seqs := make([]uint16, 0, len(fa.packets))
	for seq := range fa.packets {
		seqs = append(seqs, seq)
	}
	sort.Slice(seqs, func(i, j int) bool {
		return sequenceBefore(seqs[i], seqs[j])
	})

	// Check for gaps
	for i := 1; i < len(seqs); i++ {
		expectedSeq := seqs[i-1] + 1
		if seqs[i] != expectedSeq {
			gap := sequenceDifference(expectedSeq, seqs[i])
			if gap > 0 && gap < 100 {
				// Packet loss detected
				fa.hasPacketLoss = true
				return
			}
		}
	}
}

// finalizeFrame reassembles packets into a complete frame
func (d *H264Decoder) finalizeFrame(fa *FrameAssembly) *Frame {
	if len(fa.packets) == 0 {
		return nil
	}

	// Get sorted sequence numbers for ordered processing
	seqs := make([]uint16, 0, len(fa.packets))
	for seq := range fa.packets {
		seqs = append(seqs, seq)
	}
	sort.Slice(seqs, func(i, j int) bool {
		return sequenceBefore(seqs[i], seqs[j])
	})

	// Reassemble frame
	var frameData []byte
	var hasIDR bool
	var isCorrupted = fa.hasPacketLoss

	// Track FU-A fragmentation state
	var fuaState struct {
		active    bool
		nalHeader byte
		started   bool
		ended     bool
	}

	for _, seq := range seqs {
		packet := fa.packets[seq]
		payload := packet.Payload

		if len(payload) == 0 {
			continue
		}

		// Check NAL unit type
		nalType := payload[0] & 0x1F

		switch nalType {
		case nalUnitTypeSTAP:
			// STAP-A: Multiple NAL units in one packet
			nalUnits := d.unpackSTAPA(payload)
			for _, nal := range nalUnits {
				// Extract and cache SPS/PPS
				extractedType := nal[0] & 0x1F
				if extractedType == nalUnitTypeSPS {
					d.spsNAL = append([]byte{}, startCode...)
					d.spsNAL = append(d.spsNAL, nal...)
				} else if extractedType == nalUnitTypePPS {
					d.ppsNAL = append([]byte{}, startCode...)
					d.ppsNAL = append(d.ppsNAL, nal...)
				} else if extractedType == nalUnitTypeIDR {
					hasIDR = true
				}

				frameData = append(frameData, startCode...)
				frameData = append(frameData, nal...)
			}

		case nalUnitTypeFUA:
			// FU-A fragmentation
			if len(payload) < 2 {
				isCorrupted = true
				continue
			}

			fuHeader := payload[1]
			isStart := (fuHeader >> 7) & 0x01
			isEnd := (fuHeader >> 6) & 0x01

			if isStart == 1 {
				// Start of FU-A
				if fuaState.active {
					// Previous FU-A not ended, corruption
					isCorrupted = true
				}
				fuaState.active = true
				fuaState.started = true
				fuaState.ended = false

				// Reconstruct NAL header
				extractedType := fuHeader & 0x1F
				fnri := payload[0] & 0xE0
				fuaState.nalHeader = fnri | extractedType

				// Extract and cache SPS/PPS
				if extractedType == nalUnitTypeSPS {
					d.spsNAL = append([]byte{}, startCode...)
					d.spsNAL = append(d.spsNAL, fuaState.nalHeader)
				} else if extractedType == nalUnitTypePPS {
					d.ppsNAL = append([]byte{}, startCode...)
					d.ppsNAL = append(d.ppsNAL, fuaState.nalHeader)
				} else if extractedType == nalUnitTypeIDR {
					hasIDR = true
				}

				// Add start code and NAL header
				frameData = append(frameData, startCode...)
				frameData = append(frameData, fuaState.nalHeader)

				// Add payload (skip FU indicator and FU header)
				if len(payload) > 2 {
					frameData = append(frameData, payload[2:]...)
				}
			} else if isEnd == 1 {
				// End of FU-A
				if !fuaState.active || !fuaState.started {
					isCorrupted = true
					fuaState.active = false
					continue
				}
				fuaState.ended = true
				fuaState.active = false

				// Add payload (skip FU indicator and FU header)
				if len(payload) > 2 {
					frameData = append(frameData, payload[2:]...)
				}
			} else {
				// Middle fragment
				if !fuaState.active || !fuaState.started {
					isCorrupted = true
					continue
				}

				// Add payload (skip FU indicator and FU header)
				if len(payload) > 2 {
					frameData = append(frameData, payload[2:]...)
				}
			}

		default:
			// Single NAL unit
			if fuaState.active && !fuaState.ended {
				// FU-A not properly ended
				isCorrupted = true
				fuaState.active = false
			}

			// Extract and cache SPS/PPS
			if nalType == nalUnitTypeSPS {
				d.spsNAL = append([]byte{}, startCode...)
				d.spsNAL = append(d.spsNAL, payload...)
			} else if nalType == nalUnitTypePPS {
				d.ppsNAL = append([]byte{}, startCode...)
				d.ppsNAL = append(d.ppsNAL, payload...)
			} else if nalType == nalUnitTypeIDR {
				hasIDR = true
			}

			// Add start code and NAL unit
			frameData = append(frameData, startCode...)
			frameData = append(frameData, payload...)
		}
	}

	// If FU-A was started but not properly ended, mark as corrupted
	if fuaState.active && !fuaState.ended {
		isCorrupted = true
	}

	// If frame has no data, return nil
	if len(frameData) == 0 {
		logger.Warn("[H264Decoder:finalizeFrame] Frame has no data: timestamp=%d, packets=%d", fa.timestamp, len(fa.packets))
		return nil
	}
	
	logger.Debug("[H264Decoder:finalizeFrame] Frame assembled: timestamp=%d, size=%d bytes, packets=%d, hasIDR=%t, corrupted=%t", 
		fa.timestamp, len(frameData), len(fa.packets), hasIDR, isCorrupted)

	// For IDR frames, prepend SPS/PPS if available
	if hasIDR && len(d.spsNAL) > 0 && len(d.ppsNAL) > 0 {
		// Check if frame already has SPS/PPS
		hasSPS := d.frameHasNAL(frameData, nalUnitTypeSPS)
		hasPPS := d.frameHasNAL(frameData, nalUnitTypePPS)

		if !hasSPS || !hasPPS {
			// Prepend SPS/PPS
			prepended := make([]byte, 0, len(d.spsNAL)+len(d.ppsNAL)+len(frameData))
			prepended = append(prepended, d.spsNAL...)
			prepended = append(prepended, d.ppsNAL...)
			prepended = append(prepended, frameData...)
			frameData = prepended
		}
	}

	// If corrupted and we're dropping corrupted frames, return nil
	if isCorrupted && d.dropCorruptedFrames {
		d.stats.CorruptedFrames++
		return nil
	}

	// Create frame
	frame := &Frame{
		Data:        frameData,
		Timestamp:   fa.timestamp,
		IsCorrupted: isCorrupted,
	}
	frame.IsKey = frame.IsKeyFrame()

	// Update statistics
	d.stats.TotalFrames++
	if frame.IsCorrupted {
		d.stats.CorruptedFrames++
	}

	return frame
}

// unpackSTAPA unpacks a STAP-A packet into individual NAL units
func (d *H264Decoder) unpackSTAPA(payload []byte) [][]byte {
	if len(payload) < 1 {
		return nil
	}

	var nalUnits [][]byte
	offset := 1 // Skip STAP-A indicator

	for offset < len(payload) {
		if offset+2 > len(payload) {
			break
		}

		// Read NAL unit size (2 bytes, big-endian)
		nalSize := uint16(payload[offset])<<8 | uint16(payload[offset+1])
		offset += 2

		if offset+int(nalSize) > len(payload) {
			break
		}

		// Extract NAL unit
		nalUnit := payload[offset : offset+int(nalSize)]
		nalUnits = append(nalUnits, nalUnit)
		offset += int(nalSize)
	}

	return nalUnits
}

// frameHasNAL checks if frame contains a specific NAL type
func (d *H264Decoder) frameHasNAL(frameData []byte, nalType byte) bool {
	for i := 0; i <= len(frameData)-startCodeSize-1; i++ {
		// Look for start code
		if frameData[i] == 0x00 && frameData[i+1] == 0x00 {
			if (i+3 < len(frameData) && frameData[i+2] == 0x00 && frameData[i+3] == 0x01) ||
				(i+2 < len(frameData) && frameData[i+2] == 0x01) {

				var nalStart int
				if frameData[i+2] == 0x01 {
					nalStart = i + 3
				} else {
					nalStart = i + 4
				}

				if nalStart < len(frameData) {
					foundType := frameData[nalStart] & 0x1F
					if foundType == nalType {
						return true
					}
				}
			}
		}
	}
	return false
}

// detectPacketLoss checks if there's a gap in sequence numbers
func (d *H264Decoder) detectPacketLoss(currentSeq uint16) bool {
	if !d.sequenceInit {
		return false
	}

	// Handle sequence number wraparound (65535 -> 0)
	expectedSeq := d.expectedSequence

	// Check if current sequence is what we expected
	if currentSeq != expectedSeq {
		// Calculate gap size (handling wraparound)
		gap := sequenceDifference(expectedSeq, currentSeq)

		// If gap is 1-100, it's likely packet loss
		// If gap > 32000, it's probably wraparound or old packet
		if gap > 0 && gap < 100 {
			return true
		}
	}

	return false
}

// sequenceDifference calculates the difference between sequences handling wraparound
func sequenceDifference(seq1, seq2 uint16) uint16 {
	if seq2 >= seq1 {
		return seq2 - seq1
	}
	// Wraparound case
	return (65535 - seq1) + seq2 + 1
}

// sequenceBefore returns true if seq1 comes before seq2 (wrap-aware)
func sequenceBefore(seq1, seq2 uint16) bool {
	diff := sequenceDifference(seq1, seq2)
	return diff < 32768 // Less than half the space = seq1 before seq2
}

// sequenceAfter returns true if seq1 comes after seq2 (wrap-aware)
func sequenceAfter(seq1, seq2 uint16) bool {
	return sequenceBefore(seq2, seq1)
}


// Reset resets the decoder state
func (d *H264Decoder) Reset() {
	// Clear all frame assemblies
	for k := range d.frameMap {
		delete(d.frameMap, k)
	}
	d.currentFrame = nil
	// Note: Don't reset SSRC tracking - it persists across frame boundaries
	// Note: Don't reset SPS/PPS - they remain valid until SSRC changes
}

// GetCurrentSSRC returns the current stream SSRC
func (d *H264Decoder) GetCurrentSSRC() uint32 {
	return d.currentSSRC
}

// GetStats returns decoder statistics
func (d *H264Decoder) GetStats() DecoderStats {
	return d.stats
}

// ResetStats resets decoder statistics
func (d *H264Decoder) ResetStats() {
	d.stats = DecoderStats{}
}

// IsKeyFrame checks if the frame contains a keyframe (IDR)
func (f *Frame) IsKeyFrame() bool {
	if len(f.Data) < startCodeSize+1 {
		return false
	}

	// Find NAL units in the frame
	for i := 0; i <= len(f.Data)-startCodeSize-1; i++ {
		// Look for start code
		if f.Data[i] == 0x00 && f.Data[i+1] == 0x00 {
			if (i+3 < len(f.Data) && f.Data[i+2] == 0x00 && f.Data[i+3] == 0x01) ||
				(i+2 < len(f.Data) && f.Data[i+2] == 0x01) {

				var nalStart int
				if f.Data[i+2] == 0x01 {
					nalStart = i + 3
				} else {
					nalStart = i + 4
				}

				if nalStart < len(f.Data) {
					nalType := f.Data[nalStart] & 0x1F
					if nalType == nalUnitTypeIDR {
						return true
					}
				}
			}
		}
	}

	return false
}

// GetTimestampString returns the timestamp as a string for filename
func (f *Frame) GetTimestampString() string {
	return fmt.Sprintf("%d", f.Timestamp)
}

// String returns a string representation of the frame for debugging
func (f *Frame) String() string {
	corrupted := ""
	if f.IsCorrupted {
		corrupted = ", CORRUPTED"
	}
	return fmt.Sprintf("Frame [Timestamp: %d, Size: %d bytes, IsKey: %t%s]",
		f.Timestamp, len(f.Data), f.IsKey, corrupted)
}

// Helper functions

func isFUA(payload []byte) bool {
	if len(payload) < 1 {
		return false
	}
	nalType := payload[0] & 0x1F
	return nalType == nalUnitTypeFUA
}

func getFUANALType(payload []byte) byte {
	if len(payload) < 2 {
		return 0
	}
	return payload[1] & 0x1F
}

func isFUAStart(payload []byte) bool {
	if len(payload) < 2 {
		return false
	}
	return (payload[1]>>7)&0x01 == 1
}
