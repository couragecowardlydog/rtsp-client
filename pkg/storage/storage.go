package storage

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/rtsp-client/pkg/decoder"
	"github.com/rtsp-client/pkg/logger"
	"github.com/rtsp-client/pkg/rtp"
)

const (
	// MetadataQueueSize10MB is the queue size for 10 MB buffer
	// FrameWithTimestamp is approximately 32 bytes (pointer + uint32 + int64 + padding)
	// 10 MB = 10 * 1024 * 1024 bytes = 10,485,760 bytes
	// 10,485,760 / 32 ≈ 327,680 items
	// Using 300,000 for safety margin
	MetadataQueueSize10MB = 300000
)

var (
	// ErrNilFrame indicates a nil frame was provided
	ErrNilFrame = errors.New("nil frame provided")
	// ErrInvalidOutputDir indicates the output directory is invalid
	ErrInvalidOutputDir = errors.New("invalid output directory")
)

// StorageStats holds statistics about stored frames
type StorageStats struct {
	TotalFrames     int64
	KeyFrames       int64
	CorruptedFrames int64
	TotalBytes      int64
}

// FrameWithTimestamp holds a frame and its timestamp for synchronization
type FrameWithTimestamp struct {
	Frame     *decoder.Frame
	Timestamp uint32
	SeqNumber int64 // Sequential number for matching decoded frames
}

// ContinuousDecoder manages a long-running FFmpeg decoder session
// This matches the documentation approach: "Start your decoder once and continuously feed it frames"
// This allows decoding P-frames (not just I-frames) by maintaining frame history
type ContinuousDecoder struct {
	ffmpegPath     string
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout         io.ReadCloser
	stderr         io.ReadCloser
	running        bool
	mu             sync.Mutex
	
	// Frame synchronization
	frameQueue      chan FrameWithTimestamp
	decodedQueue    chan DecodedFrame
	metadataQueue   chan FrameWithTimestamp // Queue for frame metadata matching
	jpegDir         string  // Directory for JPEG images
	corruptedJpegDir string  // Directory for corrupted JPEG images
	spsNAL          []byte
	ppsNAL          []byte
	
	// Timestamp conversion function (RTP timestamp -> Unix epoch nanoseconds)
	timestampConverter func(uint32) int64
	
	// Statistics
	decodedCount    int64
	failedCount     int64
	seqNumber       int64
	initialized     bool
}

// DecodedFrame represents a decoded image frame ready to save
type DecodedFrame struct {
	JPEGData    []byte
	Timestamp   uint32
	IsCorrupted bool
}

// NewContinuousDecoder creates a new continuous decoder session
// timestampConverter: function to convert RTP timestamp to Unix epoch nanoseconds (can be nil)
func NewContinuousDecoder(ffmpegPath, jpegDir, corruptedJpegDir string, spsNAL, ppsNAL []byte, timestampConverter func(uint32) int64) (*ContinuousDecoder, error) {
	logger.Info("[ContinuousDecoder] Initializing continuous decoder session")
	logger.Debug("[ContinuousDecoder] Configuration: jpegDir=%s, corruptedJpegDir=%s", jpegDir, corruptedJpegDir)
	logger.Debug("[ContinuousDecoder] SPS size: %d bytes, PPS size: %d bytes", len(spsNAL), len(ppsNAL))
	
	cd := &ContinuousDecoder{
		ffmpegPath:        ffmpegPath,
		jpegDir:           jpegDir,
		corruptedJpegDir:  corruptedJpegDir,
		spsNAL:            spsNAL,
		ppsNAL:            ppsNAL,
		frameQueue:        make(chan FrameWithTimestamp, 100), // Buffer up to 100 frames
		decodedQueue:      make(chan DecodedFrame, 100),
		metadataQueue:     make(chan FrameWithTimestamp, MetadataQueueSize10MB), // Frame metadata for matching (10 MB buffer)
		timestampConverter: timestampConverter,
		initialized:       false,
	}
	
	logger.Info("[ContinuousDecoder] Starting FFmpeg process...")
	if err := cd.start(); err != nil {
		logger.Error("[ContinuousDecoder] Failed to start: %v", err)
		return nil, fmt.Errorf("failed to start continuous decoder: %w", err)
	}
	
	logger.Info("[ContinuousDecoder] FFmpeg process started successfully")
	logger.Debug("[ContinuousDecoder] Starting goroutines: feedFrames, receiveFrames, saveDecodedFrames")
	
	// Start goroutines for processing
	go cd.feedFrames()
	go cd.receiveFrames()
	go cd.saveDecodedFrames()
	
	logger.Info("[ContinuousDecoder] Continuous decoder fully initialized and ready")
	
	return cd, nil
}

// start starts the FFmpeg decoder process
func (cd *ContinuousDecoder) start() error {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	
	if cd.running {
		return nil
	}
	
	// FFmpeg command: read H.264 from stdin, output JPEG frames to stdout
	// -f h264: input format is raw H.264 Annex B
	// -i pipe:0: read from stdin
	// -f image2pipe: output format as image sequence
	// -vcodec mjpeg: encode as MJPEG (JPEG frames)
	// -vsync 0: don't duplicate or drop frames
	// pipe:1: write to stdout
	cmd := exec.Command(cd.ffmpegPath,
		"-loglevel", "error",      // Only show errors
		"-f", "h264",               // Input format: H.264 Annex B
		"-i", "pipe:0",             // Read from stdin
		"-f", "image2pipe",         // Output as image sequence
		"-vcodec", "mjpeg",         // Encode as MJPEG/JPEG
		"-q:v", "2",                // High quality JPEG
		"-vsync", "0",              // Don't duplicate/drop frames
		"pipe:1",                   // Write to stdout
	)
	
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}
	
	cd.cmd = cmd
	cd.stdin = stdin
	cd.stdout = stdout
	cd.stderr = stderr
	cd.running = true
	
	// Write SPS/PPS to initialize decoder
	if len(cd.spsNAL) > 0 && len(cd.ppsNAL) > 0 {
		logger.Debug("[ContinuousDecoder] Writing SPS (%d bytes) and PPS (%d bytes) to initialize decoder", len(cd.spsNAL), len(cd.ppsNAL))
		if _, err := cd.stdin.Write(cd.spsNAL); err != nil {
			logger.Error("[ContinuousDecoder] Failed to write SPS: %v", err)
			return fmt.Errorf("failed to write SPS: %w", err)
		}
		if _, err := cd.stdin.Write(cd.ppsNAL); err != nil {
			logger.Error("[ContinuousDecoder] Failed to write PPS: %v", err)
			return fmt.Errorf("failed to write PPS: %w", err)
		}
		cd.initialized = true
		logger.Info("[ContinuousDecoder] SPS/PPS written, decoder initialized")
	} else {
		logger.Warn("[ContinuousDecoder] No SPS/PPS provided, decoder may not work correctly")
	}
	
	logger.Debug("[ContinuousDecoder] FFmpeg process ready, PID: %d", cd.cmd.Process.Pid)
	return nil
}

