package logger

import (
	"fmt"
	"log"
	"os"
)

// Level represents the logging level
type Level int

const (
	// LevelError shows only error messages
	LevelError Level = iota
	// LevelWarn shows warnings and errors
	LevelWarn
	// LevelInfo shows informational messages, warnings, and errors (default)
	LevelInfo
	// LevelDebug shows all messages including detailed debug information
	LevelDebug
)

// Logger provides leveled logging functionality
type Logger struct {
	level Level
	*log.Logger
}

var (
	defaultLogger *Logger
)

func init() {
	// Initialize default logger with INFO level
	defaultLogger = New(LevelInfo)
}

// New creates a new logger with the specified level
func New(level Level) *Logger {
	return &Logger{
		level:  level,
		Logger: log.New(os.Stderr, "", log.LstdFlags),
	}
}

// SetLevel sets the logging level for the default logger
func SetLevel(level Level) {
	defaultLogger.SetLevel(level)
}

// GetLevel returns the current logging level
func GetLevel() Level {
	return defaultLogger.GetLevel()
}

// SetLevel sets the logging level
func (l *Logger) SetLevel(level Level) {
	l.level = level
}

// GetLevel returns the current logging level
func (l *Logger) GetLevel() Level {
	return l.level
}

// ParseLevel parses a string level name and returns the corresponding Level
func ParseLevel(levelStr string) (Level, error) {
	switch levelStr {
	case "error":
		return LevelError, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "info":
		return LevelInfo, nil
	case "debug":
		return LevelDebug, nil
	default:
		return LevelInfo, fmt.Errorf("unknown log level: %s (valid levels: error, warn, info, debug)", levelStr)
	}
}

// LevelString returns the string representation of a level
func (l Level) String() string {
	switch l {
	case LevelError:
		return "ERROR"
	case LevelWarn:
		return "WARN"
	case LevelInfo:
		return "INFO"
	case LevelDebug:
		return "DEBUG"
	default:
		return "UNKNOWN"
	}
}

// Error logs an error message
func (l *Logger) Error(format string, v ...interface{}) {
	if l.level >= LevelError {
		l.Logger.Printf("[ERROR] "+format, v...)
	}
}

// Warn logs a warning message
func (l *Logger) Warn(format string, v ...interface{}) {
	if l.level >= LevelWarn {
		l.Logger.Printf("[WARN] "+format, v...)
	}
}

// Info logs an informational message
func (l *Logger) Info(format string, v ...interface{}) {
	if l.level >= LevelInfo {
		l.Logger.Printf("[INFO] "+format, v...)
	}
}

// Debug logs a debug message
func (l *Logger) Debug(format string, v ...interface{}) {
	if l.level >= LevelDebug {
		l.Logger.Printf("[DEBUG] "+format, v...)
	}
}

// Errorf is an alias for Error
func (l *Logger) Errorf(format string, v ...interface{}) {
	l.Error(format, v...)
}

// Warnf is an alias for Warn
func (l *Logger) Warnf(format string, v ...interface{}) {
	l.Warn(format, v...)
}

// Infof is an alias for Info
func (l *Logger) Infof(format string, v ...interface{}) {
	l.Info(format, v...)
}

// Debugf is an alias for Debug
func (l *Logger) Debugf(format string, v ...interface{}) {
	l.Debug(format, v...)
}

// Package-level convenience functions that use the default logger

// Error logs an error message using the default logger
func Error(format string, v ...interface{}) {
	defaultLogger.Error(format, v...)
}

// Warn logs a warning message using the default logger
func Warn(format string, v ...interface{}) {
	defaultLogger.Warn(format, v...)
}

// Info logs an informational message using the default logger
func Info(format string, v ...interface{}) {
	defaultLogger.Info(format, v...)
}

// Debug logs a debug message using the default logger
func Debug(format string, v ...interface{}) {
	defaultLogger.Debug(format, v...)
}

// Errorf is an alias for Error using the default logger
func Errorf(format string, v ...interface{}) {
	defaultLogger.Error(format, v...)
}

// Warnf is an alias for Warn using the default logger
func Warnf(format string, v ...interface{}) {
	defaultLogger.Warn(format, v...)
}

// Infof is an alias for Info using the default logger
func Infof(format string, v ...interface{}) {
	defaultLogger.Info(format, v...)
}

// Debugf is an alias for Debug using the default logger
func Debugf(format string, v ...interface{}) {
	defaultLogger.Debug(format, v...)
}


