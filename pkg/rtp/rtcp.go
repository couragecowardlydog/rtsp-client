package rtp

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

var (
	// ErrRTCPPacketTooShort indicates RTCP packet is too short
	ErrRTCPPacketTooShort = errors.New("RTCP packet too short")
	// ErrInvalidRTCPVersion indicates invalid RTCP version
	ErrInvalidRTCPVersion = errors.New("invalid RTCP version")
)

// RTCP Packet Types
const (
	RTCP_SR   = 200 // Sender Report
	RTCP_RR   = 201 // Receiver Report
	RTCP_SDES = 202 // Source Description
	RTCP_BYE  = 203 // Goodbye
	RTCP_APP  = 204 // Application-Defined
)

// SDES Item Types
const (
	SDES_END   = 0
	SDES_CNAME = 1
	SDES_NAME  = 2
	SDES_EMAIL = 3
	SDES_PHONE = 4
	SDES_LOC   = 5
	SDES_TOOL  = 6
	SDES_NOTE  = 7
	SDES_PRIV  = 8
)

// RTCPPacket is the interface for all RTCP packet types
type RTCPPacket interface {
	GetPacketType() uint8
	GetSSRC() uint32
}

// SenderReport represents an RTCP SR packet
type SenderReport struct {
	PacketType   uint8
	SSRC         uint32
	NTPTimestamp uint64 // NTP timestamp (64-bit)
	RTPTimestamp uint32 // RTP timestamp
	PacketCount  uint32 // Sender's packet count
	OctetCount   uint32 // Sender's octet count
	ReportBlocks []ReportBlock
}

// GetPacketType returns the packet type
func (sr *SenderReport) GetPacketType() uint8 {
	return sr.PacketType
}

// GetSSRC returns the SSRC
func (sr *SenderReport) GetSSRC() uint32 {
	return sr.SSRC
}

// ReceiverReport represents an RTCP RR packet
type ReceiverReport struct {
	PacketType  uint8
	SSRC        uint32
	ReportBlock *ReportBlock
}

// GetPacketType returns the packet type
func (rr *ReceiverReport) GetPacketType() uint8 {
	return rr.PacketType
}

// GetSSRC returns the SSRC
func (rr *ReceiverReport) GetSSRC() uint32 {
	return rr.SSRC
}

// ReportBlock contains reception statistics
type ReportBlock struct {
	SSRC         uint32
	FractionLost uint8  // Fraction of packets lost
	PacketsLost  int32  // Cumulative packets lost
	HighestSeq   uint32 // Extended highest sequence number
	Jitter       uint32 // Interarrival jitter
	LSR          uint32 // Last SR timestamp
	DLSR         uint32 // Delay since last SR
}

// SDESPacket represents RTCP SDES packet
type SDESPacket struct {
	PacketType uint8
	Chunks     []SDESChunk
}

// GetPacketType returns the packet type
func (sdes *SDESPacket) GetPacketType() uint8 {
	return sdes.PacketType
}

// GetSSRC returns first chunk's SSRC
func (sdes *SDESPacket) GetSSRC() uint32 {
	if len(sdes.Chunks) > 0 {
		return sdes.Chunks[0].SSRC
	}
	return 0
}

// SDESChunk represents a source description chunk
type SDESChunk struct {
	SSRC  uint32
	Items []SDESItem
}

// SDESItem represents an SDES item
type SDESItem struct {
	Type uint8
	Text string
}

// BYEPacket represents RTCP BYE packet
type BYEPacket struct {
	PacketType uint8
	SSRCs      []uint32
	Reason     string
}

// GetPacketType returns the packet type
func (bye *BYEPacket) GetPacketType() uint8 {
	return bye.PacketType
}

// GetSSRC returns first SSRC
func (bye *BYEPacket) GetSSRC() uint32 {
	if len(bye.SSRCs) > 0 {
		return bye.SSRCs[0]
	}
	return 0
}

// CompoundRTCPPacket represents multiple RTCP packets
type CompoundRTCPPacket struct {
	Packets []RTCPPacket
}

// GetPacketType returns first packet's type
func (c *CompoundRTCPPacket) GetPacketType() uint8 {
	if len(c.Packets) > 0 {
		return c.Packets[0].GetPacketType()
	}
	return 0
}

// GetSSRC returns first packet's SSRC
func (c *CompoundRTCPPacket) GetSSRC() uint32 {
	if len(c.Packets) > 0 {
		return c.Packets[0].GetSSRC()
	}
	return 0
}

