# Critical Functional Gaps in RTP Packet to Frame Conversion

## Overview

The current implementation processes RTP packets immediately without proper frame assembly structures, reordering, or timeout handling. This violates the algorithm requirements and can cause frame corruption, missing packets, and incorrect frame boundaries.

## Critical Missing Components

### 1. Per-Timestamp Frame Assembly Structure

**Algorithm Requirement (Section 55-62):**
- Create a `FrameAssembly` structure for each distinct RTP timestamp
- Store packets mapped by sequence number within each FrameAssembly
- Track `seq_min`, `seq_max`, arrival time, and marker bit status
- Maintain status flags (collecting / complete / malformed / dropped)

**Current Implementation:**
- ❌ No per-timestamp frame assembly structure
- ❌ Single buffer that accumulates packets regardless of timestamp
- ❌ No tracking of which packets belong to which frame
- ❌ Packets are appended immediately to a single buffer

**Impact:**
- **Frame boundary errors**: Packets from different frames can be mixed
- **No frame isolation**: Cannot properly track when a frame is complete
- **Incorrect frame finalization**: May finalize frames with wrong packets

### 2. Packet Reordering

**Algorithm Requirement (Section 82, 149):**
- Reorder packets by sequence number before assembly
- Use wrap-aware comparison (circular sequence space)
- Wait for out-of-order packets to arrive

**Current Implementation:**
- ❌ No packet reordering
- ❌ Packets are processed in arrival order, not sequence order
- ❌ Out-of-order packets cause immediate frame corruption

**Impact:**
- **Out-of-order packet corruption**: If packet N+1 arrives before packet N, the frame is assembled incorrectly
- **FU fragment errors**: Fragmented NAL units may be assembled in wrong order
- **Missing reordering window**: No wait period for late packets

### 3. Reordering Window / Timeout

**Algorithm Requirement (Section 83, 102-104, 143):**
- Wait 20-80ms (configurable) for late packets before finalizing
- Implement frame finalize timeout (120ms default)
- Force finalize after timeout even if marker bit missing

**Current Implementation:**
- ❌ No reordering window - processes immediately
- ❌ No timeout-based finalization
- ❌ No wait for late packets

**Impact:**
- **Premature finalization**: Frames finalized before all packets arrive
- **Lost packets**: Late packets that arrive within reorder window are discarded
- **No recovery**: Cannot recover from temporary network jitter

### 4. Per-SSRC Frame Tracking

**Algorithm Requirement (Section 54, 116-117):**
- Maintain separate frame assemblies per SSRC
- Track multiple frames per SSRC (by timestamp)
- Handle SSRC changes gracefully

**Current Implementation:**
- ⚠️ SSRC tracking exists but not used for frame isolation
- ❌ Single frame buffer per decoder (not per SSRC per timestamp)
- ❌ Cannot handle multiple concurrent frames

**Impact:**
- **SSRC collision issues**: If SSRC changes, old frames may be mixed with new
- **Concurrent frame confusion**: Cannot handle back-to-back frames properly
- **No multi-stream support**: Cannot handle multiple streams properly

### 5. Fragment Validation

**Algorithm Requirement (Section 86, 158):**
- Validate that all FU fragments exist (start + middle + end) before reassembly
- Wait for timeout if start fragment missing
- Mark NAL as corrupted if fragments missing

**Current Implementation:**
- ⚠️ FU fragments are appended directly
- ❌ No validation that all fragments exist
- ❌ No waiting for missing fragments
- ❌ May assemble incomplete fragments

**Impact:**
- **Incomplete NAL units**: FU fragments missing middle or end cause corruption
- **Missing start fragments**: If start fragment is lost, middle/end fragments are appended anyway
- **Silent corruption**: Frame appears complete but is actually missing data

### 6. Duplicate Packet Detection

**Algorithm Requirement (Section 115, 230):**
- Detect and discard duplicate packets using (SSRC, seq) cache
- Prevent duplicate packets from corrupting frames

**Current Implementation:**
- ❌ No duplicate detection in decoder
- ⚠️ JitterBuffer has duplicate detection but is not used in main flow