// FeedFrame feeds a frame to the continuous decoder
func (cd *ContinuousDecoder) FeedFrame(frame *decoder.Frame) {
	if !cd.running {
		logger.Warn("[ContinuousDecoder] FeedFrame called but decoder not running, dropping frame (timestamp: %d)", frame.Timestamp)
		return
	}
	
	cd.mu.Lock()
	seqNum := cd.seqNumber
	cd.seqNumber++
	frameQueueLen := len(cd.frameQueue)
	cd.mu.Unlock()
	
	logger.Debug("[ContinuousDecoder] Feeding frame: seq=%d, timestamp=%d, size=%d bytes, isKey=%t, corrupted=%t, queueLen=%d",
		seqNum, frame.Timestamp, len(frame.Data), frame.IsKey, frame.IsCorrupted, frameQueueLen)
	
	select {
	case cd.frameQueue <- FrameWithTimestamp{
		Frame:     frame,
		Timestamp: frame.Timestamp,
		SeqNumber: seqNum,
	}:
		// Frame queued successfully
	default:
		// Queue full, drop frame (non-blocking)
		logger.Warn("[ContinuousDecoder] Frame queue full (%d/100), dropping frame (seq=%d, timestamp=%d)",
			frameQueueLen, seqNum, frame.Timestamp)
	}
}

// receiveFrames continuously receives decoded JPEG frames from FFmpeg stdout
func (cd *ContinuousDecoder) receiveFrames() {
	logger.Debug("[ContinuousDecoder:receiveFrames] Goroutine started")
	buffer := make([]byte, 4096)
	jpegBuffer := make([]byte, 0, 1024*1024) // 1MB initial capacity
	jpegFrameCount := 0
	readCount := 0
	
	for {
		if !cd.running {
			logger.Debug("[ContinuousDecoder:receiveFrames] Decoder stopped, exiting receiveFrames goroutine")
			return
		}
		
		cd.mu.Lock()
		stdout := cd.stdout
		cd.mu.Unlock()
		
		if stdout == nil {
			logger.Warn("[ContinuousDecoder:receiveFrames] Stdout is nil, exiting")
			return
		}
		
		n, err := stdout.Read(buffer)
		if err != nil {
			if err != io.EOF {
				logger.Error("[ContinuousDecoder:receiveFrames] Error reading from decoder stdout: %v", err)
			} else {
				logger.Debug("[ContinuousDecoder:receiveFrames] EOF reached on stdout (read %d times, decoded %d frames)", readCount, jpegFrameCount)
			}
			break
		}
		
		readCount++
		if readCount%100 == 0 {
			logger.Debug("[ContinuousDecoder:receiveFrames] Read progress: %d reads, buffer size: %d bytes, decoded frames: %d",
				readCount, len(jpegBuffer), jpegFrameCount)
		}
		
		jpegBuffer = append(jpegBuffer, buffer[:n]...)
		
		// Try to find JPEG frame boundaries (JPEG starts with 0xFF 0xD8, ends with 0xFF 0xD9)
		for {
			startIdx := findJPEGStart(jpegBuffer)
			if startIdx == -1 {
				break
			}
			
			endIdx := findJPEGEnd(jpegBuffer[startIdx:])
			if endIdx == -1 {
				// Incomplete JPEG, keep buffering
				logger.Debug("[ContinuousDecoder:receiveFrames] Incomplete JPEG frame detected at offset %d, buffering...", startIdx)
				break
			}
			
			// Extract complete JPEG frame
			jpegFrame := make([]byte, endIdx)
			copy(jpegFrame, jpegBuffer[startIdx:startIdx+endIdx])
			jpegFrameCount++
			
			logger.Debug("[ContinuousDecoder:receiveFrames] Complete JPEG frame #%d found: %d bytes (startIdx=%d, endIdx=%d)",
				jpegFrameCount, len(jpegFrame), startIdx, endIdx)
			
			// Remove processed data from buffer
			jpegBuffer = jpegBuffer[startIdx+endIdx:]
			
			// Update statistics
			cd.mu.Lock()
			cd.decodedCount++
			decodedCount := cd.decodedCount
			cd.mu.Unlock()
			
			decodedQueueLen := len(cd.decodedQueue)
			logger.Debug("[ContinuousDecoder:receiveFrames] Sending decoded frame to queue: size=%d bytes, decodedCount=%d, queueLen=%d/100",
				len(jpegFrame), decodedCount, decodedQueueLen)
			
			// Send decoded frame for saving (will be matched with metadata in saveDecodedFrames)
			select {
			case cd.decodedQueue <- DecodedFrame{
				JPEGData: jpegFrame,
			}:
				logger.Debug("[ContinuousDecoder:receiveFrames] Decoded frame queued successfully (#%d)", jpegFrameCount)
			default:
				logger.Warn("[ContinuousDecoder:receiveFrames] Decoded frame queue full (%d/100), dropping frame #%d",
					decodedQueueLen, jpegFrameCount)
			}
		}
	}
	
	logger.Debug("[ContinuousDecoder:receiveFrames] Exiting (total: %d reads, %d JPEG frames decoded, buffer: %d bytes)",
		readCount, jpegFrameCount, len(jpegBuffer))
}

