package log

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

var logger zerolog.Logger

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
			TimeFormat: time.RFC3339,
		})
	} else {
		writers = append(writers, os.Stdout)
	}

	// Set up file writer if configured
	if cfg.File != "" {
		fileLogger := &lumberjack.Logger{
			Filename:   cfg.File,
			MaxSize:    cfg.MaxSize,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     0,
			Compress:   false,
		}
		writers = append(writers, fileLogger)
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

	logger = zerolog.New(output).
		Level(logLevel).
		With().
		Timestamp().
		Logger()
}

// Debug logs a debug message
func Debug(msg string) {
	logger.Debug().Msg(msg)
}

// Debugf logs a formatted debug message
func Debugf(format string, args ...interface{}) {
	logger.Debug().Msgf(format, args...)
}

// Info logs an info message
func Info(msg string) {
	logger.Info().Msg(msg)
}

// Infof logs a formatted info message
func Infof(format string, args ...interface{}) {
	logger.Info().Msgf(format, args...)
}

// Warn logs a warning message
func Warn(msg string) {
	logger.Warn().Msg(msg)
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...interface{}) {
	logger.Warn().Msgf(format, args...)
}

// Error logs an error message
func Error(msg string) {
	logger.Error().Msg(msg)
}

// Errorf logs a formatted error message
func Errorf(format string, args ...interface{}) {
	logger.Error().Msgf(format, args...)
}

// ErrorErr logs an error with an error object
func ErrorErr(msg string, err error) {
	logger.Error().Err(err).Msg(msg)
}

// Fatal logs a fatal message and exits
func Fatal(msg string) {
	logger.Fatal().Msg(msg)
}

// Fatalf logs a formatted fatal message and exits
func Fatalf(format string, args ...interface{}) {
	logger.Fatal().Msgf(format, args...)
}

// Panic logs a panic message and panics
func Panic(msg string) {
	logger.Panic().Msg(msg)
}

// Panicf logs a formatted panic message and panics
func Panicf(format string, args ...interface{}) {
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
