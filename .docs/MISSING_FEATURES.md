# Missing Features & Implementation Roadmap

## 🎯 Priority Levels
- 🔴 **P0**: Critical for production (blocking)
- 🟠 **P1**: High priority (important for stability)
- 🟡 **P2**: Medium priority (nice to have)
- 🟢 **P3**: Low priority (future enhancement)

---

## 📋 Control Plane (RTSP Layer)

### 🔴 P0 - Critical
- [ ] **Authentication Support** (Digest & Basic Auth)
  - Parse 401 responses
  - Compute Digest authentication headers
  - Support Basic auth with credentials in URL
  - Handle auth realm and nonce
  - Tests: TestDigestAuth, TestBasicAuth, Test401Response

- [ ] **Keep-alive Mechanism**
  - Implement OPTIONS request for keep-alive
  - Parse session timeout from SETUP response
  - Schedule periodic keep-alive (OPTIONS or GET_PARAMETER)
  - Handle session expiry and renewal
  - Tests: TestKeepAlive, TestSessionExpiry, TestOptionsRequest

### 🟠 P1 - High Priority
- [x] **TCP Interleaved Transport** ✅
  - Handle `$`-framed binary data in RTSP stream
  - Parse interleaved channel headers
  - Demultiplex RTP/RTCP from TCP stream
  - Support transport fallback (UDP → TCP)
  - Tests: 7 test functions, 13 subtests - ALL PASSING ✅

- [ ] **Enhanced Error Handling**
  - Handle 400 (Bad Request)
  - Handle 454 (Session Not Found)
  - Handle 461 (Unsupported Transport)
  - Handle 500 (Internal Server Error)
  - Handle 3xx redirects (Location header)
  - Tests: TestErrorResponses, TestRedirect, Test3xxHandling

- [ ] **OPTIONS Request**
  - Discover server capabilities
  - Support keep-alive via OPTIONS
  - Parse Public header for supported methods
  - Tests: TestOptionsDiscovery, TestServerCapabilities

### 🟡 P2 - Medium Priority
- [ ] **Session Management Improvements**
  - Track session state (INIT, READY, PLAYING, RECORDING)
  - Validate state transitions
  - Handle session reuse across reconnects
  - Auto-resume on network drop
  - Tests: TestSessionState, TestSessionReuse, TestAutoResume

- [ ] **RTSP 2.0 Support**
  - Negotiate RTSP version
  - Handle version-specific features
  - Backward compatibility with 1.0
  - Tests: TestRTSP2Negotiation

### 🟢 P3 - Low Priority
- [ ] **RTSPS (RTSP over TLS)**
  - TLS handshake
  - Certificate validation
  - Secure credential transmission
  - Tests: TestRTSPSConnection, TestTLSHandshake

---

## 📡 Data Plane (RTP Layer)

### 🔴 P0 - Critical
- [ ] **Jitter Buffer**
  - Configurable buffer size (100-500ms)
  - Packet reordering by sequence number
  - Delayed packet handling
  - Buffer overflow/underflow strategies
  - Tests: TestJitterBuffer, TestPacketReordering, TestBufferOverflow

- [ ] **Sequence Number Handling**
  - Track RTP sequence numbers
  - Detect out-of-order packets
  - Handle 16-bit wraparound (0 → 65535 → 0)
  - Detect duplicate packets
  - Tests: TestSequenceTracking, TestSequenceWraparound, TestDuplicateDetection

- [ ] **Packet Loss Detection**
  - Detect gaps in sequence numbers
  - Calculate packet loss rate
  - Request keyframe on significant loss
  - Tests: TestPacketLoss, TestLossDetection, TestGapDetection

### 🟠 P1 - High Priority
- [ ] **Timestamp Management**
  - Handle 32-bit RTP timestamp wraparound
  - Detect timestamp jumps (camera restart)
  - Maintain presentation timeline
  - Tests: TestTimestampWraparound, TestTimestampJump, TestPresentationTime

- [ ] **Clock Synchronization**
  - Map RTP timestamps to wallclock time
  - Handle clock drift between sender/receiver
  - Resync on timestamp discontinuities
  - Tests: TestClockSync, TestClockDrift, TestResync

### 🟡 P2 - Medium Priority
- [ ] **Network Resilience**
  - Retry on packet read timeout
  - Exponential backoff on connection failure
  - Graceful degradation on high packet loss
  - Tests: TestRetryMechanism, TestBackoff, TestGracefulDegradation

- [ ] **Adaptive Buffering**
  - Dynamic jitter buffer sizing
  - Auto-tune based on network variance
  - Balance latency vs smoothness
  - Tests: TestAdaptiveBuffer, TestAutoTune

### 🟢 P3 - Low Priority
- [ ] **FEC (Forward Error Correction)**
  - Parse FEC packets
  - Reconstruct lost packets
  - RFC 5109 support
  - Tests: TestFEC, TestPacketReconstruction

---

## 🔁 RTCP Handling

### 🔴 P0 - Critical
- [ ] **Sender Reports (SR) Processing**
  - Parse RTCP SR packets
  - Extract NTP ↔ RTP timestamp mapping
  - Calculate sender statistics
  - Tests: TestSenderReport, TestNTPMapping, TestSRParsing

- [ ] **Receiver Reports (RR) Generation**
  - Calculate packet loss fraction
  - Compute jitter estimate
  - Send RR packets to server
  - Calculate round-trip time
  - Tests: TestReceiverReport, TestJitterCalculation, TestRTT

### 🟠 P1 - High Priority
- [ ] **RTCP Packet Types**
  - Handle SDES (Source Description)
  - Handle BYE packets (clean shutdown)
  - Handle APP packets (application-specific)
  - Tests: TestSDES, TestBYE, TestAPP

