package logging

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/MikeO7/HarborBuddy/internal/config"
)

func TestTextOutputIncludesDateWithoutColorWhenNotATerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var output bytes.Buffer
	logger, _, closeLogger, err := New(config.Default().Log, &output)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeLogger() })
	logger.Info().Msg("dated")
	text := output.String()
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("text output to a buffer contains ANSI color: %q", text)
	}
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`).MatchString(text) {
		t.Fatalf("text output timestamp lacks a date: %q", text)
	}
}

func TestJSONOutputIsUnchanged(t *testing.T) {
	cfg := config.Default().Log
	cfg.JSON = true
	var output bytes.Buffer
	logger, _, closeLogger, err := New(cfg, &output)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeLogger() })
	logger.Info().Msg("json")
	if text := output.String(); !strings.HasPrefix(text, `{"level":"info"`) || strings.Contains(text, "\x1b[") {
		t.Fatalf("JSON output = %q", text)
	}
}

func TestUseColor(t *testing.T) {
	// /dev/null is a character device, like a terminal.
	device, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = device.Close() })
	regular, err := os.Create(filepath.Join(t.TempDir(), "out.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = regular.Close() })
	closed, err := os.Create(filepath.Join(t.TempDir(), "closed.log"))
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("NO_COLOR", "")
	if !useColor(device) {
		t.Fatal("useColor(character device) = false, want true")
	}
	for name, out := range map[string]io.Writer{"buffer": &bytes.Buffer{}, "regular file": regular, "closed file": closed} {
		if useColor(out) {
			t.Fatalf("useColor(%s) = true, want false", name)
		}
	}
	t.Setenv("NO_COLOR", "1")
	if useColor(device) {
		t.Fatal("useColor with NO_COLOR=1 = true, want false")
	}
}
