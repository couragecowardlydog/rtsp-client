package storage

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/rtsp-client/pkg/decoder"
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
func NewContinuousDecoder(ffmpegPath, jpegDir, corruptedJpegDir string, spsNAL, ppsNAL []byte) (*ContinuousDecoder, error) {
	log.Printf("[ContinuousDecoder] Initializing continuous decoder session")
	log.Printf("[ContinuousDecoder] Configuration: jpegDir=%s, corruptedJpegDir=%s", jpegDir, corruptedJpegDir)
	log.Printf("[ContinuousDecoder] SPS size: %d bytes, PPS size: %d bytes", len(spsNAL), len(ppsNAL))
	
	cd := &ContinuousDecoder{
		ffmpegPath:      ffmpegPath,
		jpegDir:         jpegDir,
		corruptedJpegDir: corruptedJpegDir,
		spsNAL:          spsNAL,
		ppsNAL:          ppsNAL,
		frameQueue:      make(chan FrameWithTimestamp, 100), // Buffer up to 100 frames
		decodedQueue:    make(chan DecodedFrame, 100),
		metadataQueue:   make(chan FrameWithTimestamp, 100), // Frame metadata for matching
		initialized:     false,
	}
	
	log.Printf("[ContinuousDecoder] Starting FFmpeg process...")
	if err := cd.start(); err != nil {
		log.Printf("[ContinuousDecoder] ❌ Failed to start: %v", err)
		return nil, fmt.Errorf("failed to start continuous decoder: %w", err)
	}
	
	log.Printf("[ContinuousDecoder] ✅ FFmpeg process started successfully")
	log.Printf("[ContinuousDecoder] Starting goroutines: feedFrames, receiveFrames, saveDecodedFrames")
	
	// Start goroutines for processing
	go cd.feedFrames()
	go cd.receiveFrames()
	go cd.saveDecodedFrames()
	
	log.Printf("[ContinuousDecoder] ✅ Continuous decoder fully initialized and ready")
	
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
		log.Printf("[ContinuousDecoder] Writing SPS (%d bytes) and PPS (%d bytes) to initialize decoder", len(cd.spsNAL), len(cd.ppsNAL))
		if _, err := cd.stdin.Write(cd.spsNAL); err != nil {
			log.Printf("[ContinuousDecoder] ❌ Failed to write SPS: %v", err)
			return fmt.Errorf("failed to write SPS: %w", err)
		}
		if _, err := cd.stdin.Write(cd.ppsNAL); err != nil {
			log.Printf("[ContinuousDecoder] ❌ Failed to write PPS: %v", err)
			return fmt.Errorf("failed to write PPS: %w", err)
		}
		cd.initialized = true
		log.Printf("[ContinuousDecoder] ✅ SPS/PPS written, decoder initialized")
	} else {
		log.Printf("[ContinuousDecoder] ⚠️  No SPS/PPS provided, decoder may not work correctly")
	}
	
	log.Printf("[ContinuousDecoder] FFmpeg process ready, PID: %d", cd.cmd.Process.Pid)
	return nil
}

// FeedFrame feeds a frame to the continuous decoder
func (cd *ContinuousDecoder) FeedFrame(frame *decoder.Frame) {
	if !cd.running {
		log.Printf("[ContinuousDecoder] ⚠️  FeedFrame called but decoder not running, dropping frame (timestamp: %d)", frame.Timestamp)
		return
	}
	
	cd.mu.Lock()
	seqNum := cd.seqNumber
	cd.seqNumber++
	frameQueueLen := len(cd.frameQueue)
	cd.mu.Unlock()
	
	log.Printf("[ContinuousDecoder] 📥 Feeding frame: seq=%d, timestamp=%d, size=%d bytes, isKey=%t, corrupted=%t, queueLen=%d",
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
		log.Printf("[ContinuousDecoder] ⚠️  Frame queue full (%d/100), dropping frame (seq=%d, timestamp=%d)",
			frameQueueLen, seqNum, frame.Timestamp)
	}
}

