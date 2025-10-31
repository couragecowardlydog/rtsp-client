package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rtsp-client/pkg/decoder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrameStorage_New(t *testing.T) {
	tests := []struct {
		name        string
		outputDir   string
		expectError bool
	}{
		{
			name:        "valid directory",
			outputDir:   "/tmp/rtsp-test-frames",
			expectError: false,
		},
		{
			name:        "empty directory defaults to ./frames",
			outputDir:   "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use H.264 format for tests (no ffmpeg dependency)
			storage, err := NewFrameStorageWithFormat(tt.outputDir, false)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, storage)

				if tt.outputDir == "" {
					assert.Equal(t, "./frames", storage.outputDir)
				} else {
					assert.Equal(t, tt.outputDir, storage.outputDir)
				}

				// Cleanup
				if storage.outputDir != "" {
					os.RemoveAll(storage.outputDir)
				}
			}
		})
	}
}

func TestFrameStorage_SaveFrame(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "rtsp-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Use H.264 format for tests (no ffmpeg dependency)
	storage, err := NewFrameStorageWithFormat(tempDir, false)
	require.NoError(t, err)

	tests := []struct {
		name         string
		frame        *decoder.Frame
		expectedFile string
		expectError  bool
	}{
		{
			name: "save valid frame",
			frame: &decoder.Frame{
				Data:      []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x01, 0x02, 0x03},
				Timestamp: 1000,
				IsKey:     true,
			},
			expectedFile: "1000.h264",
			expectError:  false,
		},
		{
			name: "save frame with large timestamp",
			frame: &decoder.Frame{
				Data:      []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0x01, 0x02},
				Timestamp: 4294967295,
				IsKey:     false,
			},
			expectedFile: "4294967295.h264",
			expectError:  false,
		},
		{
			name: "save empty frame",
			frame: &decoder.Frame{
				Data:      []byte{},
				Timestamp: 2000,
			},
			expectedFile: "2000.h264",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.SaveFrame(tt.frame)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)

				// Verify file exists
				filePath := filepath.Join(tempDir, tt.expectedFile)
				_, err := os.Stat(filePath)
				require.NoError(t, err, "Frame file should exist")

				// Verify file content
				content, err := os.ReadFile(filePath)
				require.NoError(t, err)
				assert.Equal(t, tt.frame.Data, content)

				// Cleanup
				os.Remove(filePath)
			}
		})
	}
}

func TestFrameStorage_SaveFrame_NilFrame(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "rtsp-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Use H.264 format for tests (no ffmpeg dependency)
	storage, err := NewFrameStorageWithFormat(tempDir, false)
	require.NoError(t, err)

	err = storage.SaveFrame(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil frame")
}

func TestFrameStorage_GetStats(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "rtsp-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Use H.264 format for tests (no ffmpeg dependency)
	storage, err := NewFrameStorageWithFormat(tempDir, false)
	require.NoError(t, err)

	// Initially, stats should be zero
	stats := storage.GetStats()
	assert.Equal(t, int64(0), stats.TotalFrames)
	assert.Equal(t, int64(0), stats.KeyFrames)
	assert.Equal(t, int64(0), stats.TotalBytes)

	// Save some frames
	frames := []*decoder.Frame{
		{Data: []byte{0x01, 0x02, 0x03}, Timestamp: 1000, IsKey: true},
		{Data: []byte{0x04, 0x05}, Timestamp: 2000, IsKey: false},
		{Data: []byte{0x06, 0x07, 0x08, 0x09}, Timestamp: 3000, IsKey: true},
	}

	for _, frame := range frames {
		err := storage.SaveFrame(frame)
		require.NoError(t, err)
	}

	// Check stats
	stats = storage.GetStats()
	assert.Equal(t, int64(3), stats.TotalFrames)
	assert.Equal(t, int64(2), stats.KeyFrames)
	assert.Equal(t, int64(9), stats.TotalBytes) // 3 + 2 + 4
}

func TestFrameStorage_GetFilename(t *testing.T) {
	tests := []struct {
		name      string
		timestamp uint32
		expected  string
	}{
		{
			name:      "timestamp 1000",
			timestamp: 1000,
			expected:  "1000.h264",
		},
		{
			name:      "timestamp 0",
			timestamp: 0,
			expected:  "0.h264",
		},
		{
			name:      "large timestamp",
			timestamp: 4294967295,
			expected:  "4294967295.h264",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := &FrameStorage{}
			result := storage.getFilename(tt.timestamp, false)
			assert.Equal(t, tt.expected, result)
		})
	}
	
	// Test corrupted filename generation
	t.Run("corrupted frame filename", func(t *testing.T) {
		storage := &FrameStorage{}
		result := storage.getFilename(1000, true)
		assert.Equal(t, "1000_corrupted.h264", result)
	})
}

