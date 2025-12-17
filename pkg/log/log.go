package log

import (
	"io"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	logger    zerolog.Logger
	loggerMu  sync.RWMutex
	currLevel zerolog.Level
)

// Config holds logging configuration
type Config struct {
	Level      string
	JSON       bool
	File       string
	MaxSize    int // megabytes
	MaxBackups int
	Output     io.Writer // Optional: override output (default stdout)
}

// Initialize sets up the logger with the given configuration
func Initialize(cfg Config) {
	var writers []io.Writer

	// Set up console writer
	if cfg.Output != nil {
		writers = append(writers, cfg.Output)
	} else if !cfg.JSON {
		writers = append(writers, zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.TimeOnly, // e.g., 15:04:05
		})
	} else {
		writers = append(writers, os.Stdout)
	}

	// Set up file writer if configured
	if cfg.File != "" {
		// Ensure the file exists with 0644 permissions so it's readable by the host user
		// This handles the "NoPermissions" error when mounting logs from Docker
		f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err == nil {
			f.Close()
			_ = os.Chmod(cfg.File, 0644)

			fileLogger := &lumberjack.Logger{
				Filename:   cfg.File,
				MaxSize:    cfg.MaxSize,
				MaxBackups: cfg.MaxBackups,
				MaxAge:     0,
				Compress:   false,
			}
			writers = append(writers, fileLogger)
		} else {
			// If we can't open the file, don't try to log to it
			// This prevents noisy errors from lumberjack
		}
	}

	// Create multi-writer
	output := io.MultiWriter(writers...)

	// Parse log level
	logLevel := zerolog.InfoLevel
	switch cfg.Level {
	case "debug":
		logLevel = zerolog.DebugLevel
	case "info":
		logLevel = zerolog.InfoLevel
	case "warn":
		logLevel = zerolog.WarnLevel
	case "error":
		logLevel = zerolog.ErrorLevel
	default:
		logLevel = zerolog.InfoLevel
	}

	// Use SetGlobalLevel for dynamic control
	currLevel = logLevel
	zerolog.SetGlobalLevel(logLevel)

	loggerMu.Lock()
	logger = zerolog.New(output).
		With().
		Timestamp().
		Logger()
	loggerMu.Unlock()
}

// ToggleDebug toggles the log level between Info and Debug
func ToggleDebug() {
	loggerMu.Lock()
	defer loggerMu.Unlock()

	if currLevel == zerolog.InfoLevel {
		currLevel = zerolog.DebugLevel
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		// We can't log using Info() here because it would deadlock (Info uses RLock)
		// Use logger directly since we have the lock
		logger.Info().Msg("🔄 Log level switched to DEBUG via signal")
	} else {
		currLevel = zerolog.InfoLevel
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		logger.Info().Msg("🔄 Log level switched to INFO via signal")
	}
}

// Debug logs a debug message
func Debug(msg string) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Debug().Msg(msg)
}

// Debugf logs a formatted debug message
func Debugf(format string, args ...interface{}) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Debug().Msgf(format, args...)
}

// Info logs an info message
func Info(msg string) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Info().Msg(msg)
}

// Infof logs a formatted info message
func Infof(format string, args ...interface{}) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Info().Msgf(format, args...)
}

// Warn logs a warning message
func Warn(msg string) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Warn().Msg(msg)
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...interface{}) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Warn().Msgf(format, args...)
}

// Error logs an error message
func Error(msg string) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Error().Msg(msg)
}

// Errorf logs a formatted error message
func Errorf(format string, args ...interface{}) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Error().Msgf(format, args...)
}

// ErrorErr logs an error with an error object
func ErrorErr(msg string, err error) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Error().Err(err).Msg(msg)
}

// ErrorWithHint logs an error with an additional hint field
func ErrorWithHint(msg, hint string, err error) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Error().Err(err).Str("hint", hint).Msg(msg)
}

// Fatal logs a fatal message and exits
func Fatal(msg string) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Fatal().Msg(msg)
}

// Fatalf logs a formatted fatal message and exits
func Fatalf(format string, args ...interface{}) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Fatal().Msgf(format, args...)
}

// Panic logs a panic message and panics
func Panic(msg string) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Panic().Msg(msg)
}

// Panicf logs a formatted panic message and panics
func Panicf(format string, args ...interface{}) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Panic().Msgf(format, args...)
}

// WithContainer returns a logger with container context
func WithContainer(containerID, containerName string) *zerolog.Logger {
	l := logger.With().
		Str("container_id", containerID).
		Str("container_name", containerName).
		Logger()
	return &l
}

// WithFields returns a logger with generic fields context
func WithFields(fields map[string]interface{}) *zerolog.Logger {
	l := logger.With().Fields(fields).Logger()
	return &l
}

// WithImage returns a logger with image context
func WithImage(imageID, imageTag string) *zerolog.Logger {
	l := logger.With().
		Str("image_id", imageID).
		Str("image_tag", imageTag).
		Logger()
	return &l
}