// receiveFrames continuously receives decoded JPEG frames from FFmpeg stdout
func (cd *ContinuousDecoder) receiveFrames() {
	log.Printf("[ContinuousDecoder:receiveFrames] 🟢 Goroutine started")
	buffer := make([]byte, 4096)
	jpegBuffer := make([]byte, 0, 1024*1024) // 1MB initial capacity
	jpegFrameCount := 0
	readCount := 0
	
	for {
		if !cd.running {
			log.Printf("[ContinuousDecoder:receiveFrames] 🔴 Decoder stopped, exiting receiveFrames goroutine")
			return
		}
		
		cd.mu.Lock()
		stdout := cd.stdout
		cd.mu.Unlock()
		
		if stdout == nil {
			log.Printf("[ContinuousDecoder:receiveFrames] ⚠️  Stdout is nil, exiting")
			return
		}
		
		n, err := stdout.Read(buffer)
		if err != nil {
			if err != io.EOF {
				log.Printf("[ContinuousDecoder:receiveFrames] ❌ Error reading from decoder stdout: %v", err)
			} else {
				log.Printf("[ContinuousDecoder:receiveFrames] 📄 EOF reached on stdout (read %d times, decoded %d frames)", readCount, jpegFrameCount)
			}
			break
		}
		
		readCount++
		if readCount%100 == 0 {
			log.Printf("[ContinuousDecoder:receiveFrames] 📊 Read progress: %d reads, buffer size: %d bytes, decoded frames: %d",
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
				log.Printf("[ContinuousDecoder:receiveFrames] ⏳ Incomplete JPEG frame detected at offset %d, buffering...", startIdx)
				break
			}
			
			// Extract complete JPEG frame
			jpegFrame := make([]byte, endIdx)
			copy(jpegFrame, jpegBuffer[startIdx:startIdx+endIdx])
			jpegFrameCount++
			
			log.Printf("[ContinuousDecoder:receiveFrames] 🖼️  Complete JPEG frame #%d found: %d bytes (startIdx=%d, endIdx=%d)",
				jpegFrameCount, len(jpegFrame), startIdx, endIdx)
			
			// Remove processed data from buffer
			jpegBuffer = jpegBuffer[startIdx+endIdx:]
			
			// Update statistics
			cd.mu.Lock()
			cd.decodedCount++
			decodedCount := cd.decodedCount
			cd.mu.Unlock()
			
			decodedQueueLen := len(cd.decodedQueue)
			log.Printf("[ContinuousDecoder:receiveFrames] 📤 Sending decoded frame to queue: size=%d bytes, decodedCount=%d, queueLen=%d/100",
				len(jpegFrame), decodedCount, decodedQueueLen)
			
			// Send decoded frame for saving (will be matched with metadata in saveDecodedFrames)
			select {
			case cd.decodedQueue <- DecodedFrame{
				JPEGData: jpegFrame,
			}:
				log.Printf("[ContinuousDecoder:receiveFrames] ✅ Decoded frame queued successfully (#%d)", jpegFrameCount)
			default:
				log.Printf("[ContinuousDecoder:receiveFrames] ⚠️  Decoded frame queue full (%d/100), dropping frame #%d",
					decodedQueueLen, jpegFrameCount)
			}
		}
	}
	
	log.Printf("[ContinuousDecoder:receiveFrames] 🔴 Exiting (total: %d reads, %d JPEG frames decoded, buffer: %d bytes)",
		readCount, jpegFrameCount, len(jpegBuffer))
}