func TestFrameStorage_Close(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "rtsp-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Use H.264 format for tests (no ffmpeg dependency)
	storage, err := NewFrameStorageWithFormat(tempDir, false)
	require.NoError(t, err)

	err = storage.Close()
	assert.NoError(t, err)
}

func TestStorageStats_String(t *testing.T) {
	stats := &StorageStats{
		TotalFrames: 100,
		KeyFrames:   10,
		TotalBytes:  1024000,
	}

	result := stats.String()
	assert.Contains(t, result, "100")        // Total frames
	assert.Contains(t, result, "10")         // Keyframes
	assert.Contains(t, result, "0.98 MB")    // Size formatted in MB
	
	// Test with corrupted frames
	statsWithCorrupted := &StorageStats{
		TotalFrames:     100,
		KeyFrames:       10,
		CorruptedFrames: 5,
		TotalBytes:      1024000,
	}
	
	resultWithCorrupted := statsWithCorrupted.String()
	assert.Contains(t, resultWithCorrupted, "Corrupted: 5")
}

func TestFrameStorage_getFilenameJPEG(t *testing.T) {
	storage := &FrameStorage{}

	tests := []struct {
		name      string
		timestamp uint32
		corrupted bool
		expected  string
	}{
		{
			name:      "normal frame",
			timestamp: 1000,
			corrupted: false,
			expected:  "1000.jpg",
		},
		{
			name:      "corrupted frame",
			timestamp: 2000,
			corrupted: true,
			expected:  "2000_corrupted.jpg",
		},
		{
			name:      "zero timestamp",
			timestamp: 0,
			corrupted: false,
			expected:  "0.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := storage.getFilenameJPEG(tt.timestamp, tt.corrupted)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFrameStorage_decodeFrameToJPEG_NoFFmpeg(t *testing.T) {
	storage := &FrameStorage{
		ffmpegPath: "", // No ffmpeg
		spsNAL:     []byte{0x00, 0x00, 0x00, 0x01, 0x67},
		ppsNAL:     []byte{0x00, 0x00, 0x00, 0x01, 0x68},
	}

	frame := &decoder.Frame{
		Data:      []byte{0x00, 0x00, 0x00, 0x01, 0x65},
		Timestamp: 1000,
	}

	err := storage.decodeFrameToJPEG(frame, "/tmp/test.jpg")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ffmpeg not available")
}

func TestFrameStorage_decodeFrameToJPEG_NoSPSPPS(t *testing.T) {
	// This test assumes ffmpeg might not be available, so we'll just check
	// the SPS/PPS validation logic by setting empty SPS/PPS
	storage := &FrameStorage{
		ffmpegPath: "/usr/bin/ffmpeg", // Dummy path
		spsNAL:     []byte{},           // Empty - should fail
		ppsNAL:     []byte{},
	}

	frame := &decoder.Frame{
		Data:      []byte{0x00, 0x00, 0x00, 0x01, 0x65},
		Timestamp: 1000,
	}

	err := storage.decodeFrameToJPEG(frame, "/tmp/test.jpg")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SPS/PPS not available")
}
