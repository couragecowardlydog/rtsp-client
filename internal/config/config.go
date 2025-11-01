package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rtsp-client/pkg/logger"

	"gopkg.in/yaml.v3"
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
	Verbose           bool      // Deprecated: use LogLevel instead, kept for backward compatibility
	LogLevel          string    // Log level: error, warn, info, debug (default: info)
	SaveJPEG          bool
	ContinuousDecoder bool // Enable continuous decoder session (can decode P-frames)
}

// yamlConfig represents the YAML configuration structure
type yamlConfig struct {
	RTSPURL           string `yaml:"rtsp_url"`
	OutputDir         string `yaml:"output_dir"`
	Timeout           string `yaml:"timeout"`
	Verbose           bool   `yaml:"verbose"`             // Deprecated: use log_level instead
	LogLevel          string `yaml:"log_level"`           // Log level: error, warn, info, debug
	SaveJPEG          bool   `yaml:"save_jpeg"`
	ContinuousDecoder bool   `yaml:"continuous_decoder"`
}

// LoadFromYAML loads configuration from a YAML file
func LoadFromYAML(filePath string) (*Config, map[string]bool, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read YAML file: %w", err)
	}

	// First unmarshal to a map to detect which fields were present
	var rawMap map[string]interface{}
	if err := yaml.Unmarshal(data, &rawMap); err != nil {
		return nil, nil, fmt.Errorf("failed to parse YAML file: %w", err)
	}

	// Track which fields were present in YAML
	present := make(map[string]bool)
	for key := range rawMap {
		// Convert YAML key to our internal key names
		switch key {
		case "rtsp_url", "output_dir", "timeout", "verbose", "log_level", "save_jpeg", "continuous_decoder":
			present[key] = true
		}
	}

	// Now unmarshal to our struct
	var yamlCfg yamlConfig
	if err := yaml.Unmarshal(data, &yamlCfg); err != nil {
		return nil, nil, fmt.Errorf("failed to parse YAML file: %w", err)
	}

	// Convert YAML config to Config struct
	config := &Config{
		RTSPURL:           yamlCfg.RTSPURL,
		OutputDir:         yamlCfg.OutputDir,
		Verbose:           yamlCfg.Verbose,
		LogLevel:          yamlCfg.LogLevel,
		SaveJPEG:          yamlCfg.SaveJPEG,
		ContinuousDecoder: yamlCfg.ContinuousDecoder,
	}

	// Parse timeout duration from string
	if yamlCfg.Timeout != "" {
		duration, err := time.ParseDuration(yamlCfg.Timeout)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid timeout value in YAML: %w", err)
		}
		config.Timeout = duration
	}

	return config, present, nil
}

// setDefaults sets default values for the configuration
func (c *Config) setDefaults() {
	if c.OutputDir == "" {
		c.OutputDir = "./frames"
	}
	if c.Timeout == 0 {
		c.Timeout = 10 * time.Second
	}
	if !c.SaveJPEG {
		c.SaveJPEG = true
	}
	if !c.ContinuousDecoder {
		c.ContinuousDecoder = true
	}
	// Set default log level to "info" if not specified
	// If Verbose is true, override to "debug" for backward compatibility
	if c.LogLevel == "" {
		if c.Verbose {
			c.LogLevel = "debug"
		} else {
			c.LogLevel = "info"
		}
	}
}

// merge merges values from another config, using the presence map to determine which fields to merge
func (c *Config) merge(other *Config, present map[string]bool) {
	if present["rtsp_url"] && other.RTSPURL != "" {
		c.RTSPURL = other.RTSPURL
	}
	if present["output_dir"] && other.OutputDir != "" {
		c.OutputDir = other.OutputDir
	}
	if present["timeout"] && other.Timeout != 0 {
		c.Timeout = other.Timeout
	}
	if present["verbose"] {
		c.Verbose = other.Verbose
	}
	if present["log_level"] && other.LogLevel != "" {
		c.LogLevel = other.LogLevel
	}
	if present["save_jpeg"] {
		c.SaveJPEG = other.SaveJPEG
	}
	if present["continuous_decoder"] {
		c.ContinuousDecoder = other.ContinuousDecoder
	}
}

// isYAMLFile checks if the given path is a YAML file
func isYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yml" || ext == ".yaml"
}

