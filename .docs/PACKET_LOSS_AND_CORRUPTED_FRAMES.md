# 🔴 Packet Loss Detection & Corrupted Frame Handling

## Overview

Real-world RTSP streaming often experiences packet loss due to network issues. When RTP packets are lost during frame transmission, the resulting video frames become **corrupted** with missing data. Instead of discarding these frames, the client **saves them separately for analysis**.

## 🎯 The Problem

### What Happens During Packet Loss?

```
Frame needs 3 packets:

Packet 1 (Seq: 100, Start): ✅ Received
Packet 2 (Seq: 101, Middle): ❌ LOST (network issue)
Packet 3 (Seq: 102, End): ✅ Received

Result: Frame assembled from packets 1 + 3 = CORRUPTED (missing middle data)
```

### Why This Matters

**Corrupted frames cause:**
- Visual artifacts when decoded (green blocks, glitches)
- Decoder errors in video players
- Propagating errors to subsequent frames (depending on codec)
- Loss of critical video data

**In production:**
- Network isn't perfect
- Cameras may be far away
- WiFi can drop packets
- Bandwidth can be limited

## 🔍 How Detection Works

### 1. Sequence Number Tracking

Every RTP packet has a **16-bit sequence number** that increments by 1:

```
Packet 1: Seq 100
Packet 2: Seq 101  ← Expected
Packet 3: Seq 102
```

If we receive sequence 100 then suddenly 102, we know **packet 101 was lost**.

### 2. Detection Algorithm

```go
// In H264Decoder
expectedSequence uint16  // What we expect next
lastSequence     uint16  // What we received last

// When new packet arrives
if currentSeq != expectedSequence {
    gap := currentSeq - expectedSequence
    if gap > 0 && gap < 100 {
        // Packet loss detected!
        if fragmenting {
            // We're building a frame, mark it as corrupted
            packetLossDetected = true
        }
    }
}
```

### 3. Sequence Number Wraparound

Sequence numbers are 16-bit (0-65535), so they wrap around:

```
65533
65534
65535
0      ← Wraps back to zero
1
2
```

The decoder handles this correctly to avoid false positives.

## 📁 Storage Strategy

### Directory Structure

```
frames/
├── 1000000.h264          ← Good frame
├── 1003000.h264          ← Good frame
├── 1006000.h264          ← Good frame
└── corrupted_frames/
    ├── 1001500_corrupted.h264  ← Bad frame (for analysis)
    ├── 1004200_corrupted.h264  ← Bad frame
    └── 1008100_corrupted.h264  ← Bad frame
```

### Why Save Corrupted Frames?

Instead of discarding them, we save them because:

✅ **Debugging** - Analyze what went wrong  
✅ **Network monitoring** - Track packet loss patterns  
✅ **Quality metrics** - Measure stream health  
✅ **Forensics** - Investigate issues after the fact  
✅ **Development** - Test decoder robustness  

### Filename Convention

```
Normal:    {timestamp}.h264
Corrupted: {timestamp}_corrupted.h264
```

Easy to identify and process separately!

## 🎬 Real-World Example

### Scenario: Intermittent WiFi

```bash
$ ./bin/rtsp-client -url rtsp://192.168.1.100/stream -verbose

🔌 Connecting to RTSP server...
✅ Streaming started. Press Ctrl+C to stop.
📡 Receiving frames...

💾 Saved frame: 1000000 (timestamp: 1000000) [KEYFRAME]
💾 Saved frame: 1003000 (timestamp: 1003000)
🔴 Corrupted frame saved: 1006000 (timestamp: 1006000) → corrupted_frames/
💾 Saved frame: 1009000 (timestamp: 1009000)
💾 Saved frame: 1012000 (timestamp: 1012000)
🔴 Corrupted frame saved: 1015000 (timestamp: 1015000) → corrupted_frames/

📊 Frames: 156 (Keyframes: 12, Corrupted: 8) | Size: 4.23 MB | Recoveries: 0✅ 0❌ | Packet Loss: 8 events

^C
📊 Final Statistics:
  Storage: Frames: 156 (Keyframes: 12, Corrupted: 8) | Size: 4.23 MB
  Decoder: Total Frames: 156, Corrupted: 8, Packet Loss Events: 8

⚠️  8 corrupted frames saved to: ./frames/corrupted_frames/
```

### Log Messages

