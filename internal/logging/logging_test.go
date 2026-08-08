package logging

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/rs/zerolog"
)

func TestNewRejectsInvalidLevel(t *testing.T) {
	cfg := config.Default().Log
	cfg.Level = "verbose"
	if _, _, _, err := New(cfg, &bytes.Buffer{}); err == nil {
		t.Fatal("New() error = nil, want invalid level error")
	}
}

func TestNewWritesConsoleOutput(t *testing.T) {
	cfg := config.Default().Log
	var output bytes.Buffer
	logger, _, closeLogger, err := New(cfg, &output)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := closeLogger(); err != nil {
			t.Errorf("close logger: %v", err)
		}
	})

	logger.Info().Str("component", "test").Msg("hello")
	if text := output.String(); !strings.Contains(text, "hello") || !strings.Contains(text, "component") || !strings.Contains(text, "test") {
		t.Fatalf("console output = %q", text)
	}
}

func TestNewReturnsExplicitFileError(t *testing.T) {
	cfg := config.Default().Log
	cfg.File = t.TempDir()
	if _, _, _, err := New(cfg, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "open log file") {
		t.Fatalf("New() error = %v, want explicit file error", err)
	}
}

func TestNewWritesJSONToFile(t *testing.T) {
	cfg := config.Default().Log
	cfg.File = filepath.Join(t.TempDir(), "harborbuddy.log")
	var output bytes.Buffer
	logger, _, closeLogger, err := New(cfg, &output)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info().Msg("file message")
	if err := closeLogger(); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(cfg.File)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(contents, []byte(`"message":"file message"`)) {
		t.Fatalf("file output = %q", contents)
	}
}

func TestLevelControllerRestoresBaseLevel(t *testing.T) {
	cfg := config.Default().Log
	cfg.Level = "warn"
	_, controller, closeLogger, err := New(cfg, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := closeLogger(); err != nil {
			t.Errorf("close logger: %v", err)
		}
	})

	if got := controller.ToggleDebug(); got != zerolog.DebugLevel {
		t.Fatalf("first ToggleDebug() = %v, want debug", got)
	}
	if got := controller.ToggleDebug(); got != zerolog.WarnLevel {
		t.Fatalf("second ToggleDebug() = %v, want configured warn", got)
	}
}

func TestHandleLevelSignalsTogglesAndRestores(t *testing.T) {
	cfg := config.Default().Log
	cfg.Level = "error"
	logger, controller, closeLogger, err := New(cfg, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := closeLogger(); err != nil {
			t.Errorf("close logger: %v", err)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 2)
	go handleLevelSignals(ctx, logger, controller, signals)

	signals <- syscall.SIGUSR1
	waitForLevel(t, controller, zerolog.DebugLevel)
	signals <- syscall.SIGUSR1
	waitForLevel(t, controller, zerolog.ErrorLevel)
}

func waitForLevel(t *testing.T, controller *LevelController, want zerolog.Level) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if controller.CurrentLevel() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("current level = %v, want %v", controller.CurrentLevel(), want)
}