- [ ] **A/V Synchronization**
  - Use NTP time from SR for sync
  - Align audio and video streams
  - Maintain lip-sync
  - Tests: TestAVSync, TestLipSync, TestMultiStreamSync

### 🟡 P2 - Medium Priority
- [ ] **RTCP Statistics & Telemetry**
  - Aggregate packet loss metrics
  - Track bitrate variations
  - Monitor jitter trends
  - Export Prometheus metrics
  - Tests: TestMetrics, TestStatistics, TestTelemetry

---

## ⚙️ Transport & Resilience

### 🔴 P0 - Critical
- [ ] **Connection Recovery**
  - Detect network drops
  - Retry RTSP connection
  - Resume PLAY with same session
  - Handle session invalidation
  - Tests: TestConnectionRecovery, TestRetry, TestSessionResume

### 🟠 P1 - High Priority
- [ ] **Transport Fallback**
  - Try UDP first
  - Fallback to TCP on failure/firewall
  - Detect UDP port blocking
  - Tests: TestTransportFallback, TestUDPBlocking

- [ ] **Timeout Configuration**
  - Configurable read/write timeouts
  - Connection timeout vs idle timeout
  - Graceful timeout handling
  - Tests: TestTimeoutConfig, TestIdleTimeout

### 🟡 P2 - Medium Priority
- [ ] **NAT Traversal**
  - STUN support for NAT detection
  - Hole punching for symmetric NAT
  - Tests: TestNATTraversal, TestSTUN

---

## 🎞️ Media Decoding

### 🟠 P1 - High Priority
- [ ] **SDP Parser Improvements**
  - Robust SDP parsing (handle malformed)
  - Extract codec parameters (SPS/PPS)
  - Parse multiple media tracks
  - Handle dynamic payload types
  - Tests: TestSDPParser, TestMalformedSDP, TestMultiTrack

- [ ] **FU-A Reassembly Improvements**
  - Handle FU-A fragmentation edge cases
  - Detect incomplete fragments
  - Timeout incomplete assemblies
  - Tests: TestFUAEdgeCases, TestFragmentTimeout

### 🟡 P2 - Medium Priority
- [ ] **Keyframe Management**
  - Detect I-frames vs P/B frames
  - Wait for keyframe after loss
  - Request keyframe via RTCP FIR
  - Tests: TestKeyframeDetection, TestKeyframeRequest

- [ ] **H.265/HEVC Support**
  - Parse H.265 NAL units
  - Handle VPS/SPS/PPS
  - Support fragmentation
  - Tests: TestH265Decoder, TestHEVCFragmentation

---

## 🧠 Testing & Quality

### 🔴 P0 - Critical
- [ ] **Integration Tests**
  - End-to-end RTSP flow test
  - Mock RTSP server for testing
  - Network condition simulation
  - Tests: TestE2E, TestMockServer, TestNetworkConditions

### 🟠 P1 - High Priority
- [ ] **Benchmark Tests**
  - RTP packet parsing performance
  - Jitter buffer throughput
  - Memory allocation profiling
  - Tests: BenchmarkRTPParsing, BenchmarkJitterBuffer

- [ ] **Fuzz Testing**
  - Fuzz RTSP response parser
  - Fuzz RTP packet parser
  - Fuzz SDP parser
  - Tests: FuzzRTSPResponse, FuzzRTPPacket

### 🟡 P2 - Medium Priority
- [ ] **Chaos Testing**
  - Random packet drops
  - Variable latency injection
  - Out-of-order packet delivery
  - Tests: TestChaos, TestPacketDrop, TestLatencyVariation

---

## 📚 Documentation

### 🟠 P1 - High Priority
- [ ] **Architecture Diagrams**
  - End-to-end RTSP/RTP/RTCP flow diagram
  - Jitter buffer architecture
  - State machine diagrams
  - Sequence diagrams

- [ ] **API Documentation**
  - Godoc for all exported functions
  - Usage examples
  - Configuration guide
  - Tests: TestExamples (runnable docs)

### 🟡 P2 - Medium Priority
- [ ] **Performance Guide**
  - Tuning jitter buffer
  - Optimizing for low latency
  - Memory optimization tips

- [ ] **Troubleshooting Guide**
  - Common issues & solutions
  - Debug logging setup
  - Network analysis with Wireshark

---

## 📊 Progress Tracking

### Phase 1: Core Stability (P0) ✅ COMPLETE
- [x] Authentication ✅
- [x] Keep-alive ✅
- [x] Jitter Buffer ✅
- [x] Sequence Handling ✅
- [x] Packet Loss Detection ✅
- [x] RTCP SR/RR ✅
- [x] Connection Recovery ✅
- [x] Integration Tests ✅

### Phase 2: Production Ready (P1) ✅ COMPLETE
- [x] TCP Interleaved ✅
- [x] Enhanced Error Handling ✅
- [x] Timestamp Management ✅
- [x] A/V Sync ✅
- [x] Transport Fallback ✅
- [x] SDP Improvements ✅

### Phase 3: Advanced Features (P2)
- [ ] Adaptive Buffering
- [ ] Session State Machine
- [ ] Network Resilience
- [ ] Statistics & Telemetry

### Phase 4: Future (P3)
- [ ] RTSPS
- [ ] FEC
- [ ] NAT Traversal
- [ ] H.265 Support

---

## 🎯 Implementation Notes

### TDD Workflow
1. **Red**: Write failing test
2. **Green**: Implement minimal code to pass
3. **Refactor**: Clean up and optimize
4. **Repeat**: Move to next feature

### Code Quality Standards
- Minimum 80% code coverage
- All exported functions must have tests
- Edge cases must be tested
- Integration tests for critical paths
- Benchmark tests for performance-critical code

---

**Last Updated**: 2025-10-30


