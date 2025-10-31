package config

import (
	"errors"
	"flag"
	"fmt"
	"time"
)

var (
	// ErrInvalidURL indicates the RTSP URL is invalid
	ErrInvalidURL = errors.New("invalid RTSP URL")
)

// Config holds application configuration
type Config struct {
	RTSPURL           string
	OutputDir         string
	Timeout           time.Duration
	Verbose           bool
	SaveJPEG          bool
	ContinuousDecoder bool // Enable continuous decoder session (can decode P-frames)
}

// ParseFlags parses command-line flags and returns configuration
func ParseFlags() (*Config, error) {
	config := &Config{}

	flag.StringVar(&config.RTSPURL, "url", "", "RTSP stream URL (required)")
	flag.StringVar(&config.OutputDir, "output", "./frames", "Output directory for frames")
	flag.DurationVar(&config.Timeout, "timeout", 10*time.Second, "Connection timeout")
	flag.BoolVar(&config.Verbose, "verbose", false, "Enable verbose logging")
	flag.BoolVar(&config.SaveJPEG, "jpeg", true, "Save frames as JPEG images (requires ffmpeg)")
	flag.BoolVar(&config.ContinuousDecoder, "continuous-decoder", true, "Use continuous decoder session (can decode P-frames, default: true). Set to false for frame-by-frame mode (keyframes only)")

	flag.Parse()

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.RTSPURL == "" {
		return fmt.Errorf("%w: URL is required", ErrInvalidURL)
	}

	if c.Timeout <= 0 {
		c.Timeout = 10 * time.Second
	}

	if c.OutputDir == "" {
		c.OutputDir = "./frames"
	}

	return nil
}

// String returns a string representation of the configuration
func (c *Config) String() string {
	return fmt.Sprintf(
		"Configuration:\n  RTSP URL: %s\n  Output Dir: %s\n  Timeout: %v\n  Verbose: %t\n  Save JPEG: %t\n  Continuous Decoder: %t",
		c.RTSPURL,
		c.OutputDir,
		c.Timeout,
		c.Verbose,
		c.SaveJPEG,
		c.ContinuousDecoder,
	)
}