// saveDecodedFrames saves decoded JPEG frames to disk
func (cd *ContinuousDecoder) saveDecodedFrames() {
	logger.Debug("[ContinuousDecoder:saveDecodedFrames] Goroutine started")
	savedCount := 0
	metadataMatchCount := 0
	metadataMissCount := 0
	
	for decodedFrame := range cd.decodedQueue {
		savedCount++
		
		logger.Debug("[ContinuousDecoder:saveDecodedFrames] Processing decoded frame #%d: size=%d bytes",
			savedCount, len(decodedFrame.JPEGData))
		
		// Get matching frame metadata from queue (FIFO, sequential matching)
		var frameData FrameWithTimestamp
		metadataQueueLen := len(cd.metadataQueue)
		
		select {
		case frameData = <-cd.metadataQueue:
			metadataMatchCount++
			logger.Debug("[ContinuousDecoder:saveDecodedFrames] Metadata matched: timestamp=%d, seq=%d (matchCount=%d, queueLen=%d->%d)",
				frameData.Timestamp, frameData.SeqNumber, metadataMatchCount, metadataQueueLen, len(cd.metadataQueue))
		default:
			metadataMissCount++
			// No metadata available, use fallback
			frameData = FrameWithTimestamp{
				Timestamp: uint32(savedCount), // Fallback to sequential number
			}
			logger.Warn("[ContinuousDecoder:saveDecodedFrames] No metadata available, using fallback timestamp=%d (missCount=%d, queueLen=%d)",
				frameData.Timestamp, metadataMissCount, metadataQueueLen)
		}
		
		decodedFrame.Timestamp = frameData.Timestamp
		decodedFrame.IsCorrupted = frameData.Frame != nil && frameData.Frame.IsCorrupted
		
		if frameData.Frame != nil {
			logger.Debug("[ContinuousDecoder:saveDecodedFrames] Frame info: timestamp=%d, isKey=%t, corrupted=%t",
				decodedFrame.Timestamp, frameData.Frame.IsKey, decodedFrame.IsCorrupted)
		}
		
		// Determine output directory for JPEG files
		var targetDir string
		if decodedFrame.IsCorrupted {
			targetDir = cd.corruptedJpegDir
			logger.Warn("[ContinuousDecoder:saveDecodedFrames] Frame is corrupted, saving to corrupted JPEG directory")
		} else {
			targetDir = cd.jpegDir
		}
		
		// Save JPEG file with NTP timestamp in nanoseconds and RTP timestamp
		// Format: NTPNANOSECONDS.RTPTIMESTAMP.jpg (e.g., 1761897425257387428.1234567890.jpg)
		// This preserves both NTP (wall-clock) time and RTP timestamp for correlation
		var jpgPath string
		var namingFormat string
		if cd.timestampConverter != nil {
			unixNanos := cd.timestampConverter(decodedFrame.Timestamp)
			if unixNanos != 0 {
				// Format: ntpinnanoseconds.rtptimestamp.jpg
				// This preserves full nanosecond precision from RTCP NTP timestamps and RTP timestamp
				if decodedFrame.IsCorrupted {
					jpgPath = filepath.Join(targetDir, fmt.Sprintf("%d.%d_corrupted.jpg", unixNanos, decodedFrame.Timestamp))
				} else {
					jpgPath = filepath.Join(targetDir, fmt.Sprintf("%d.%d.jpg", unixNanos, decodedFrame.Timestamp))
				}
				namingFormat = fmt.Sprintf("NTP (nanoseconds).RTP timestamp: %d.%d", unixNanos, decodedFrame.Timestamp)
			} else {
				// Fallback to RTP timestamp if mapping not available yet
				if decodedFrame.IsCorrupted {
					jpgPath = filepath.Join(targetDir, fmt.Sprintf("%d_corrupted.jpg", decodedFrame.Timestamp))
				} else {
					jpgPath = filepath.Join(targetDir, fmt.Sprintf("%d.jpg", decodedFrame.Timestamp))
				}
				namingFormat = fmt.Sprintf("RTP timestamp (mapping not available): %d", decodedFrame.Timestamp)
			}
		} else {
			// No converter provided, use RTP timestamp
			if decodedFrame.IsCorrupted {
				jpgPath = filepath.Join(targetDir, fmt.Sprintf("%d_corrupted.jpg", decodedFrame.Timestamp))
			} else {
				jpgPath = filepath.Join(targetDir, fmt.Sprintf("%d.jpg", decodedFrame.Timestamp))
			}
			namingFormat = fmt.Sprintf("RTP timestamp (no converter): %d", decodedFrame.Timestamp)
		}
		logger.Debug("[ContinuousDecoder:saveDecodedFrames] Saving JPEG: path=%s, size=%d bytes, naming=%s", jpgPath, len(decodedFrame.JPEGData), namingFormat)
		
		if err := os.WriteFile(jpgPath, decodedFrame.JPEGData, 0644); err != nil {
			logger.Error("[ContinuousDecoder:saveDecodedFrames] Failed to save decoded frame #%d: %v", savedCount, err)
		} else {
			logger.Debug("[ContinuousDecoder:saveDecodedFrames] Successfully saved frame #%d: %s (%d bytes)",
				savedCount, jpgPath, len(decodedFrame.JPEGData))
		}
		
		// Log statistics every 10 frames
		if savedCount%10 == 0 {
			cd.mu.Lock()
			decodedCount := cd.decodedCount
			failedCount := cd.failedCount
			cd.mu.Unlock()
			logger.Debug("[ContinuousDecoder:saveDecodedFrames] Statistics: saved=%d, decoded=%d, failed=%d, metadataMatches=%d, metadataMisses=%d",
				savedCount, decodedCount, failedCount, metadataMatchCount, metadataMissCount)
		}
	}
	
	logger.Debug("[ContinuousDecoder:saveDecodedFrames] Decoded queue closed, exiting (total saved: %d, matches: %d, misses: %d)",
		savedCount, metadataMatchCount, metadataMissCount)
}

