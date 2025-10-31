# Missing Components for Proper JPEG Conversion

## Overview

Comparing the current implementation with the algorithm document (ALGORITHM.md), there are several critical missing components that prevent optimal JPEG conversion from RTP packets.

## Critical Missing Components

### 1. Frame Queue (Thread-Safe Buffer)

**Algorithm Requirement (Section 29-32):**
- Thread-safe queue between frame reassembler and JPEG converter
- Bounded size to provide back-pressure
- Non-blocking push with drop policy (prefer dropping non-keyframes when full)

**Current Implementation:**
- ❌ No frame queue exists
- ❌ Frames are saved synchronously in the receiver thread
- ❌ No back-pressure mechanism

**Impact:**
- Receiver thread can be blocked waiting for JPEG conversion
- No buffering means dropped frames during high load
- Cannot decouple frame reception from JPEG processing

### 2. Separate JPEG Converter Thread

**Algorithm Requirement (Section 34-40):**
- Dedicated thread(s) that pull frames from the queue
- Processes frames asynchronously without blocking receiver
- Handles decode errors gracefully

**Current Implementation:**
- ❌ JPEG conversion happens synchronously in `SaveFrame()`
- ❌ Called directly from the receiver thread
- ❌ Blocks packet reception while ffmpeg runs

**Impact:**
- Violates algorithm principle: "Never performs heavy CPU work or disk I/O" in receiver thread
- Slow JPEG conversion can cause packet loss
- No parallelism - only one frame decoded at a time

### 3. Atomic File Writes

**Algorithm Requirement (Section 187):**
- Write to temporary file first
- Rename to final location after successful write
- Prevents partial/corrupted JPEG files from being read

**Current Implementation:**
- ❌ Direct write to final file location
- ❌ No atomic write pattern

**Impact:**
- Potential for partial/corrupted JPEG files if process crashes
- Other processes might read incomplete files

### 4. Better Metrics and Error Handling

**Algorithm Requirement (Section 189):**
- Report successful saves, decode errors, average time
- Track metrics for tuning

**Current Implementation:**
- ⚠️ Basic error logging exists
- ❌ No detailed metrics for decode performance
- ❌ No tracking of conversion time

## What's Currently Working

✅ **Keyframe-only decoding** - Fixed to only decode IDR frames (self-contained)
✅ **SPS/PPS prepending** - Codec parameters are prepended before decoding
✅ **Error handling** - Basic error logging for decode failures
✅ **Frame validation** - Corrupted frames are skipped

## Recommended Implementation Plan

### Phase 1: Add Frame Queue
1. Create a bounded channel for frame queue
2. Make `SaveFrame()` non-blocking (push to queue)
3. Implement drop policy for non-keyframes when queue is full

### Phase 2: Add JPEG Converter Goroutine
1. Create dedicated goroutine(s) that consume from queue
2. Move `decodeFrameToJPEG()` call to converter thread
3. Add graceful shutdown mechanism

### Phase 3: Atomic File Writes
1. Write JPEG to temp file first (`filename.jpg.tmp`)
2. Rename after successful write
3. Clean up temp files on startup

### Phase 4: Enhanced Metrics
1. Track conversion time per frame
2. Track queue depth and drop counts
3. Report conversion success/failure rates

## Quick Fix Applied

The immediate fix to decode only keyframes has been applied. This prevents attempts to decode P-frames which cannot be decoded standalone. However, the architectural improvements above should be implemented for production-quality operation.