| Emoji | Message | Meaning |
|-------|---------|---------|
| 💾 | Saved frame | Normal frame stored |
| 🔴 | Corrupted frame saved | Damaged frame stored to `corrupted_frames/` |
| ⚠️ | Packet Loss: N events | N packet loss occurrences detected |

## 📊 Statistics Tracking

### Decoder Stats

```go
type DecoderStats struct {
    TotalFrames      int  // All frames processed
    CorruptedFrames  int  // Frames with packet loss
    PacketLossEvents int  // Number of times packet loss detected
}

// Access stats
stats := decoder.GetStats()
fmt.Printf("Corruption rate: %.2f%%", 
    float64(stats.CorruptedFrames) / float64(stats.TotalFrames) * 100)
```

### Storage Stats

```go
type StorageStats struct {
    TotalFrames     int64  // All saved frames
    KeyFrames       int64  // I-frames (keyframes)
    CorruptedFrames int64  // Saved to corrupted_frames/
    TotalBytes      int64  // Total disk usage
}
```

## 🔧 Configuration

### Adjust Packet Loss Sensitivity

In `pkg/decoder/h264.go`:

```go
// Current: Gap between 1-100 = packet loss
if gap > 0 && gap < 100 {
    return true
}

// More sensitive (catches smaller gaps)
if gap > 0 && gap < 50 {
    return true
}

// Less sensitive (ignores small gaps)
if gap > 0 && gap < 200 {
    return true
}
```

### Disable Corrupted Frame Saving

If you want to discard instead of save:

```go
// In cmd/rtsp-client/main.go
if frame.IsCorrupted {
    log.Printf("⚠️  Discarding corrupted frame: %d", frame.Timestamp)
    continue  // Skip saving
}
```

## 🧪 Testing Packet Loss

### Method 1: Network Simulation

Use `tc` (traffic control) on Linux to simulate packet loss:

```bash
# Add 5% packet loss
sudo tc qdisc add dev eth0 root netem loss 5%

# Test the client
./bin/rtsp-client -url rtsp://192.168.1.100/stream

# Remove packet loss
sudo tc qdisc del dev eth0 root
```

### Method 2: WiFi Distance

Simply move the device farther from the WiFi router to naturally introduce packet loss.

### Method 3: Network Congestion

Run bandwidth-heavy tasks while streaming:

```bash
# In one terminal
./bin/rtsp-client -url rtsp://camera/stream

# In another terminal (create congestion)
iperf3 -c router_ip -t 60
```

## 🔬 Analyzing Corrupted Frames

### 1. Check File Sizes

```bash
# Compare sizes
ls -lh frames/*.h264
ls -lh frames/corrupted_frames/*.h264

# Corrupted frames are often smaller (missing data)
```

### 2. Try to Decode

```bash
# Attempt to decode with FFmpeg
ffmpeg -i frames/1000000.h264 -frames:v 1 good.png
ffmpeg -i frames/corrupted_frames/1003000_corrupted.h264 -frames:v 1 bad.png

# Compare the images
```

### 3. Hex Dump

```bash
# Look at raw data
hexdump -C frames/1000000.h264 | head -20
hexdump -C frames/corrupted_frames/1003000_corrupted.h264 | head -20
```

### 4. Calculate Corruption Rate

```bash
TOTAL=$(ls frames/*.h264 | wc -l)
CORRUPTED=$(ls frames/corrupted_frames/*.h264 | wc -l)
RATE=$(echo "scale=2; $CORRUPTED * 100 / $TOTAL" | bc)

echo "Corruption rate: ${RATE}%"
```

## 📈 Metrics & Monitoring

### What to Monitor

1. **Corruption Rate**
   ```
   corrupted_frames / total_frames * 100
   ```
   - **Good:** < 1%
   - **Acceptable:** 1-5%
   - **Bad:** > 5%

2. **Packet Loss Events**
   ```
   Number of times packet loss detected
   ```
   - Spikes indicate network issues
   - Patterns may reveal specific problems

3. **Corrupted Frame Distribution**
   ```
   Time-series analysis of when corruption occurs
   ```
   - Random = network noise
   - Periodic = interference
   - Burst = connection issues

### Alert Thresholds

```go
// Example monitoring
if stats.CorruptedFrames > stats.TotalFrames / 10 {
    // More than 10% corrupted
    alert("High packet loss detected!")
}

if decoderStats.PacketLossEvents > 100 {
    // More than 100 loss events
    alert("Network quality degraded")
}
```