// feedFrames continuously feeds frames to FFmpeg stdin
func (cd *ContinuousDecoder) feedFrames() {
	logger.Debug("[ContinuousDecoder:feedFrames] Goroutine started")
	frameCount := 0
	
	for frameData := range cd.frameQueue {
		frameCount++
		
		if !cd.running {
			logger.Debug("[ContinuousDecoder:feedFrames] Decoder stopped, exiting feedFrames goroutine (processed %d frames)", frameCount)
			return
		}
		
		logger.Debug("[ContinuousDecoder:feedFrames] Processing frame seq=%d, timestamp=%d, size=%d bytes",
			frameData.SeqNumber, frameData.Timestamp, len(frameData.Frame.Data))
		
		// Skip corrupted frames to avoid decoder errors
		if frameData.Frame.IsCorrupted {
			logger.Debug("[ContinuousDecoder:feedFrames] Skipping corrupted frame seq=%d, timestamp=%d",
				frameData.SeqNumber, frameData.Timestamp)
			continue
		}
		
		// Send metadata to matching queue (only for frames that will be decoded)
		metadataQueueLen := len(cd.metadataQueue)
		select {
		case cd.metadataQueue <- frameData:
			logger.Debug("[ContinuousDecoder:feedFrames] Metadata queued: seq=%d, timestamp=%d (metadataQueue: %d/%d)",
				frameData.SeqNumber, frameData.Timestamp, metadataQueueLen+1, MetadataQueueSize10MB)
		default:
			logger.Warn("[ContinuousDecoder:feedFrames] Metadata queue full (%d/%d), skipping metadata for seq=%d",
				metadataQueueLen, MetadataQueueSize10MB, frameData.SeqNumber)
		}
		
		cd.mu.Lock()
		stdin := cd.stdin
		cd.mu.Unlock()
		
		if stdin == nil {
			logger.Warn("[ContinuousDecoder:feedFrames] Stdin is nil, skipping frame seq=%d", frameData.SeqNumber)
			continue
		}
		
		// Write frame data to FFmpeg stdin
		bytesWritten, err := stdin.Write(frameData.Frame.Data)
		if err != nil {
			logger.Error("[ContinuousDecoder:feedFrames] Failed to write frame to FFmpeg stdin: seq=%d, bytes=%d/%d, error=%v",
				frameData.SeqNumber, bytesWritten, len(frameData.Frame.Data), err)
			cd.mu.Lock()
			cd.failedCount++
			failedCount := cd.failedCount
			cd.mu.Unlock()
			logger.Debug("[ContinuousDecoder:feedFrames] Failed frame count: %d", failedCount)
			continue
		}
		
		logger.Debug("[ContinuousDecoder:feedFrames] Wrote frame to FFmpeg: seq=%d, timestamp=%d, bytes=%d/%d",
			frameData.SeqNumber, frameData.Timestamp, bytesWritten, len(frameData.Frame.Data))
		
		// Flush to ensure frame is sent
		if flusher, ok := stdin.(interface{ Flush() error }); ok {
			if err := flusher.Flush(); err != nil {
				logger.Warn("[ContinuousDecoder:feedFrames] Flush error: %v", err)
			}
		}
	}
	
	logger.Debug("[ContinuousDecoder:feedFrames] Frame queue closed, exiting (total frames processed: %d)", frameCount)
}

// findJPEGStart finds the start of a JPEG frame (0xFF 0xD8)
func findJPEGStart(data []byte) int {
	for i := 0; i < len(data)-1; i++ {
		if data[i] == 0xFF && data[i+1] == 0xD8 {
			return i
		}
	}
	return -1
}

// findJPEGEnd finds the end of a JPEG frame (0xFF 0xD9)
func findJPEGEnd(data []byte) int {
	for i := 0; i < len(data)-1; i++ {
		if data[i] == 0xFF && data[i+1] == 0xD9 {
			return i + 2 // Include the end marker
		}
	}
	return -1
}

// Stop stops the continuous decoder
func (cd *ContinuousDecoder) Stop() error {
	cd.mu.Lock()
	
	if !cd.running {
		cd.mu.Unlock()
		logger.Warn("[ContinuousDecoder] Stop called but decoder not running")
		return nil
	}
	
	logger.Debug("[ContinuousDecoder] Stopping continuous decoder...")
	logger.Debug("[ContinuousDecoder] Queue states: frameQueue=%d, decodedQueue=%d, metadataQueue=%d",
		len(cd.frameQueue), len(cd.decodedQueue), len(cd.metadataQueue))
	
	cd.running = false
	cd.mu.Unlock()
	
	// Close queues
	logger.Debug("[ContinuousDecoder] Closing queues...")
	close(cd.frameQueue)
	close(cd.decodedQueue)
	close(cd.metadataQueue)
	
	// Close stdin to signal end of input
	if cd.stdin != nil {
		logger.Debug("[ContinuousDecoder] Closing stdin pipe...")
		if err := cd.stdin.Close(); err != nil {
			logger.Warn("[ContinuousDecoder] Error closing stdin: %v", err)
		}
	}
	
	// Wait for process to finish
	if cd.cmd != nil {
		logger.Debug("[ContinuousDecoder] Waiting for FFmpeg process to finish (PID: %d)...", cd.cmd.Process.Pid)
		if err := cd.cmd.Wait(); err != nil {
			cd.mu.Lock()
			decodedCount := cd.decodedCount
			failedCount := cd.failedCount
			cd.mu.Unlock()
			logger.Warn("[ContinuousDecoder] FFmpeg process error: %v (decoded: %d, failed: %d)", err, decodedCount, failedCount)
			return fmt.Errorf("ffmpeg process error: %w", err)
		}
		logger.Debug("[ContinuousDecoder] FFmpeg process exited successfully")
	}
	
	cd.mu.Lock()
	decodedCount := cd.decodedCount
	failedCount := cd.failedCount
	cd.mu.Unlock()
	
	logger.Debug("[ContinuousDecoder] Stopped successfully (decoded: %d frames, failed: %d frames)", decodedCount, failedCount)
	
	return nil
}

