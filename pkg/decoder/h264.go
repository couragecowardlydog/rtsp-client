package decoder

import (
	"fmt"
	"sort"
	"time"

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

	// Initialize SSRC tracking on first packet
	if !d.ssrcInitialized {
		d.currentSSRC = packet.SSRC
		d.ssrcInitialized = true
	}

	// Detect SSRC change (stream changed or camera rebooted)
	if packet.SSRC != d.currentSSRC {
		fmt.Printf("⚠️  SSRC changed: 0x%x → 0x%x (stream changed or camera rebooted)\n",
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
				if frame := d.finalizeFrame(fa); frame != nil {
					if ts == packet.Timestamp {
						completedFrame = frame
					} else {
						// Another frame completed, return it
						delete(d.frameMap, ts)
						return frame
					}
				}
				delete(d.frameMap, ts)
			}
		}
	}

	// Check if current frame should be finalized
	if d.shouldFinalizeFrame(frameAssembly) {
		completedFrame = d.finalizeFrame(frameAssembly)
		delete(d.frameMap, packet.Timestamp)
		return completedFrame
	}

	return nil
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

// processSingleNAL processes a single NAL unit packet
func (d *H264Decoder) processSingleNAL(packet *rtp.Packet) *Frame {
	// Add start code and NAL unit to buffer
	d.buffer = append(d.buffer, startCode...)
	d.buffer = append(d.buffer, packet.Payload...)

	// If marker bit is set, frame is complete
	if packet.Marker {
		frame := d.createFrame()
		d.Reset()
		return frame
	}

	return nil
}

// processFUA processes a Fragmentation Unit A packet
func (d *H264Decoder) processFUA(packet *rtp.Packet) *Frame {
	if len(packet.Payload) < 2 {
		return nil
	}

	fuHeader := packet.Payload[1]
	isStart := (fuHeader >> 7) & 0x01
	isEnd := (fuHeader >> 6) & 0x01

	if isStart == 1 {
		// Start of fragmented NAL unit
		d.fragmenting = true

		// Reconstruct NAL header
		nalType := fuHeader & 0x1F
		fnri := packet.Payload[0] & 0xE0
		nalHeader := fnri | nalType

		// Add start code and NAL header
		d.buffer = append(d.buffer, startCode...)
		d.buffer = append(d.buffer, nalHeader)

		// Add payload (skip FU indicator and FU header)
		if len(packet.Payload) > 2 {
			d.buffer = append(d.buffer, packet.Payload[2:]...)
		}
	} else {
		// Middle or end fragment
		if len(packet.Payload) > 2 {
			d.buffer = append(d.buffer, packet.Payload[2:]...)
		}
	}

	// Check if this is the end of the fragmented NAL unit
	if isEnd == 1 || packet.Marker {
		d.fragmenting = false
		frame := d.createFrame()
		d.Reset()
		return frame
	}

	return nil
}

// createFrame creates a Frame from the current buffer
func (d *H264Decoder) createFrame() *Frame {
	if len(d.buffer) == 0 {
		return nil
	}

	// Copy buffer to frame data
	frameData := make([]byte, len(d.buffer))
	copy(frameData, d.buffer)

	frame := &Frame{
		Data:        frameData,
		Timestamp:   d.currentTimestamp,
		IsCorrupted: d.packetLossDetected,
	}

	frame.IsKey = frame.IsKeyFrame()

	// Update statistics
	d.stats.TotalFrames++
	if frame.IsCorrupted {
		d.stats.CorruptedFrames++
	}

	return frame
}

// Reset resets the decoder state
func (d *H264Decoder) Reset() {
	d.buffer = d.buffer[:0]
	d.fragmenting = false
	d.packetLossDetected = false
	// Note: Don't reset SSRC tracking - it persists across frame boundaries
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