// ParseRTCPPacket parses RTCP packet from bytes
func ParseRTCPPacket(data []byte) (RTCPPacket, error) {
	if len(data) < 4 {
		return nil, ErrRTCPPacketTooShort
	}

	// Parse header
	version := (data[0] >> 6) & 0x03
	if version != 2 {
		return nil, ErrInvalidRTCPVersion
	}

	packetType := data[1]

	switch packetType {
	case RTCP_SR:
		return parseSenderReport(data)
	case RTCP_RR:
		return parseReceiverReport(data)
	case RTCP_SDES:
		return parseSDES(data)
	case RTCP_BYE:
		return parseBYE(data)
	default:
		// Unknown packet type, skip
		return nil, errors.New("unknown RTCP packet type")
	}
}

// parseSenderReport parses SR packet
func parseSenderReport(data []byte) (*SenderReport, error) {
	if len(data) < 28 {
		return nil, ErrRTCPPacketTooShort
	}

	sr := &SenderReport{
		PacketType: data[1],
	}

	// SSRC of sender
	sr.SSRC = binary.BigEndian.Uint32(data[4:8])

	// NTP Timestamp (64-bit)
	ntpMSW := binary.BigEndian.Uint32(data[8:12])
	ntpLSW := binary.BigEndian.Uint32(data[12:16])
	sr.NTPTimestamp = (uint64(ntpMSW) << 32) | uint64(ntpLSW)

	// RTP Timestamp
	sr.RTPTimestamp = binary.BigEndian.Uint32(data[16:20])

	// Sender's packet count
	sr.PacketCount = binary.BigEndian.Uint32(data[20:24])

	// Sender's octet count
	sr.OctetCount = binary.BigEndian.Uint32(data[24:28])

	// Parse report blocks if present
	rc := data[0] & 0x1F // Report count
	sr.ReportBlocks = make([]ReportBlock, 0, rc)

	offset := 28
	for i := 0; i < int(rc) && offset+24 <= len(data); i++ {
		rb := parseReportBlock(data[offset : offset+24])
		sr.ReportBlocks = append(sr.ReportBlocks, rb)
		offset += 24
	}

	return sr, nil
}

// parseReceiverReport parses RR packet
func parseReceiverReport(data []byte) (*ReceiverReport, error) {
	if len(data) < 8 {
		return nil, ErrRTCPPacketTooShort
	}

	rr := &ReceiverReport{
		PacketType: data[1],
		SSRC:       binary.BigEndian.Uint32(data[4:8]),
	}

	// Parse report block if present
	rc := data[0] & 0x1F
	if rc > 0 && len(data) >= 32 {
		rb := parseReportBlock(data[8:32])
		rr.ReportBlock = &rb
	}

	return rr, nil
}

// parseReportBlock parses a report block
func parseReportBlock(data []byte) ReportBlock {
	rb := ReportBlock{}

	rb.SSRC = binary.BigEndian.Uint32(data[0:4])
	rb.FractionLost = data[4]

	// Packets lost (24-bit signed)
	packetsLost := int32(data[5])<<16 | int32(data[6])<<8 | int32(data[7])
	if data[5]&0x80 != 0 {
		// Sign extend if negative
		packetsLost |= ^int32(0xFFFFFF)
	}
	rb.PacketsLost = packetsLost

	rb.HighestSeq = binary.BigEndian.Uint32(data[8:12])
	rb.Jitter = binary.BigEndian.Uint32(data[12:16])
	rb.LSR = binary.BigEndian.Uint32(data[16:20])
	rb.DLSR = binary.BigEndian.Uint32(data[20:24])

	return rb
}

// parseSDES parses SDES packet
func parseSDES(data []byte) (*SDESPacket, error) {
	if len(data) < 8 {
		return nil, ErrRTCPPacketTooShort
	}

	sdes := &SDESPacket{
		PacketType: data[1],
		Chunks:     []SDESChunk{},
	}

	sc := data[0] & 0x1F // Source count
	offset := 4

	for i := 0; i < int(sc) && offset < len(data); i++ {
		chunk := SDESChunk{}
		if offset+4 > len(data) {
			break
		}
		chunk.SSRC = binary.BigEndian.Uint32(data[offset : offset+4])
		offset += 4

		// Parse items
		for offset < len(data) {
			itemType := data[offset]
			if itemType == SDES_END {
				offset++
				break
			}
			if offset+1 >= len(data) {
				break
			}

			length := int(data[offset+1])
			if offset+2+length > len(data) {
				break
			}

			item := SDESItem{
				Type: itemType,
				Text: string(data[offset+2 : offset+2+length]),
			}
			chunk.Items = append(chunk.Items, item)
			offset += 2 + length
		}

		sdes.Chunks = append(sdes.Chunks, chunk)
	}

	return sdes, nil
}