// GetStats returns decoder statistics
func (cd *ContinuousDecoder) GetStats() (decoded int64, failed int64) {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	return cd.decodedCount, cd.failedCount
}

// FrameStorage handles saving video frames to disk
type FrameStorage struct {
	outputDir          string
	h264Dir            string  // Directory for H.264 files
	jpegDir            string  // Directory for JPEG images
	corruptedFramesDir string
	corruptedJpegDir   string  // Directory for corrupted JPEG images
	stats              StorageStats
	mu                 sync.RWMutex
	saveAsJPG          bool
	ffmpegPath         string
	spsNAL             []byte
	ppsNAL             []byte
	streamFile         *os.File
	streamFilePath     string
	frameCount         int
	lastSnapshotFrame  int
	snapshotInterval   int // Extract JPG every N frames
	
	// Continuous decoder session (matches documentation approach)
	continuousDecoder *ContinuousDecoder
	useContinuousMode bool // Enable continuous decoder session
	
	// Timestamp mapping for converting RTP timestamps to Unix epoch
	timestampMapper *rtp.TimestampMapper
}

// NewFrameStorage creates a new frame storage handler
func NewFrameStorage(outputDir string) (*FrameStorage, error) {
	return NewFrameStorageWithFormat(outputDir, true)
}

// NewFrameStorageWithFormat creates a new frame storage handler with format option
// Uses continuous decoder by default
func NewFrameStorageWithFormat(outputDir string, saveAsJPG bool) (*FrameStorage, error) {
	return NewFrameStorageWithOptions(outputDir, saveAsJPG, true)
}