## 🛠️ Troubleshooting

### High Corruption Rate

**Possible causes:**
- Poor network quality
- WiFi interference
- Bandwidth limitation
- Router/switch issues
- Camera overload

**Solutions:**
1. Use wired connection instead of WiFi
2. Reduce video bitrate at camera
3. Use QoS to prioritize RTSP traffic
4. Check for network congestion
5. Move closer to WiFi access point

### All Frames Corrupted

**Possible causes:**
- Stream not H.264 (wrong codec)
- Firewall blocking packets
- Severe network issues

**Solutions:**
1. Verify stream codec: `ffprobe rtsp://camera/stream`
2. Check firewall rules for UDP ports
3. Test with different camera/stream

### No Corruption Detected (But Video Has Issues)

**Possible causes:**
- Corruption happens before RTP layer
- Camera encoding issues
- Not using sequence numbers correctly

**Solutions:**
1. Check camera logs
2. Test with different client (VLC)
3. Verify RTP packet format

## 💡 Best Practices

### 1. Regular Monitoring

```go
// Log stats periodically
go func() {
    ticker := time.NewTicker(1 * time.Minute)
    for range ticker.C {
        stats := decoder.GetStats()
        if stats.CorruptedFrames > 0 {
            rate := float64(stats.CorruptedFrames) / float64(stats.TotalFrames) * 100
            log.Printf("⚠️  Corruption rate: %.2f%% (%d/%d frames)",
                rate, stats.CorruptedFrames, stats.TotalFrames)
        }
    }
}()
```

### 2. Periodic Cleanup

Corrupted frames can accumulate. Clean them periodically:

```bash
# Keep only last 1000 corrupted frames
cd frames/corrupted_frames
ls -t *.h264 | tail -n +1001 | xargs rm -f
```

### 3. Archive for Analysis

```bash
# Archive corrupted frames daily
DATE=$(date +%Y%m%d)
tar -czf corrupted_frames_${DATE}.tar.gz frames/corrupted_frames/
mv corrupted_frames_${DATE}.tar.gz /archive/
```

### 4. Network Quality Baseline

Establish baseline corruption rates for your environment:

```bash
# Test for 10 minutes
timeout 600 ./bin/rtsp-client -url rtsp://camera/stream

# Check corruption rate
# This becomes your "normal" baseline
```

## 🎓 Technical Details

### Fragmentation Unit A (FU-A)

Large frames are split using FU-A:

```
FU-A Header:
 0 1 2 3 4 5 6 7
+-+-+-+-+-+-+-+-+
|S|E|R|  Type   |
+-+-+-+-+-+-+-+-+

S = Start bit
E = End bit
```

If packet with `S=1` is lost → entire frame is lost (no start)  
If middle packet lost → frame is corrupted (missing data)  
If packet with `E=1` is lost → frame never completes

### Sequence Number Arithmetic

RTP uses 16-bit sequences with wraparound:

```go
// Handle wraparound correctly
func sequenceDifference(seq1, seq2 uint16) uint16 {
    if seq2 >= seq1 {
        return seq2 - seq1
    }
    // Wraparound case: 65535 → 0
    return (65535 - seq1) + seq2 + 1
}
```

### Frame Integrity

A frame is marked corrupted if:
1. Sequence gap detected during fragmentation
2. Timestamp changes before FU-A end received
3. Unexpected FU-A flags (middle before start)

## 📁 Related Files

- `pkg/decoder/h264.go` - Packet loss detection logic
- `pkg/storage/storage.go` - Corrupted frame storage
- `cmd/rtsp-client/main.go` - Logging and statistics
- `pkg/rtp/packet.go` - RTP packet structure

## 🔗 See Also

- [Error Handling & Recovery](.docs/ERROR_HANDLING_AND_RECOVERY.md)
- [RTP Packet to Frame Conversion](.docs/RTP_PACKET_TO_FRAME.md)
- [Architecture Documentation](.docs/ARCHITECTURE.md)

## 📚 References

- [RFC 3550 - RTP: A Transport Protocol for Real-Time Applications](https://tools.ietf.org/html/rfc3550)
- [RFC 6184 - RTP Payload Format for H.264 Video](https://tools.ietf.org/html/rfc6184)
- [RFC 1982 - Serial Number Arithmetic](https://tools.ietf.org/html/rfc1982)

