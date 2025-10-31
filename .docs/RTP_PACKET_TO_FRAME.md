# RTP Packet to Frame Conversion

## Overview

In RTSP video streaming, **one RTP packet does NOT necessarily equal one frame**. A video frame can be transmitted either as a single RTP packet or split across multiple RTP packets depending on the frame size.

## Key Concept

```
Small Frame:  1 RTP Packet = 1 Frame
Large Frame:  Multiple RTP Packets = 1 Frame (fragmented)
```

## RTP Packet Structure

An RTP packet consists of:

```
+------------------+
| RTP Header       | (12+ bytes)
+------------------+
| Payload Data     | (H.264 NAL unit or fragment)
+------------------+
```

### Important RTP Header Fields

| Field | Purpose |
|-------|---------|
| **Marker Bit** | Set to `1` on the last packet of a frame |
| **Timestamp** | Same value for all packets belonging to one frame |
| **Sequence Number** | Increments for each packet (for ordering and loss detection) |
| **Payload Type** | Identifies the codec (e.g., H.264) |

## H.264 Packetization Modes

### Mode 1: Single NAL Unit

When a frame (NAL unit) is small enough to fit in a single RTP packet:

```
┌─────────────────────────────────┐
│  RTP Packet (Marker = 1)        │
│  ┌─────────────────────────┐    │
│  │ Complete NAL Unit       │    │
│  └─────────────────────────┘    │
└─────────────────────────────────┘
         ↓
    Complete Frame
```

**Processing:**
1. Receive RTP packet
2. Check if NAL type != 28 (not fragmented)
3. If marker bit = 1, frame is complete
4. Add H.264 start code (0x00 0x00 0x00 0x01) + payload = complete frame

### Mode 2: Fragmentation Unit A (FU-A)

When a frame is too large (common for keyframes), it's split across multiple packets:

```
┌─────────────────────────────────┐
│  RTP Packet 1 (Marker = 0)      │
│  ┌─────────────────────────┐    │
│  │ FU-A Start Fragment     │    │
│  │ [S=1, E=0]              │    │
│  └─────────────────────────┘    │
└─────────────────────────────────┘
         ↓
┌─────────────────────────────────┐
│  RTP Packet 2 (Marker = 0)      │
│  ┌─────────────────────────┐    │
│  │ FU-A Middle Fragment    │    │
│  │ [S=0, E=0]              │    │
│  └─────────────────────────┘    │
└─────────────────────────────────┘
         ↓
┌─────────────────────────────────┐
│  RTP Packet N (Marker = 1)      │
│  ┌─────────────────────────┐    │
│  │ FU-A End Fragment       │    │
│  │ [S=0, E=1]              │    │
│  └─────────────────────────┘    │
└─────────────────────────────────┘
         ↓
    Complete Frame (assembled)
```

**FU-A Header Format:**
```
 0 1 2 3 4 5 6 7
+-+-+-+-+-+-+-+-+
|S|E|R|  Type   |
+-+-+-+-+-+-+-+-+

S = Start bit (1 for first fragment)
E = End bit (1 for last fragment)
R = Reserved (must be 0)
Type = NAL unit type (5 bits)
```

## Implementation in rtsp-client

### Step 1: Receive RTP Packet

The RTSP client receives raw RTP packets from the network:

```go
// client.go - ReadPacket()
buffer := make([]byte, 2048)
n, err := c.rtpConn.Read(buffer)
packet, err := rtp.ParsePacket(buffer[:n])
```

### Step 2: Parse RTP Header

Extract key fields from the RTP packet:

```go
// packet.go - ParsePacket()
packet := &Packet{
    Version:        (data[0] >> 6) & 0x03,
    Marker:         (data[1] >> 7) & 0x01 == 1,
    PayloadType:    data[1] & 0x7F,
    SequenceNumber: binary.BigEndian.Uint16(data[2:4]),
    Timestamp:      binary.BigEndian.Uint32(data[4:8]),
    SSRC:           binary.BigEndian.Uint32(data[8:12]),
    Payload:        payload,
}
```

### Step 3: Decode Payload into Frame

The H.264 decoder determines if the packet is single or fragmented:

```go
// h264.go - ProcessPacket()
func (d *H264Decoder) ProcessPacket(packet *rtp.Packet) *Frame {
    // Check if this is a FU-A (NAL type 28)
    if isFUA(packet.Payload) {
        return d.processFUA(packet)
    }
    
    // Single NAL unit
    return d.processSingleNAL(packet)
}
```

#### Processing Single NAL Unit