// NewFrameStorageWithOptions creates a new frame storage handler with all options
// continuousDecoder: if true, uses continuous decoder session (can decode P-frames)
func NewFrameStorageWithOptions(outputDir string, saveAsJPG bool, continuousDecoder bool) (*FrameStorage, error) {
	if outputDir == "" {
		outputDir = "./frames"
	}

	// Create output directory if it doesn't exist
	logger.Info("[FrameStorage] Creating output directory: %s", outputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create separate directories for H.264 and JPEG files
	h264Dir := filepath.Join(outputDir, "h264")
	logger.Info("[FrameStorage] Creating H.264 directory: %s", h264Dir)
	if err := os.MkdirAll(h264Dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create H.264 directory: %w", err)
	}

	jpegDir := filepath.Join(outputDir, "jpeg")
	logger.Info("[FrameStorage] Creating JPEG directory: %s", jpegDir)
	if err := os.MkdirAll(jpegDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create JPEG directory: %w", err)
	}

	// Create corrupted frames directory
	corruptedFramesDir := filepath.Join(outputDir, "corrupted_frames")
	logger.Info("[FrameStorage] Creating corrupted frames directory: %s", corruptedFramesDir)
	if err := os.MkdirAll(corruptedFramesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create corrupted frames directory: %w", err)
	}

	// Create corrupted JPEG directory
	corruptedJpegDir := filepath.Join(corruptedFramesDir, "jpeg")
	logger.Info("[FrameStorage] Creating corrupted JPEG directory: %s", corruptedJpegDir)
	if err := os.MkdirAll(corruptedJpegDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create corrupted JPEG directory: %w", err)
	}

	logger.Info("[FrameStorage] Directory structure created successfully: %s/ (h264/, jpeg/, corrupted_frames/jpeg/)", outputDir)

	// Find ffmpeg if saving as JPG
	var ffmpegPath string
	if saveAsJPG {
		path, err := exec.LookPath("ffmpeg")
		if err != nil {
			// FFmpeg not found, will save as H.264
			logger.Warn("[FrameStorage] JPEG mode requested but ffmpeg not found. Saving as H.264 only. Install ffmpeg to enable JPEG frame conversion: https://ffmpeg.org/download.html")
			saveAsJPG = false
		} else {
			ffmpegPath = path
			logger.Info("[FrameStorage] JPEG mode enabled - H.264 files will be saved in h264/ folder, JPEG images in jpeg/ folder")
		}
	} else {
		logger.Info("[FrameStorage] H.264 mode - saving frames as .h264 only")
	}

	fs := &FrameStorage{
		outputDir:          outputDir,
		h264Dir:            h264Dir,
		jpegDir:            jpegDir,
		corruptedFramesDir: corruptedFramesDir,
		corruptedJpegDir:   corruptedJpegDir,
		stats:              StorageStats{},
		saveAsJPG:          saveAsJPG,
		ffmpegPath:         ffmpegPath,
		snapshotInterval:   90, // Extract JPG every 90 frames (about 1 per 3 seconds at 30fps)
		useContinuousMode: continuousDecoder,
		timestampMapper:    rtp.NewTimestampMapper(),
	}
	
	if continuousDecoder && saveAsJPG && ffmpegPath != "" {
		logger.Info("[FrameStorage] Continuous decoder mode enabled (can decode all frames including P-frames)")
	} else if saveAsJPG && ffmpegPath != "" {
		logger.Info("[FrameStorage] Frame-by-frame decoder mode (keyframes only)")
	}

	// Create H.264 stream file for continuous writing
	if saveAsJPG && ffmpegPath != "" {
		streamPath := filepath.Join(outputDir, "stream.h264")
		streamFile, err := os.Create(streamPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create stream file: %w", err)
		}
		fs.streamFile = streamFile
		fs.streamFilePath = streamPath
	}

	return fs, nil
}

// SetSPSPPS sets the SPS and PPS NAL units from base64-encoded SDP parameters
func (s *FrameStorage) SetSPSPPS(spsBase64, ppsBase64 string) error {
	// Decode SPS
	if spsBase64 != "" {
		spsData, err := decodeBase64(spsBase64)
		if err != nil {
			return fmt.Errorf("failed to decode SPS: %w", err)
		}
		// Prepend start code
		s.spsNAL = append([]byte{0x00, 0x00, 0x00, 0x01}, spsData...)
	}

	// Decode PPS
	if ppsBase64 != "" {
		ppsData, err := decodeBase64(ppsBase64)
		if err != nil {
			return fmt.Errorf("failed to decode PPS: %w", err)
		}
		// Prepend start code
		s.ppsNAL = append([]byte{0x00, 0x00, 0x00, 0x01}, ppsData...)
	}

	if len(s.spsNAL) > 0 && len(s.ppsNAL) > 0 {
		logger.Info("[FrameStorage] SPS/PPS loaded from SDP for JPG conversion")
		
		// Write SPS and PPS to stream file first
		if s.streamFile != nil {
			s.streamFile.Write(s.spsNAL)
			s.streamFile.Write(s.ppsNAL)
		}
		
		// Initialize continuous decoder if enabled
		if s.useContinuousMode && s.ffmpegPath != "" && s.continuousDecoder == nil {
			logger.Debug("[FrameStorage] Initializing continuous decoder with SPS/PPS...")
			var err error
			s.continuousDecoder, err = NewContinuousDecoder(
				s.ffmpegPath,
				s.jpegDir,
				s.corruptedJpegDir,
				s.spsNAL,
				s.ppsNAL,
				s.getUnixTimestamp, // Pass timestamp converter function
			)
			if err != nil {
				logger.Error("[FrameStorage] Failed to start continuous decoder: %v (falling back to frame-by-frame)", err)
				s.useContinuousMode = false
			} else {
				logger.Info("[FrameStorage] Continuous decoder initialized successfully")
				logger.Info("[FrameStorage] Continuous decoder session started (can decode P-frames)")
			}
		}
	}

	return nil
}

// UpdateTimestampMapping updates the RTP to Unix timestamp mapping from an RTCP Sender Report
func (s *FrameStorage) UpdateTimestampMapping(sr *rtp.SenderReport) {
	logger.Debug("[FrameStorage:UpdateTimestampMapping] Received RTCP Sender Report: SSRC=0x%x, RTP=%d, NTP=%d, Packets=%d, Octets=%d",
		sr.SSRC, sr.RTPTimestamp, sr.NTPTimestamp, sr.PacketCount, sr.OctetCount)
	
	// Convert NTP to human-readable time for logging
	if sr.NTPTimestamp != 0 {
		ntpTime := rtp.NTPToTime(sr.NTPTimestamp)
		logger.Debug("[FrameStorage:UpdateTimestampMapping] NTP Time (human-readable): %s", ntpTime.Format(time.RFC3339Nano))
	}
	
	s.mu.Lock()
	s.timestampMapper.UpdateFromSR(sr)
	s.mu.Unlock()
	
	logger.Debug("[FrameStorage:UpdateTimestampMapping] Mapping update completed")
}

// getUnixTimestamp converts an RTP timestamp to Unix epoch timestamp (nanoseconds)
// Returns the Unix timestamp in nanoseconds, or 0 if mapping is not yet available
// Thread-safe: TimestampMapper is internally thread-safe, so we can call it without locks
func (s *FrameStorage) getUnixTimestamp(rtpTimestamp uint32) int64 {
	// Check mapper state
	mapperState := s.timestampMapper.GetState()
	if !mapperState.Initialized {
		logger.Warn("[FrameStorage:getUnixTimestamp] Timestamp mapping not available: mapper not initialized yet. Need RTCP Sender Report. Current state: initialized=%t, rtpTimestamp=%d, ntpTimestamp=%d",
			mapperState.Initialized, mapperState.RTPTimestamp, mapperState.NTPTimestamp)
		return 0
	}
	
	logger.Debug("[FrameStorage:getUnixTimestamp] Attempting conversion: RTP=%d, mapper has RTP=%d, NTP=%d, clockRate=%d",
		rtpTimestamp, mapperState.RTPTimestamp, mapperState.NTPTimestamp, mapperState.ClockRate)
	
	// Convert RTP timestamp to NTP timestamp
	// TimestampMapper.RTPToNTP is thread-safe internally
	ntpTimestamp := s.timestampMapper.RTPToNTP(rtpTimestamp)
	if ntpTimestamp == 0 {
		logger.Warn("[FrameStorage:getUnixTimestamp] RTPToNTP returned 0 (mapping check failed)")
		return 0
	}
	
	// Convert NTP timestamp to Unix timestamp (nanoseconds)
	unixTime := rtp.NTPToTime(ntpTimestamp)
	unixNanos := unixTime.UnixNano()
	logger.Debug("[FrameStorage:getUnixTimestamp] Conversion successful: RTP=%d -> NTP=%d -> Unix=%d (%s)",
		rtpTimestamp, ntpTimestamp, unixNanos, unixTime.Format(time.RFC3339Nano))
	return unixNanos
}

// decodeBase64 decodes base64 string
func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// SaveFrame saves a video frame to disk with timestamp-based naming
func (s *FrameStorage) SaveFrame(frame *decoder.Frame) error {
	if frame == nil {
		return ErrNilFrame
	}

	logger.Debug("[FrameStorage:SaveFrame] Acquiring lock for timestamp=%d", frame.Timestamp)
	s.mu.Lock()
	logger.Debug("[FrameStorage:SaveFrame] Lock acquired for timestamp=%d", frame.Timestamp)
	defer func() {
		logger.Debug("[FrameStorage:SaveFrame] Releasing lock for timestamp=%d", frame.Timestamp)
		s.mu.Unlock()
	}()

	// Save each frame as individual H.264 file
	// Also maintain continuous stream for potential video playback
	if s.saveAsJPG && s.streamFile != nil && len(s.spsNAL) > 0 && len(s.ppsNAL) > 0 {
		// Write to continuous stream
		s.streamFile.Write(frame.Data)
		s.streamFile.Sync()
		s.frameCount++
	}
	
	// Always save individual frame as H.264 in H.264 directory
	var h264TargetDir string
	if frame.IsCorrupted {
		h264TargetDir = s.corruptedFramesDir
	} else {
		h264TargetDir = s.h264Dir
	}
	h264Filename := s.getFilenameH264(frame.Timestamp, frame.IsCorrupted)
	h264Path := filepath.Join(h264TargetDir, h264Filename)
	logger.Debug("[FrameStorage:SaveFrame] Saving H.264 frame: %s (timestamp: %d, size: %d bytes, keyframe: %t)", h264Path, frame.Timestamp, len(frame.Data), frame.IsKey)
	if err := os.WriteFile(h264Path, frame.Data, 0644); err != nil {
		return fmt.Errorf("failed to write frame file: %w", err)
	}

	// Decode frame to JPEG if enabled
	if s.saveAsJPG && s.ffmpegPath != "" && len(s.spsNAL) > 0 && len(s.ppsNAL) > 0 {
		if s.useContinuousMode && s.continuousDecoder != nil {
			// Use continuous decoder session (matches documentation approach)
			// This can decode all frames including P-frames, not just keyframes
			logger.Debug("[FrameStorage:SaveFrame] Feeding frame to continuous decoder: timestamp=%d, isKey=%t, corrupted=%t, size=%d",
				frame.Timestamp, frame.IsKey, frame.IsCorrupted, len(frame.Data))
			s.continuousDecoder.FeedFrame(frame)
		} else {
			// Frame-by-frame decoding (original approach - keyframes only)
			// Only decode keyframes - they are self-contained and can be decoded independently
			// P-frames depend on previous frames and cannot be decoded without reference frames
			if !frame.IsCorrupted && frame.IsKey {
				// Save JPEG to JPEG directory
				jpgFilename := s.getFilenameJPEG(frame.Timestamp, false)
				jpgPath := filepath.Join(s.jpegDir, jpgFilename)
				logger.Debug("[FrameStorage:SaveFrame] Decoding keyframe to JPEG: %s (RTP timestamp: %d)", jpgPath, frame.Timestamp)
				if err := s.decodeFrameToJPEG(frame, jpgPath); err != nil {
					logger.Warn("[FrameStorage:SaveFrame] Failed to decode keyframe to JPEG: %v", err)
				}
			}
		}
	}

	// Update statistics
	s.stats.TotalFrames++
	s.stats.TotalBytes += int64(len(frame.Data))
	if frame.IsKey {
		s.stats.KeyFrames++
	}
	if frame.IsCorrupted {
		s.stats.CorruptedFrames++
	}

	return nil
}

// GetStats returns current storage statistics
func (s *FrameStorage) GetStats() StorageStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return StorageStats{
		TotalFrames:     s.stats.TotalFrames,
		KeyFrames:       s.stats.KeyFrames,
		CorruptedFrames: s.stats.CorruptedFrames,
		TotalBytes:      s.stats.TotalBytes,
	}
}