// saveDecodedFrames saves decoded JPEG frames to disk
func (cd *ContinuousDecoder) saveDecodedFrames() {
	log.Printf("[ContinuousDecoder:saveDecodedFrames] 🟢 Goroutine started")
	savedCount := 0
	metadataMatchCount := 0
	metadataMissCount := 0
	
	for decodedFrame := range cd.decodedQueue {
		savedCount++
		
		log.Printf("[ContinuousDecoder:saveDecodedFrames] 💾 Processing decoded frame #%d: size=%d bytes",
			savedCount, len(decodedFrame.JPEGData))
		
		// Get matching frame metadata from queue (FIFO, sequential matching)
		var frameData FrameWithTimestamp
		metadataQueueLen := len(cd.metadataQueue)
		
		select {
		case frameData = <-cd.metadataQueue:
			metadataMatchCount++
			log.Printf("[ContinuousDecoder:saveDecodedFrames] ✅ Metadata matched: timestamp=%d, seq=%d (matchCount=%d, queueLen=%d->%d)",
				frameData.Timestamp, frameData.SeqNumber, metadataMatchCount, metadataQueueLen, len(cd.metadataQueue))
		default:
			metadataMissCount++
			// No metadata available, use fallback
			frameData = FrameWithTimestamp{
				Timestamp: uint32(savedCount), // Fallback to sequential number
			}
			log.Printf("[ContinuousDecoder:saveDecodedFrames] ⚠️  No metadata available, using fallback timestamp=%d (missCount=%d, queueLen=%d)",
				frameData.Timestamp, metadataMissCount, metadataQueueLen)
		}
		
		decodedFrame.Timestamp = frameData.Timestamp
		decodedFrame.IsCorrupted = frameData.Frame != nil && frameData.Frame.IsCorrupted
		
		if frameData.Frame != nil {
			log.Printf("[ContinuousDecoder:saveDecodedFrames] Frame info: timestamp=%d, isKey=%t, corrupted=%t",
				decodedFrame.Timestamp, frameData.Frame.IsKey, decodedFrame.IsCorrupted)
		}
		
		// Determine output directory for JPEG files
		var targetDir string
		if decodedFrame.IsCorrupted {
			targetDir = cd.corruptedJpegDir
			log.Printf("[ContinuousDecoder:saveDecodedFrames] ⚠️  Frame is corrupted, saving to corrupted JPEG directory")
		} else {
			targetDir = cd.jpegDir
		}
		
		// Save JPEG file
		jpgPath := filepath.Join(targetDir, fmt.Sprintf("%d.jpg", decodedFrame.Timestamp))
		log.Printf("[ContinuousDecoder:saveDecodedFrames] 💾 Saving JPEG: path=%s, size=%d bytes", jpgPath, len(decodedFrame.JPEGData))
		
		if err := os.WriteFile(jpgPath, decodedFrame.JPEGData, 0644); err != nil {
			log.Printf("[ContinuousDecoder:saveDecodedFrames] ❌ Failed to save decoded frame #%d: %v", savedCount, err)
		} else {
			log.Printf("[ContinuousDecoder:saveDecodedFrames] ✅ Successfully saved frame #%d: %s (%d bytes)",
				savedCount, jpgPath, len(decodedFrame.JPEGData))
		}
		
		// Log statistics every 10 frames
		if savedCount%10 == 0 {
			cd.mu.Lock()
			decodedCount := cd.decodedCount
			failedCount := cd.failedCount
			cd.mu.Unlock()
			log.Printf("[ContinuousDecoder:saveDecodedFrames] 📊 Statistics: saved=%d, decoded=%d, failed=%d, metadataMatches=%d, metadataMisses=%d",
				savedCount, decodedCount, failedCount, metadataMatchCount, metadataMissCount)
		}
	}
	
	log.Printf("[ContinuousDecoder:saveDecodedFrames] 🔴 Decoded queue closed, exiting (total saved: %d, matches: %d, misses: %d)",
		savedCount, metadataMatchCount, metadataMissCount)
}