// parseBYE parses BYE packet
func parseBYE(data []byte) (*BYEPacket, error) {
	if len(data) < 4 {
		return nil, ErrRTCPPacketTooShort
	}

	bye := &BYEPacket{
		PacketType: data[1],
		SSRCs:      []uint32{},
	}

	sc := data[0] & 0x1F // Source count
	offset := 4

	// Parse SSRCs
	for i := 0; i < int(sc) && offset+4 <= len(data); i++ {
		ssrc := binary.BigEndian.Uint32(data[offset : offset+4])
		bye.SSRCs = append(bye.SSRCs, ssrc)
		offset += 4
	}

	// Parse reason if present
	if offset < len(data) {
		length := int(data[offset])
		if offset+1+length <= len(data) {
			bye.Reason = string(data[offset+1 : offset+1+length])
		}
	}

	return bye, nil
}

// TimestampMapper maps between RTP and NTP timestamps
type TimestampMapper struct {
	mu           sync.RWMutex
	ntpTimestamp uint64
	rtpTimestamp uint32
	initialized  bool
	clockRate    uint32 // Default 90000 for H.264
}

// NewTimestampMapper creates a new timestamp mapper
func NewTimestampMapper() *TimestampMapper {
	return &TimestampMapper{
		clockRate: 90000, // 90 kHz for H.264
	}
}

// UpdateFromSR updates mapping from Sender Report
func (tm *TimestampMapper) UpdateFromSR(sr *SenderReport) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.ntpTimestamp = sr.NTPTimestamp
	tm.rtpTimestamp = sr.RTPTimestamp
	tm.initialized = true
}

// RTPToNTP converts RTP timestamp to NTP timestamp
func (tm *TimestampMapper) RTPToNTP(rtpTime uint32) uint64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if !tm.initialized {
		return 0
	}

	// Calculate RTP difference
	rtpDiff := int64(rtpTime) - int64(tm.rtpTimestamp)

	// Convert RTP units to NTP units (NTP fraction is 2^32 per second)
	// RTP is at clockRate Hz (usually 90000)
	ntpDiff := (rtpDiff << 32) / int64(tm.clockRate)

	return uint64(int64(tm.ntpTimestamp) + ntpDiff)
}

// NTPToTime converts NTP timestamp to Go time
func NTPToTime(ntp uint64) time.Time {
	// NTP epoch is Jan 1, 1900
	// Unix epoch is Jan 1, 1970
	// Difference is 2208988800 seconds
	const ntpEpochOffset = 2208988800

	seconds := (ntp >> 32) - ntpEpochOffset
	fraction := ntp & 0xFFFFFFFF

	// Convert fraction to nanoseconds
	nanos := (fraction * 1000000000) >> 32

	return time.Unix(int64(seconds), int64(nanos))
}

// TimeToNTP converts Go time to NTP timestamp
func TimeToNTP(t time.Time) uint64 {
	const ntpEpochOffset = 2208988800

	seconds := uint64(t.Unix() + ntpEpochOffset)
	nanos := uint64(t.Nanosecond())

	// Convert nanoseconds to NTP fraction
	fraction := (nanos << 32) / 1000000000

	return (seconds << 32) | fraction
}

// calculateFractionLost calculates fraction of packets lost (0-255)
func calculateFractionLost(expected, received int) uint8 {
	if expected <= 0 {
		return 0
	}

	lost := expected - received
	if lost < 0 {
		lost = 0
	}

	// Fraction is in units of 1/256
	fraction := (lost * 256) / expected
	if fraction > 255 {
		fraction = 255
	}

	return uint8(fraction)
}

// calculateInterarrivalJitter calculates jitter (RFC 3550)
func calculateInterarrivalJitter(jitter *uint32, rtpTimestamp uint32, arrivalTime time.Time,
	lastRTPTimestamp *uint32, lastArrivalTime *time.Time, clockRate uint32) {

	if lastArrivalTime.IsZero() {
		*lastRTPTimestamp = rtpTimestamp
		*lastArrivalTime = arrivalTime
		return
	}

	// Calculate transit time difference
	rtpDiff := int64(rtpTimestamp - *lastRTPTimestamp)
	arrivalDiff := arrivalTime.Sub(*lastArrivalTime).Nanoseconds()

	// Convert arrival time to RTP units
	arrivalDiffRTP := (arrivalDiff * int64(clockRate)) / 1000000000

	// Calculate difference
	d := rtpDiff - arrivalDiffRTP
	if d < 0 {
		d = -d
	}

	// Update jitter with smoothing (RFC 3550)
	// J(i) = J(i-1) + (|D(i-1,i)| - J(i-1))/16
	*jitter = *jitter + (uint32(d)-*jitter)/16

	*lastRTPTimestamp = rtpTimestamp
	*lastArrivalTime = arrivalTime
}