// EnableContinuousDecoder enables the continuous decoder session mode
// This allows decoding P-frames (not just I-frames) by maintaining frame history
func (s *FrameStorage) EnableContinuousDecoder(enable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.useContinuousMode = enable
	
	if !enable && s.continuousDecoder != nil {
		// Stop existing decoder
		s.continuousDecoder.Stop()
		s.continuousDecoder = nil
	}
}

// Close closes the storage handler
func (s *FrameStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.continuousDecoder != nil {
		if err := s.continuousDecoder.Stop(); err != nil {
			logger.Warn("[FrameStorage] Error stopping continuous decoder: %v", err)
		}
		s.continuousDecoder = nil
	}
	
	if s.streamFile != nil {
		s.streamFile.Close()
		// Optionally remove the stream file after closing
		// os.Remove(s.streamFilePath)
	}
	return nil
}

// getFilename generates a filename based on timestamp
func (s *FrameStorage) getFilename(timestamp uint32, corrupted bool) string {
	ext := ".h264"
	if s.saveAsJPG && s.ffmpegPath != "" {
		ext = ".jpg"
	}
	
	if corrupted {
		return fmt.Sprintf("%d_corrupted%s", timestamp, ext)
	}
	return fmt.Sprintf("%d%s", timestamp, ext)
}

// getFilenameH264 generates a filename for H.264 using NTP timestamp in nanoseconds and RTP timestamp
// Format: NTPNANOSECONDS.RTPTIMESTAMP.h264 (e.g., 1761897425257387428.1234567890.h264)
// This preserves both NTP (wall-clock) time and RTP timestamp for correlation
func (s *FrameStorage) getFilenameH264(rtpTimestamp uint32, corrupted bool) string {
	unixNanos := s.getUnixTimestamp(rtpTimestamp)
	if unixNanos == 0 {
		// Fallback to RTP timestamp if mapping not available yet
		filename := fmt.Sprintf("%d_corrupted.h264", rtpTimestamp)
		if !corrupted {
			filename = fmt.Sprintf("%d.h264", rtpTimestamp)
		}
		logger.Debug("[FrameStorage:getFilenameH264] Using RTP timestamp (mapping not available): %s (RTP timestamp: %d)", filename, rtpTimestamp)
		return filename
	}
	
	// Format: ntpinnanoseconds.rtptimestamp.h264
	// This preserves full nanosecond precision from RTCP NTP timestamps and RTP timestamp
	filename := fmt.Sprintf("%d.%d_corrupted.h264", unixNanos, rtpTimestamp)
	if !corrupted {
		filename = fmt.Sprintf("%d.%d.h264", unixNanos, rtpTimestamp)
	}
	logger.Debug("[FrameStorage:getFilenameH264] Using NTP (nanoseconds).RTP timestamp: %s (NTP: %d, RTP: %d)", filename, unixNanos, rtpTimestamp)
	return filename
}