```go
func (d *H264Decoder) processSingleNAL(packet *rtp.Packet) *Frame {
    // Add H.264 start code
    d.buffer = append(d.buffer, startCode...) // [0x00, 0x00, 0x00, 0x01]
    d.buffer = append(d.buffer, packet.Payload...)
    
    // If marker bit is set, frame is complete
    if packet.Marker {
        frame := d.createFrame()
        d.Reset()
        return frame
    }
    
    return nil // Not complete yet
}
```

#### Processing FU-A Fragments

```go
func (d *H264Decoder) processFUA(packet *rtp.Packet) *Frame {
    fuHeader := packet.Payload[1]
    isStart := (fuHeader >> 7) & 0x01
    isEnd := (fuHeader >> 6) & 0x01
    
    if isStart == 1 {
        // First fragment: reconstruct NAL header
        nalType := fuHeader & 0x1F
        fnri := packet.Payload[0] & 0xE0
        nalHeader := fnri | nalType
        
        d.buffer = append(d.buffer, startCode...)
        d.buffer = append(d.buffer, nalHeader)
        d.buffer = append(d.buffer, packet.Payload[2:]...)
        d.fragmenting = true
    } else {
        // Middle or end fragment
        d.buffer = append(d.buffer, packet.Payload[2:]...)
    }
    
    // Check if complete
    if isEnd == 1 || packet.Marker {
        d.fragmenting = false
        frame := d.createFrame()
        d.Reset()
        return frame
    }
    
    return nil // Still accumulating
}
```

### Step 4: Create Complete Frame

Once all packets are received:

```go
func (d *H264Decoder) createFrame() *Frame {
    frameData := make([]byte, len(d.buffer))
    copy(frameData, d.buffer)
    
    frame := &Frame{
        Data:      frameData,
        Timestamp: d.currentTimestamp,
        IsKey:     frame.IsKeyFrame(),
    }
    
    return frame
}
```

## Frame Detection Logic

The decoder knows a frame is complete when:

1. **Marker bit is set** (`packet.Marker == true`)
2. **FU-A end bit is set** (for fragmented frames)
3. **Timestamp changes** (new frame started before previous completed - error recovery)

## Common Scenarios

### Scenario 1: P-Frame (Small)
```
Packet 1: [Timestamp=1000, Marker=1, NAL Type=1]
          ↓
Result: 1 complete frame immediately
```

### Scenario 2: I-Frame/Keyframe (Large)
```
Packet 1: [Timestamp=2000, Marker=0, NAL Type=28, FU-A Start]
Packet 2: [Timestamp=2000, Marker=0, NAL Type=28, FU-A Middle]
Packet 3: [Timestamp=2000, Marker=0, NAL Type=28, FU-A Middle]
Packet 4: [Timestamp=2000, Marker=1, NAL Type=28, FU-A End]
          ↓
Result: 1 complete frame after packet 4
```

### Scenario 3: Multiple Frames in Sequence
```
Packet 1: [TS=1000, M=1, Type=1] → Frame 1 complete
Packet 2: [TS=2000, M=0, Type=28, Start]
Packet 3: [TS=2000, M=1, Type=28, End] → Frame 2 complete
Packet 4: [TS=3000, M=1, Type=1] → Frame 3 complete
```

## Key Takeaways

1. **Frame boundaries are determined by:**
   - Timestamp (same for all packets in a frame)
   - Marker bit (set on last packet)
   - FU-A start/end flags (for fragmented frames)

2. **The decoder maintains state:**
   - Buffer for accumulating fragments
   - Current timestamp being processed
   - Fragmentation state flag

3. **ProcessPacket returns:**
   - `nil` while accumulating packets
   - `*Frame` when a complete frame is assembled

4. **Typical frame sizes:**
   - P-frames: Often fit in single packet (~500-2000 bytes)
   - I-frames/Keyframes: Usually fragmented (10KB-100KB+)

5. **H.264 Start Code:**
   - Every NAL unit in the final frame data is prefixed with `[0x00, 0x00, 0x00, 0x01]`
   - This is the Annex B format required by most H.264 decoders

## Related Files

- `pkg/rtp/packet.go` - RTP packet parsing
- `pkg/decoder/h264.go` - H.264 frame assembly
- `pkg/rtsp/client.go` - Network I/O and packet reception

## References

- [RFC 3550 - RTP: A Transport Protocol for Real-Time Applications](https://tools.ietf.org/html/rfc3550)
- [RFC 6184 - RTP Payload Format for H.264 Video](https://tools.ietf.org/html/rfc6184)
- [H.264/AVC Specification - ITU-T Rec. H.264](https://www.itu.int/rec/T-REC-H.264)

