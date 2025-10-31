# RTSP Client Architecture

## Overview

This document describes the architecture of the production-ready RTSP client built in Go. The client is designed to connect to RTSP streams, receive RTP packets, decode H.264 video frames, and save them to disk with timestamp-based naming.

## Design Principles

1. **Test-Driven Development (TDD)**: All components have comprehensive unit tests written before implementation
2. **Clean Architecture**: Clear separation of concerns with well-defined interfaces
3. **Production-Ready**: Robust error handling, graceful shutdown, and performance monitoring
4. **Maintainability**: Clean, documented code following Go best practices

## Component Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Main Application                     │
│                  (cmd/rtsp-client)                      │
└────────────┬────────────────────────────────────────────┘
             │
             ├─────────────────────────────────────────────┐
             │                                             │
             ▼                                             ▼
┌────────────────────────┐                   ┌────────────────────────┐
│   Configuration        │                   │   Signal Handler       │
│   (internal/config)    │                   │   (Graceful Shutdown)  │
└────────────────────────┘                   └────────────────────────┘
             │
             ▼
┌────────────────────────┐
│    RTSP Client         │
│    (pkg/rtsp)          │
│                        │
│  - Connect             │
│  - DESCRIBE            │
│  - SETUP               │
│  - PLAY                │
│  - TEARDOWN            │
└────────┬───────────────┘
         │
         │ RTP Packets
         ▼
┌────────────────────────┐
│    RTP Parser          │
│    (pkg/rtp)           │
│                        │
│  - Parse Headers       │
│  - Extract Payload     │
│  - Validate Packets    │
└────────┬───────────────┘
         │
         │ Parsed Packets
         ▼
┌────────────────────────┐
│   H.264 Decoder        │
│   (pkg/decoder)        │
│                        │
│  - NAL Unit Assembly   │
│  - FU-A Defragment     │
│  - Frame Detection     │
└────────┬───────────────┘
         │
         │ Complete Frames
         ▼
┌────────────────────────┐
│   Frame Storage        │
│   (pkg/storage)        │
│                        │
│  - Timestamp Naming    │
│  - File I/O            │
│  - Statistics          │
└────────────────────────┘
```

## Package Descriptions

### cmd/rtsp-client
Main application entry point that orchestrates all components:
- Parses command-line arguments
- Initializes all components
- Handles graceful shutdown on SIGINT/SIGTERM
- Provides periodic statistics reporting
- Manages the main packet processing loop

### internal/config
Configuration management:
- Command-line flag parsing
- Configuration validation
- Default value handling

### pkg/rtsp
RTSP protocol implementation:
- TCP connection management to RTSP server
- RTSP request/response handling (DESCRIBE, SETUP, PLAY, TEARDOWN)
- Session management
- UDP listener setup for RTP/RTCP
- Connection timeout handling

### pkg/rtp
RTP packet parsing:
- RTP header parsing (RFC 3550)
- Payload extraction
- Sequence number tracking
- Timestamp extraction
- Marker bit detection

### pkg/decoder
H.264 video decoder:
- NAL (Network Abstraction Layer) unit handling
- FU-A (Fragmentation Unit A) reassembly
- Frame boundary detection
- Keyframe identification (IDR frames)
- Annex B byte stream formatting

### pkg/storage
Frame storage management:
- Timestamp-based filename generation
- Thread-safe file I/O
- Storage statistics tracking (frames, keyframes, bytes)
- Directory management

## Data Flow

1. **Connection Establishment**
   ```
   Main → Config → RTSP Client → RTSP Server
   ```

2. **RTSP Handshake**
   ```
   DESCRIBE → SDP Response
   SETUP    → Session ID + Transport Parameters
   PLAY     → Start Streaming
   ```

3. **Packet Processing**
   ```
   UDP Socket → RTP Parser → H.264 Decoder → Frame Storage
   ```

4. **Graceful Shutdown**
   ```
   Signal Handler → Cancel Context → TEARDOWN → Close Connections
   ```

## Error Handling

### Connection Errors
- Retry mechanism not implemented (fail fast)
- Clear error messages for connection failures
- Timeout handling for all network operations

### Packet Errors
- Silently skip malformed packets
- Count consecutive errors
- Abort after threshold (100 errors)
- Continue on timeout errors

### File I/O Errors
- Log and continue on individual frame save failures
- Fatal error if output directory cannot be created

## Concurrency

### Main Goroutine
- Packet reading and processing
- Frame decoding and saving
- Signal handling

### Statistics Goroutine
- Periodic statistics reporting (every 5 seconds)
- Non-blocking, context-aware

### Thread Safety
- Storage uses mutex for statistics updates
- Decoder is not thread-safe (single goroutine use only)
- RTSP client is not thread-safe (single goroutine use only)

## Performance Considerations

### Memory Management
- Packet buffers: 2KB per read
- Frame buffers: Dynamic, released after save
- Decoder buffer: Reused, grows as needed

### I/O Optimization
- Buffered reading from network
- Direct file writes (no buffering)
- Minimal memory copies

### CPU Usage
- Single-threaded processing (sufficient for single stream)
- No unnecessary copying
- Efficient byte operations

## Testing Strategy

### Unit Tests
- All packages have comprehensive unit tests
- Mock data for RTSP responses
- Edge cases covered (empty payloads, invalid data)
- Error conditions tested

### Integration Tests
- End-to-end flow testing
- Real RTSP server required (opt-in via build tag)
- Performance testing under load

### Test Coverage
```bash
go test -cover ./...
```

## Extension Points

### Adding New Codecs
Implement the decoder interface in `pkg/decoder/`:
```go
type Decoder interface {
    ProcessPacket(*rtp.Packet) *Frame
    Reset()
}
```

### Adding New Storage Backends
Implement the storage interface in `pkg/storage/`:
```go
type Storage interface {
    SaveFrame(*decoder.Frame) error
    GetStats() StorageStats
    Close() error
}
```

### Adding Authentication
Extend `pkg/rtsp/client.go` to support:
- Basic authentication
- Digest authentication
- Token-based authentication

## Security Considerations

### Network
- No input validation bypass
- Timeout on all network operations
- Limited buffer sizes to prevent memory exhaustion

### File System
- Safe filename generation (timestamp-based, no user input)
- Directory creation with safe permissions (0755)
- File creation with safe permissions (0644)

### Resource Limits
- Maximum error count to prevent infinite loops
- Context-based cancellation
- Proper resource cleanup on shutdown

## Future Enhancements

1. **Reconnection Logic**: Automatic reconnection on connection loss
2. **Multiple Streams**: Support for multiple concurrent streams
3. **Frame Buffering**: In-memory frame buffer for smoother processing
4. **Metrics Export**: Prometheus metrics endpoint
5. **Configuration File**: YAML/JSON configuration file support
6. **Authentication**: Full RFC 2617 digest authentication
7. **RTCP Support**: Full RTCP implementation for stream quality monitoring
8. **HLS Output**: Convert frames to HLS segments for web playback