// feedFrames continuously feeds frames to FFmpeg stdin
func (cd *ContinuousDecoder) feedFrames() {
	log.Printf("[ContinuousDecoder:feedFrames] 🟢 Goroutine started")
	frameCount := 0
	
	for frameData := range cd.frameQueue {
		frameCount++
		
		if !cd.running {
			log.Printf("[ContinuousDecoder:feedFrames] 🔴 Decoder stopped, exiting feedFrames goroutine (processed %d frames)", frameCount)
			return
		}
		
		log.Printf("[ContinuousDecoder:feedFrames] Processing frame seq=%d, timestamp=%d, size=%d bytes",
			frameData.SeqNumber, frameData.Timestamp, len(frameData.Frame.Data))
		
		// Skip corrupted frames to avoid decoder errors
		if frameData.Frame.IsCorrupted {
			log.Printf("[ContinuousDecoder:feedFrames] ⏭️  Skipping corrupted frame seq=%d, timestamp=%d",
				frameData.SeqNumber, frameData.Timestamp)
			continue
		}
		
		// Send metadata to matching queue (only for frames that will be decoded)
		metadataQueueLen := len(cd.metadataQueue)
		select {
		case cd.metadataQueue <- frameData:
			log.Printf("[ContinuousDecoder:feedFrames] ✅ Metadata queued: seq=%d, timestamp=%d (metadataQueue: %d/100)",
				frameData.SeqNumber, frameData.Timestamp, metadataQueueLen+1)
		default:
			log.Printf("[ContinuousDecoder:feedFrames] ⚠️  Metadata queue full (%d/100), skipping metadata for seq=%d",
				metadataQueueLen, frameData.SeqNumber)
		}
		
		cd.mu.Lock()
		stdin := cd.stdin
		cd.mu.Unlock()
		
		if stdin == nil {
			log.Printf("[ContinuousDecoder:feedFrames] ⚠️  Stdin is nil, skipping frame seq=%d", frameData.SeqNumber)
			continue
		}
		
		// Write frame data to FFmpeg stdin
		bytesWritten, err := stdin.Write(frameData.Frame.Data)
		if err != nil {
			log.Printf("[ContinuousDecoder:feedFrames] ❌ Failed to write frame to FFmpeg stdin: seq=%d, bytes=%d/%d, error=%v",
				frameData.SeqNumber, bytesWritten, len(frameData.Frame.Data), err)
			cd.mu.Lock()
			cd.failedCount++
			failedCount := cd.failedCount
			cd.mu.Unlock()
			log.Printf("[ContinuousDecoder:feedFrames] Failed frame count: %d", failedCount)
			continue
		}
		
		log.Printf("[ContinuousDecoder:feedFrames] ✅ Wrote frame to FFmpeg: seq=%d, timestamp=%d, bytes=%d/%d",
			frameData.SeqNumber, frameData.Timestamp, bytesWritten, len(frameData.Frame.Data))
		
		// Flush to ensure frame is sent
		if flusher, ok := stdin.(interface{ Flush() error }); ok {
			if err := flusher.Flush(); err != nil {
				log.Printf("[ContinuousDecoder:feedFrames] ⚠️  Flush error: %v", err)
			}
		}
	}
	
	log.Printf("[ContinuousDecoder:feedFrames] 🔴 Frame queue closed, exiting (total frames processed: %d)", frameCount)
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
		log.Printf("[ContinuousDecoder] ⚠️  Stop called but decoder not running")
		return nil
	}
	
	log.Printf("[ContinuousDecoder] 🛑 Stopping continuous decoder...")
	log.Printf("[ContinuousDecoder] Queue states: frameQueue=%d, decodedQueue=%d, metadataQueue=%d",
		len(cd.frameQueue), len(cd.decodedQueue), len(cd.metadataQueue))
	
	cd.running = false
	cd.mu.Unlock()
	
	// Close queues
	log.Printf("[ContinuousDecoder] Closing queues...")
	close(cd.frameQueue)
	close(cd.decodedQueue)
	close(cd.metadataQueue)
	
	// Close stdin to signal end of input
	if cd.stdin != nil {
		log.Printf("[ContinuousDecoder] Closing stdin pipe...")
		if err := cd.stdin.Close(); err != nil {
			log.Printf("[ContinuousDecoder] ⚠️  Error closing stdin: %v", err)
		}
	}
	
	// Wait for process to finish
	if cd.cmd != nil {
		log.Printf("[ContinuousDecoder] Waiting for FFmpeg process to finish (PID: %d)...", cd.cmd.Process.Pid)
		if err := cd.cmd.Wait(); err != nil {
			cd.mu.Lock()
			decodedCount := cd.decodedCount
			failedCount := cd.failedCount
			cd.mu.Unlock()
			log.Printf("[ContinuousDecoder] ⚠️  FFmpeg process error: %v (decoded: %d, failed: %d)", err, decodedCount, failedCount)
			return fmt.Errorf("ffmpeg process error: %w", err)
		}
		log.Printf("[ContinuousDecoder] ✅ FFmpeg process exited successfully")
	}
	
	cd.mu.Lock()
	decodedCount := cd.decodedCount
	failedCount := cd.failedCount
	cd.mu.Unlock()
	
	log.Printf("[ContinuousDecoder] ✅ Stopped successfully (decoded: %d frames, failed: %d frames)", decodedCount, failedCount)
	
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
	fmt.Printf("📁 Creating output directory: %s\n", outputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create separate directories for H.264 and JPEG files
	h264Dir := filepath.Join(outputDir, "h264")
	fmt.Printf("📁 Creating H.264 directory: %s\n", h264Dir)
	if err := os.MkdirAll(h264Dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create H.264 directory: %w", err)
	}

	jpegDir := filepath.Join(outputDir, "jpeg")
	fmt.Printf("📁 Creating JPEG directory: %s\n", jpegDir)
	if err := os.MkdirAll(jpegDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create JPEG directory: %w", err)
	}

	// Create corrupted frames directory
	corruptedFramesDir := filepath.Join(outputDir, "corrupted_frames")
	fmt.Printf("📁 Creating corrupted frames directory: %s\n", corruptedFramesDir)
	if err := os.MkdirAll(corruptedFramesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create corrupted frames directory: %w", err)
	}

	// Create corrupted JPEG directory
	corruptedJpegDir := filepath.Join(corruptedFramesDir, "jpeg")
	fmt.Printf("📁 Creating corrupted JPEG directory: %s\n", corruptedJpegDir)
	if err := os.MkdirAll(corruptedJpegDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create corrupted JPEG directory: %w", err)
	}

	fmt.Printf("✅ Directory structure created successfully:\n")
	fmt.Printf("   %s/\n", outputDir)
	fmt.Printf("   ├── h264/              (H.264 frame files)\n")
	fmt.Printf("   ├── jpeg/              (JPEG image files)\n")
	fmt.Printf("   └── corrupted_frames/ (Corrupted frames)\n")
	fmt.Printf("       └── jpeg/          (Corrupted JPEG files)\n")

	// Find ffmpeg if saving as JPG
	var ffmpegPath string
	if saveAsJPG {
		path, err := exec.LookPath("ffmpeg")
		if err != nil {
			// FFmpeg not found, will save as H.264
			fmt.Println("⚠️  JPEG mode requested but ffmpeg not found. Saving as H.264 only.")
			fmt.Println("   Install ffmpeg to enable JPEG frame conversion: https://ffmpeg.org/download.html")
			saveAsJPG = false
		} else {
			ffmpegPath = path
			fmt.Println("✅ JPEG mode enabled - H.264 files will be saved in h264/ folder, JPEG images in jpeg/ folder")
		}
	} else {
		fmt.Println("📹 H.264 mode - saving frames as .h264 only")
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
	}
	
	if continuousDecoder && saveAsJPG && ffmpegPath != "" {
		fmt.Println("✅ Continuous decoder mode enabled (can decode all frames including P-frames)")
	} else if saveAsJPG && ffmpegPath != "" {
		fmt.Println("✅ Frame-by-frame decoder mode (keyframes only)")
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
		fmt.Println("✅ SPS/PPS loaded from SDP for JPG conversion")
		
		// Write SPS and PPS to stream file first
		if s.streamFile != nil {
			s.streamFile.Write(s.spsNAL)
			s.streamFile.Write(s.ppsNAL)
		}
		
		// Initialize continuous decoder if enabled
		if s.useContinuousMode && s.ffmpegPath != "" && s.continuousDecoder == nil {
			log.Printf("[FrameStorage] Initializing continuous decoder with SPS/PPS...")
			var err error
			s.continuousDecoder, err = NewContinuousDecoder(
				s.ffmpegPath,
				s.jpegDir,
				s.corruptedJpegDir,
				s.spsNAL,
				s.ppsNAL,
			)
			if err != nil {
				log.Printf("[FrameStorage] ❌ Failed to start continuous decoder: %v (falling back to frame-by-frame)", err)
				fmt.Printf("⚠️  Failed to start continuous decoder: %v (falling back to frame-by-frame)\n", err)
				s.useContinuousMode = false
			} else {
				log.Printf("[FrameStorage] ✅ Continuous decoder initialized successfully")
				fmt.Println("✅ Continuous decoder session started (can decode P-frames)")
			}
		}
	}

	return nil
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

	s.mu.Lock()
	defer s.mu.Unlock()

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
	h264Path := filepath.Join(h264TargetDir, s.getFilenameH264(frame.Timestamp, frame.IsCorrupted))
	if err := os.WriteFile(h264Path, frame.Data, 0644); err != nil {
		return fmt.Errorf("failed to write frame file: %w", err)
	}

	// Decode frame to JPEG if enabled
	if s.saveAsJPG && s.ffmpegPath != "" && len(s.spsNAL) > 0 && len(s.ppsNAL) > 0 {
		if s.useContinuousMode && s.continuousDecoder != nil {
			// Use continuous decoder session (matches documentation approach)
			// This can decode all frames including P-frames, not just keyframes
			log.Printf("[FrameStorage:SaveFrame] Feeding frame to continuous decoder: timestamp=%d, isKey=%t, corrupted=%t, size=%d",
				frame.Timestamp, frame.IsKey, frame.IsCorrupted, len(frame.Data))
			s.continuousDecoder.FeedFrame(frame)
		} else {
			// Frame-by-frame decoding (original approach - keyframes only)
			// Only decode keyframes - they are self-contained and can be decoded independently
			// P-frames depend on previous frames and cannot be decoded without reference frames
			if !frame.IsCorrupted && frame.IsKey {
				// Save JPEG to JPEG directory
				jpgPath := filepath.Join(s.jpegDir, s.getFilenameJPEG(frame.Timestamp, false))
				if err := s.decodeFrameToJPEG(frame, jpgPath); err != nil {
					fmt.Printf("⚠️  Failed to decode keyframe to JPEG: %v\n", err)
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
			fmt.Printf("⚠️  Error stopping continuous decoder: %v\n", err)
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

// getFilenameH264 generates a filename for H.264 fallback
func (s *FrameStorage) getFilenameH264(timestamp uint32, corrupted bool) string {
	if corrupted {
		return fmt.Sprintf("%d_corrupted.h264", timestamp)
	}
	return fmt.Sprintf("%d.h264", timestamp)
}

// getFilenameJPEG generates a filename for JPEG output
func (s *FrameStorage) getFilenameJPEG(timestamp uint32, corrupted bool) string {
	if corrupted {
		return fmt.Sprintf("%d_corrupted.jpg", timestamp)
	}
	return fmt.Sprintf("%d.jpg", timestamp)
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
