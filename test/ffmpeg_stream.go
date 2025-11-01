package test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// FFmpegStream manages an ffmpeg streaming process
type FFmpegStream struct {
	videoFile    string
	rtspURL      string
	ctx          context.Context
	cancel       context.CancelFunc
	cmd          *exec.Cmd
	isStreaming  bool
	streamLoop   bool // Whether to loop the video
}

// NewFFmpegStream creates a new FFmpeg streaming instance
func NewFFmpegStream(videoFile, rtspURL string, loop bool) (*FFmpegStream, error) {
	// Validate video file exists
	if _, err := os.Stat(videoFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("video file does not exist: %s", videoFile)
	}

	// Resolve absolute path
	absPath, err := filepath.Abs(videoFile)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve video file path: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &FFmpegStream{
		videoFile:   absPath,
		rtspURL:     rtspURL,
		ctx:         ctx,
		cancel:      cancel,
		streamLoop:  loop,
		isStreaming: false,
	}, nil
}

// Start starts the ffmpeg streaming process
func (f *FFmpegStream) Start() error {
	if f.isStreaming {
		return fmt.Errorf("ffmpeg stream is already running")
	}

	// Check if ffmpeg is available
	if err := f.checkFFmpeg(); err != nil {
		return fmt.Errorf("ffmpeg not available: %w", err)
	}

	// Build ffmpeg command
	args := []string{
		"-re",                    // Read input at native frame rate
		"-stream_loop", "-1",     // Loop indefinitely
		"-i", f.videoFile,        // Input file
		"-rtsp_transport", "tcp", // Use TCP transport
		"-c:v", "libx264",        // Video codec
		"-preset", "ultrafast",   // Encoding preset
		"-tune", "zerolatency",   // Low latency tuning
		"-c:a", "aac",            // Audio codec
		"-b:a", "128k",           // Audio bitrate
		"-f", "rtsp",             // Output format
		f.rtspURL,                // RTSP URL
	}

	// If not looping, remove stream_loop
	if !f.streamLoop {
		// Remove "-stream_loop" and "-1" from args
		newArgs := []string{}
		for i := 0; i < len(args); i++ {
			if args[i] == "-stream_loop" {
				i++ // Skip next element (-1)
				continue
			}
			newArgs = append(newArgs, args[i])
		}
		args = newArgs
	}

	// Create command
	f.cmd = exec.CommandContext(f.ctx, "ffmpeg", args...)

	// Redirect stderr to capture any errors (ffmpeg uses stderr for progress)
	f.cmd.Stderr = os.Stderr
	f.cmd.Stdout = os.Stderr

	// Start the process
	if err := f.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	f.isStreaming = true

	// Give ffmpeg a moment to start streaming
	time.Sleep(2 * time.Second)

	// Check if process is still running (might have failed immediately)
	if f.cmd.Process != nil {
		// Use syscall to check if process exists (signal 0 is a no-op that just checks)
		if err := syscall.Kill(f.cmd.Process.Pid, syscall.Signal(0)); err != nil {
			f.isStreaming = false
			return fmt.Errorf("ffmpeg process exited immediately: %w", err)
		}
	}

	return nil
}

// Stop stops the ffmpeg streaming process
func (f *FFmpegStream) Stop() error {
	if !f.isStreaming {
		return nil
	}

	f.cancel()

	// Wait for process to terminate
	if f.cmd != nil && f.cmd.Process != nil {
		done := make(chan error, 1)
		go func() {
			done <- f.cmd.Wait()
		}()

		select {
		case <-done:
			// Process finished
		case <-time.After(5 * time.Second):
			// Force kill if it doesn't stop
			f.cmd.Process.Kill()
		}
	}

	f.isStreaming = false
	return nil
}

// IsStreaming returns whether the stream is active
func (f *FFmpegStream) IsStreaming() bool {
	return f.isStreaming
}

// checkFFmpeg verifies ffmpeg is available
func (f *FFmpegStream) checkFFmpeg() error {
	cmd := exec.Command("ffmpeg", "-version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg command not found: %w", err)
	}
	return nil
}

