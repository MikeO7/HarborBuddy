package log

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestInitialize(t *testing.T) {
	// Capture output
	var buf bytes.Buffer

	cfg := Config{
		Level:  "debug",
		JSON:   true,
		Output: &buf,
	}

	Initialize(cfg)

	// Log something
	Info("test message")

	// Verify output
	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("Expected log output to contain 'test message', got: %s", output)
	}

	// Verify JSON format
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logEntry); err != nil {
		t.Errorf("Expected valid JSON log output: %v", err)
	}

	if logEntry["message"] != "test message" {
		t.Errorf("Expected message 'test message', got %v", logEntry["message"])
	}

	if logEntry["level"] != "info" {
		t.Errorf("Expected level 'info', got %v", logEntry["level"])
	}
}

func TestHelperFunctions(t *testing.T) {
	// Setup to capture output
	var buf bytes.Buffer
	logger = zerolog.New(&buf).Level(zerolog.DebugLevel)

	t.Run("Debug", func(t *testing.T) {
		buf.Reset()
		Debug("debug msg")
		if !strings.Contains(buf.String(), "debug msg") {
			t.Errorf("Expected output to contain 'debug msg'")
		}
	})

	t.Run("Info", func(t *testing.T) {
		buf.Reset()
		Info("info msg")
		if !strings.Contains(buf.String(), "info msg") {
			t.Errorf("Expected output to contain 'info msg'")
		}
	})

	t.Run("Warn", func(t *testing.T) {
		buf.Reset()
		Warn("warn msg")
		if !strings.Contains(buf.String(), "warn msg") {
			t.Errorf("Expected output to contain 'warn msg'")
		}
	})

	t.Run("Error", func(t *testing.T) {
		buf.Reset()
		Error("error msg")
		if !strings.Contains(buf.String(), "error msg") {
			t.Errorf("Expected output to contain 'error msg'")
		}
	})

	t.Run("Formatted", func(t *testing.T) {
		buf.Reset()
		Infof("hello %s", "world")
		if !strings.Contains(buf.String(), "hello world") {
			t.Errorf("Expected output to contain 'hello world'")
		}
	})
}

func TestContextHelpers(t *testing.T) {
	var buf bytes.Buffer
	logger = zerolog.New(&buf)

	t.Run("WithContainer", func(t *testing.T) {
		buf.Reset()
		l := WithContainer("cid123", "cname")
		l.Info().Msg("test")

		output := buf.String()
		if !strings.Contains(output, "cid123") || !strings.Contains(output, "cname") {
			t.Errorf("Expected output to contain container fields, got: %s", output)
		}
	})

	t.Run("WithImage", func(t *testing.T) {
		buf.Reset()
		l := WithImage("iid456", "tag:latest")
		l.Info().Msg("test")

		output := buf.String()
		if !strings.Contains(output, "iid456") || !strings.Contains(output, "tag:latest") {
			t.Errorf("Expected output to contain image fields, got: %s", output)
		}
	})

	t.Run("WithFields", func(t *testing.T) {
		buf.Reset()
		fields := map[string]interface{}{
			"foo":   "bar",
			"count": 42,
		}
		l := WithFields(fields)
		l.Info().Msg("test")

		output := buf.String()
		if !strings.Contains(output, "foo") || !strings.Contains(output, "bar") || !strings.Contains(output, "42") {
			t.Errorf("Expected output to contain custom fields, got: %s", output)
		}
	})
}

func TestLogLevels(t *testing.T) {
	levels := []struct {
		cfgLevel  string
		wantLevel zerolog.Level
	}{
		{"debug", zerolog.DebugLevel},
		{"info", zerolog.InfoLevel},
		{"warn", zerolog.WarnLevel},
		{"error", zerolog.ErrorLevel},
		{"invalid", zerolog.InfoLevel},
	}

	for _, tt := range levels {
		t.Run(tt.cfgLevel, func(t *testing.T) {
			Initialize(Config{Level: tt.cfgLevel})
			if logger.GetLevel() != tt.wantLevel {
				t.Errorf("Expected level %v for config %s, got %v", tt.wantLevel, tt.cfgLevel, logger.GetLevel())
			}
		})
	}
}

func TestFileLogging(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "testlog")
	if err != nil {
		t.Fatal(err)
	}
	tmpFileName := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpFileName)

	cfg := Config{
		File:  tmpFileName,
		Level: "info",
	}

	Initialize(cfg)
	Info("file log test")

	// Check if file contains content
	content, err := os.ReadFile(tmpFileName)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if !strings.Contains(string(content), "file log test") {
		t.Errorf("Log file did not contain expected message")
	}
}

func TestFormattedLogging(t *testing.T) {
	var buf bytes.Buffer
	logger = zerolog.New(&buf).Level(zerolog.DebugLevel)

	t.Run("Debugf", func(t *testing.T) {
		buf.Reset()
		Debugf("debug %s %d", "test", 42)
		if !strings.Contains(buf.String(), "debug test 42") {
			t.Errorf("Expected output to contain 'debug test 42', got: %s", buf.String())
		}
	})

	t.Run("Warnf", func(t *testing.T) {
		buf.Reset()
		Warnf("warning %s", "message")
		if !strings.Contains(buf.String(), "warning message") {
			t.Errorf("Expected output to contain 'warning message', got: %s", buf.String())
		}
	})

	t.Run("Errorf", func(t *testing.T) {
		buf.Reset()
		Errorf("error %d", 500)
		if !strings.Contains(buf.String(), "error 500") {
			t.Errorf("Expected output to contain 'error 500', got: %s", buf.String())
		}
	})
}

func TestErrorWithErr(t *testing.T) {
	var buf bytes.Buffer
	logger = zerolog.New(&buf).Level(zerolog.ErrorLevel)

	testErr := fmt.Errorf("test error")
	ErrorErr("operation failed", testErr)

	output := buf.String()
	if !strings.Contains(output, "operation failed") {
		t.Errorf("Expected output to contain 'operation failed', got: %s", output)
	}
	if !strings.Contains(output, "test error") {
		t.Errorf("Expected output to contain 'test error', got: %s", output)
	}
}

func TestPanicLogging(t *testing.T) {
	var buf bytes.Buffer
	logger = zerolog.New(&buf)

	t.Run("Panic", func(t *testing.T) {
		buf.Reset()
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("Expected Panic to panic, but it didn't")
			}
		}()
		Panic("panic message")
	})

	t.Run("Panicf", func(t *testing.T) {
		buf.Reset()
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("Expected Panicf to panic, but it didn't")
			}
		}()
		Panicf("panic %s", "formatted")
	})
}

func TestInitialize_JSONMode(t *testing.T) {
	// Test JSON mode (else branch in Initialize)
	cfg := Config{
		Level: "info",
		JSON:  true,
		// No Output specified - should use os.Stdout
	}

	// This just exercises the code path - we can't easily capture os.Stdout
	Initialize(cfg)

	// Verify logger works
	Info("test JSON mode")
}

func TestInitialize_FileError(t *testing.T) {
	// Test with an invalid file path that will fail to open
	cfg := Config{
		Level: "info",
		File:  "/nonexistent/directory/that/does/not/exist/logfile.log",
	}

	// Should not panic, just skip file logging
	Initialize(cfg)
	Info("test file error handling")
}