// ParseFlags parses command-line flags and returns configuration
// Supports YAML configuration file as first positional argument
// Priority order: Defaults < YAML < Command-line flags
func ParseFlags() (*Config, error) {
	// Start with default values
	config := &Config{}
	config.setDefaults()

	// Check for YAML file in command line arguments before parsing flags
	// We need to do this manually since flag.Parse() consumes all args
	args := os.Args[1:]
	yamlPath := ""
	newArgs := []string{}

	for i, arg := range args {
		// Skip flag arguments
		if strings.HasPrefix(arg, "-") {
			newArgs = append(newArgs, arg)
			continue
		}
		// First non-flag argument that is a YAML file
		if i == 0 || (i > 0 && !strings.HasPrefix(args[i-1], "-")) {
			if isYAMLFile(arg) {
				yamlPath = arg
				// Don't add YAML path to newArgs as it's not a flag
				continue
			}
		}
		newArgs = append(newArgs, arg)
	}

	// Load YAML config if found (overrides defaults)
	if yamlPath != "" {
		yamlConfig, present, err := LoadFromYAML(yamlPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load YAML config: %w", err)
		}
		// Merge YAML values (they override defaults)
		config.merge(yamlConfig, present)
	}

	// Now set up flags and parse remaining arguments
	// Use separate flag variables to detect if flags were explicitly set
	var flagURL string
	var flagOutputDir string
	var flagTimeout time.Duration
	var flagVerbose bool
	var flagLogLevel string
	var flagSaveJPEG bool
	var flagContinuousDecoder bool

	flag.StringVar(&flagURL, "url", "", "RTSP stream URL (required)")
	flag.StringVar(&flagOutputDir, "output", "", "Output directory for frames")
	flag.DurationVar(&flagTimeout, "timeout", 0, "Connection timeout")
	flag.BoolVar(&flagVerbose, "verbose", false, "Enable verbose logging (deprecated: use -log-level debug instead)")
	flag.StringVar(&flagLogLevel, "log-level", "", "Log level: error, warn, info, debug (default: info)")
	flag.BoolVar(&flagSaveJPEG, "jpeg", false, "Save frames as JPEG images (requires ffmpeg)")
	flag.BoolVar(&flagContinuousDecoder, "continuous-decoder", false, "Use continuous decoder session (can decode P-frames, default: true). Set to false for frame-by-frame mode (keyframes only)")

	// Temporarily replace os.Args to exclude the YAML file path
	oldArgs := os.Args
	if yamlPath != "" {
		os.Args = []string{oldArgs[0]}
		os.Args = append(os.Args, newArgs...)
	}
	flag.Parse()
	os.Args = oldArgs

	// Merge flag values (flags override YAML and defaults)
	if flagURL != "" {
		config.RTSPURL = flagURL
	}
	if flagOutputDir != "" {
		config.OutputDir = flagOutputDir
	}
	if flagTimeout != 0 {
		config.Timeout = flagTimeout
	}
	// For boolean flags, check if they were set by looking at flag.NFlag() or by using a different approach
	// Actually, we can use flag.Visit to see which flags were explicitly set
	flagSet := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		flagSet[f.Name] = true
	})

	if flagSet["verbose"] {
		config.Verbose = flagVerbose
		// If verbose is set via flag and log-level not set, override log level to debug
		if flagVerbose && !flagSet["log-level"] {
			config.LogLevel = "debug"
		}
	}
	if flagSet["log-level"] {
		config.LogLevel = flagLogLevel
		// If log-level is set, disable verbose flag
		if flagLogLevel != "" {
			config.Verbose = false
		}
	}
	if flagSet["jpeg"] {
		config.SaveJPEG = flagSaveJPEG
	}
	if flagSet["continuous-decoder"] {
		config.ContinuousDecoder = flagContinuousDecoder
	}

	// Re-apply defaults for any values that are still empty/zero
	config.setDefaults()

	// Validate configuration
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

	// Validate log level
	if c.LogLevel != "" {
		_, err := logger.ParseLevel(c.LogLevel)
		if err != nil {
			return fmt.Errorf("invalid log level: %w", err)
		}
	}

	return nil
}

// GetLogLevel returns the logger.Level for the configured log level
func (c *Config) GetLogLevel() logger.Level {
	// Apply backward compatibility: if Verbose is true and LogLevel not set, use debug
	if c.Verbose && c.LogLevel == "" {
		return logger.LevelDebug
	}
	
	if c.LogLevel == "" {
		return logger.LevelInfo
	}
	
	level, err := logger.ParseLevel(c.LogLevel)
	if err != nil {
		// Default to info if parsing fails
		return logger.LevelInfo
	}
	return level
}

// String returns a string representation of the configuration
func (c *Config) String() string {
	logLevel := c.LogLevel
	if logLevel == "" {
		if c.Verbose {
			logLevel = "debug (via verbose flag)"
		} else {
			logLevel = "info"
		}
	}
	return fmt.Sprintf(
		"Configuration:\n  RTSP URL: %s\n  Output Dir: %s\n  Timeout: %v\n  Log Level: %s\n  Save JPEG: %t\n  Continuous Decoder: %t",
		c.RTSPURL,
		c.OutputDir,
		c.Timeout,
		logLevel,
		c.SaveJPEG,
		c.ContinuousDecoder,
	)
}