// getFilenameJPEG generates a filename for JPEG output using NTP timestamp in nanoseconds and RTP timestamp
// Format: NTPNANOSECONDS.RTPTIMESTAMP.jpg (e.g., 1761897425257387428.1234567890.jpg)
// This preserves both NTP (wall-clock) time and RTP timestamp for correlation
func (s *FrameStorage) getFilenameJPEG(rtpTimestamp uint32, corrupted bool) string {
	unixNanos := s.getUnixTimestamp(rtpTimestamp)
	if unixNanos == 0 {
		// Fallback to RTP timestamp if mapping not available yet
		mapperState := s.timestampMapper.GetState()
		logger.Warn("[FrameStorage:getFilenameJPEG] Cannot use Unix timestamp - mapping not available: RTP timestamp=%d, mapper initialized=%t",
			rtpTimestamp, mapperState.Initialized)
		if !mapperState.Initialized {
			logger.Debug("[FrameStorage:getFilenameJPEG] Timestamp mapper not initialized. Need RTCP Sender Report (SR) packet to initialize mapping. Check: RTCP packets being received, RTCP Sender Report (type 200) received, ReadRTCP() working, UpdateTimestampMapping() called")
		} else {
			logger.Debug("[FrameStorage:getFilenameJPEG] Mapper initialized: RTP=%d, NTP=%d, clockRate=%d", mapperState.RTPTimestamp, mapperState.NTPTimestamp, mapperState.ClockRate)
			logger.Warn("[FrameStorage:getFilenameJPEG] Conversion failed for unknown reason")
		}
		filename := fmt.Sprintf("%d_corrupted.jpg", rtpTimestamp)
		if !corrupted {
			filename = fmt.Sprintf("%d.jpg", rtpTimestamp)
		}
		logger.Debug("[FrameStorage:getFilenameJPEG] Using RTP timestamp fallback: %s", filename)
		return filename
	}
	
	// Format: ntpinnanoseconds.rtptimestamp.jpg
	// This preserves full nanosecond precision from RTCP NTP timestamps and RTP timestamp
	filename := fmt.Sprintf("%d.%d_corrupted.jpg", unixNanos, rtpTimestamp)
	if !corrupted {
		filename = fmt.Sprintf("%d.%d.jpg", unixNanos, rtpTimestamp)
	}
	logger.Debug("[FrameStorage:getFilenameJPEG] Using NTP (nanoseconds).RTP timestamp: %s (NTP: %d, RTP: %d)", filename, unixNanos, rtpTimestamp)
	return filename
}

// decodeFrameToJPEG decodes an H.264 frame (complete NAL units) to JPEG image
// The frame must have SPS/PPS available for proper decoding
func (s *FrameStorage) decodeFrameToJPEG(frame *decoder.Frame, outputPath string) error {
	if s.ffmpegPath == "" {
		return fmt.Errorf("ffmpeg not available")
	}

	if len(s.spsNAL) == 0 || len(s.ppsNAL) == 0 {
		return fmt.Errorf("SPS/PPS not available for frame decoding")
	}

	// Note: Only keyframes are decoded (enforced at call site)
	// Keyframes are self-contained and don't require reference frames

	// Construct complete H.264 stream: SPS + PPS + Frame data
	// Frame data already contains start codes and NAL units (IDR slice)
	// SPS/PPS are prepended to provide decoder configuration
	var h264Stream bytes.Buffer
	h264Stream.Write(s.spsNAL)
	h264Stream.Write(s.ppsNAL)
	h264Stream.Write(frame.Data)

	// Use ffmpeg to decode H.264 Annex B format (with start codes) from stdin and output JPEG
	// -f h264: input format is raw H.264 Annex B
	// -i pipe:0: read from stdin
	// -vframes 1: decode only 1 frame (the keyframe)
	// -vsync 0: don't duplicate or drop frames
	// -pix_fmt yuvj420p: use full range YUV for better quality JPEG output
	// -q:v 2: high quality JPEG (scale 2-31, lower is better)
	// -y: overwrite output file
	// -loglevel warning: show warnings for debugging decode issues
	cmd := exec.Command(s.ffmpegPath,
		"-loglevel", "warning",       // Show warnings for debugging
		"-f", "h264",                  // H.264 Annex B format (with start codes)
		"-i", "pipe:0",                // Read from stdin
		"-vframes", "1",               // Decode only 1 frame
		"-vsync", "0",                 // Don't duplicate/drop frames
		"-pix_fmt", "yuvj420p",        // Full range YUV for better JPEG quality
		"-q:v", "2",                   // High quality JPEG
		"-y",                          // Overwrite output file
		outputPath,
	)

	// Set stdin to the H.264 stream
	cmd.Stdin = &h264Stream

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg decode failed: %w (stderr: %s)", err, stderr.String())
	}

	return nil
}

// extractSnapshotFromStream extracts a JPG snapshot from the H.264 stream
func (s *FrameStorage) extractSnapshotFromStream(outputPath string, timestamp uint32) error {
	if s.streamFilePath == "" {
		return fmt.Errorf("stream file not initialized")
	}

	// Use ffmpeg to extract frame from specific time position in stream
	// Calculate time position based on frame count (assuming ~30fps)
	seconds := float64(s.frameCount) / 30.0
	timePos := fmt.Sprintf("%.2f", seconds)
	
	// -ss: seek to position
	// -f h264: force H.264 format  
	// -i: input stream file
	// -vframes 1: extract 1 frame
	// -q:v 2: high quality
	cmd := exec.Command(s.ffmpegPath,
		"-ss", timePos,
		"-f", "h264",
		"-i", s.streamFilePath,
		"-vframes", "1",
		"-q:v", "2",
		"-y",
		outputPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg snapshot failed: %w (stderr: %s)", err, stderr.String())
	}

	return nil
}

// String returns a string representation of storage stats
func (s *StorageStats) String() string {
	corruptedInfo := ""
	if s.CorruptedFrames > 0 {
		corruptedInfo = fmt.Sprintf(", Corrupted: %d", s.CorruptedFrames)
	}
	return fmt.Sprintf(
		"Frames: %d (Keyframes: %d%s) | Size: %.2f MB",
		s.TotalFrames,
		s.KeyFrames,
		corruptedInfo,
		float64(s.TotalBytes)/(1024*1024),
	)
}