**Impact:**
- **Duplicate corruption**: Same packet processed twice, corrupting frame
- **Buffer bloat**: Duplicate packets waste memory

### 7. Resource Limits

**Algorithm Requirement (Section 91, 106, 259):**
- Enforce max packets per frame
- Enforce max frame bytes
- Cap memory for outstanding frames
- Drop non-key frames when overloaded

**Current Implementation:**
- ❌ No limits on frame size
- ❌ No limits on packets per frame
- ❌ No memory capping
- ❌ Potential DoS vulnerability

**Impact:**
- **Memory exhaustion**: Unlimited frame size can crash system
- **DoS vulnerability**: Malicious sender can exhaust memory
- **No back-pressure**: Cannot prioritize keyframes under load

### 8. Marker Bit Handling for Timestamp Changes

**Algorithm Requirement (Section 85, 243):**
- When timestamp changes, finalize previous frame after reorder window
- Use marker bit OR timestamp change OR timeout to detect frame end

**Current Implementation:**
- ⚠️ Checks timestamp change during fragmentation
- ❌ No reorder window wait when timestamp changes
- ❌ Immediate finalization may miss late packets

**Impact:**
- **Late packet loss**: Packets arriving after timestamp change are discarded
- **Incomplete frames**: Previous frame finalized too early

### 9. Proper Sequence Number Ordering in Fragments

**Algorithm Requirement (Section 158):**
- When reassembling FU fragments, ensure sequence numbers are consecutive
- Validate that no fragments are missing between start and end

**Current Implementation:**
- ⚠️ FU fragments appended directly
- ❌ No validation of sequence continuity within fragments
- ❌ Missing middle fragments not detected

**Impact:**
- **Fragment gaps**: Missing middle fragments cause NAL corruption
- **Silent errors**: Frame appears valid but has missing data

## What's Currently Working

✅ **Basic SSRC detection** - SSRC changes are detected and tracked
✅ **Sequence wraparound handling** - Sequence numbers handle 16-bit wrap
✅ **Basic FU-A processing** - Fragments are collected (but not validated)
✅ **Marker bit detection** - Used to finalize frames
✅ **Packet loss detection** - Gaps in sequence are detected (but not handled properly)

## Critical Issues Summary

1. **No Frame Assembly Structure**: Packets are appended to a single buffer without per-frame tracking
2. **No Reordering**: Packets processed in arrival order, not sequence order
3. **No Timeout Logic**: No wait for late packets or timeout-based finalization
4. **Fragment Validation Missing**: FU fragments assembled without verifying all fragments exist
5. **No Resource Limits**: Unbounded frame size creates DoS risk

## Example Problem Scenario

```
Frame requires 4 packets: seq 100, 101, 102, 103

Arrival order: 100, 103, 101, 102 (packet 102 arrives late)

Current behavior:
- Seq 100: appended
- Seq 103: appended immediately (out of order)
- Seq 101: appended
- Seq 102: arrives late, but frame already finalized

Result: Frame has packets [100, 103, 101] - CORRUPTED

Correct behavior (per algorithm):
- Seq 100: stored in FrameAssembly for timestamp X
- Seq 103: stored, but wait for seq 101, 102
- Seq 101: stored, wait for seq 102
- Seq 102: arrives, now reorder to [100, 101, 102, 103], then finalize
```

## Recommended Fix Priority

### Priority 1 (Critical):
1. Implement per-timestamp FrameAssembly structure
2. Add packet reordering with sequence-based ordering
3. Add reordering window (40ms default)

### Priority 2 (High):
4. Add timeout-based finalization (120ms)
5. Validate FU fragments before assembly
6. Add duplicate packet detection

### Priority 3 (Medium):
7. Add resource limits (max packets/frame, max bytes)
8. Improve SSRC handling for concurrent frames
9. Add proper fragment sequence validation

## Architecture Change Required

The decoder needs to be restructured:

```
Current:
Packet → ProcessPacket() → Append to buffer → Check marker → Finalize

Required:
Packet → Store in FrameAssembly[timestamp] by sequence
      → Wait for reorder window
      → Reorder by sequence
      → Validate fragments
      → Finalize when complete/timeout
```

This requires significant refactoring but is essential for correct frame assembly.

