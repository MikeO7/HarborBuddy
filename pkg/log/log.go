package log

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

var logger zerolog.Logger

// Initialize sets up the logger with the given level and output format
func Initialize(level string, jsonOutput bool) {
	var output io.Writer = os.Stdout

	// Set up console writer for pretty output if not JSON
	if !jsonOutput {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	}

	// Parse log level
	logLevel := zerolog.InfoLevel
	switch level {
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

// WithContainer returns a logger with container context
func WithContainer(containerID, containerName string) *zerolog.Logger {
	l := logger.With().
		Str("container_id", containerID).
		Str("container_name", containerName).
		Logger()
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
