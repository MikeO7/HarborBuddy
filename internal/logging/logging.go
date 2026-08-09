package logging

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

// LevelController switches between debug logging and the configured base level.
type LevelController struct {
	mu      sync.Mutex
	base    zerolog.Level
	current zerolog.Level
}

var closeLogFile = func(file *os.File) error { return file.Close() }

// ToggleDebug enables debug logging, or restores the configured base level if
// debug logging is already enabled.
func (c *LevelController) ToggleDebug() zerolog.Level {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.current == zerolog.DebugLevel {
		c.current = c.base
	} else {
		c.current = zerolog.DebugLevel
	}
	zerolog.SetGlobalLevel(c.current)
	return c.current
}

// BaseLevel returns the configured level that ToggleDebug restores.
func (c *LevelController) BaseLevel() zerolog.Level {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.base
}

// CurrentLevel returns the currently active process logging level.
func (c *LevelController) CurrentLevel() zerolog.Level {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// New constructs the application logger and returns a function that closes any
// configured rotating file writer.
func New(cfg config.LogConfig, stdout io.Writer) (zerolog.Logger, *LevelController, func() error, error) {
	level, err := zerolog.ParseLevel(cfg.Level)
	if err != nil {
		return zerolog.Logger{}, nil, nil, fmt.Errorf("invalid log level %q: %w", cfg.Level, err)
	}
	if stdout == nil {
		stdout = os.Stdout
	}

	output := stdout
	if !cfg.JSON {
		output = zerolog.ConsoleWriter{Out: stdout, TimeFormat: time.TimeOnly}
	}

	var fileWriter *lumberjack.Logger
	if cfg.File != "" {
		file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return zerolog.Logger{}, nil, nil, fmt.Errorf("open log file %q: %w", cfg.File, err)
		}
		if err := closeLogFile(file); err != nil {
			return zerolog.Logger{}, nil, nil, fmt.Errorf("close log file %q after validation: %w", cfg.File, err)
		}
		fileWriter = &lumberjack.Logger{
			Filename:   cfg.File,
			MaxSize:    cfg.MaxSize,
			MaxBackups: cfg.MaxBackups,
		}
		output = io.MultiWriter(output, fileWriter)
	}

	zerolog.SetGlobalLevel(level)
	logger := zerolog.New(output).With().Timestamp().Logger()
	controller := &LevelController{base: level, current: level}
	closeFn := func() error {
		if fileWriter == nil {
			return nil
		}
		return fileWriter.Close()
	}
	return logger, controller, closeFn, nil
}

// NotifyLevelSignals handles SIGUSR1 until ctx is canceled. Repeated signals
// alternate between debug and the configured base level. The returned function
// unregisters the signal and stops the goroutine.
func NotifyLevelSignals(ctx context.Context, logger zerolog.Logger, controller *LevelController) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGUSR1)
	signalCtx, cancel := context.WithCancel(ctx)
	go handleLevelSignals(signalCtx, logger, controller, signals)

	var once sync.Once
	return func() {
		once.Do(func() {
			signal.Stop(signals)
			cancel()
		})
	}
}

func handleLevelSignals(ctx context.Context, logger zerolog.Logger, controller *LevelController, signals <-chan os.Signal) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-signals:
			level := controller.ToggleDebug()
			logger.Info().Str("level", level.String()).Msg("Log level changed by SIGUSR1")
		}
	}
}
