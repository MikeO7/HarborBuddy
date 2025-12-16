package log

import (
	"bytes"
	"encoding/json"
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
