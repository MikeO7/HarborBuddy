package scheduler

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/rs/zerolog"
)

func TestCleanupOnlyFailureKeepsCycleCorrelation(t *testing.T) {
	s := newScheduler(fixedClock{now: time.Now()})
	s.cycleID = func(time.Time) string { return "cycle-123" }
	s.runCleanup = func(context.Context, config.Config, docker.Client, zerolog.Logger) error {
		return errors.New("cleanup failed")
	}
	var output bytes.Buffer
	err := s.runCleanupOnly(context.Background(), config.Default(), nil, zerolog.New(&output))
	if err == nil {
		t.Fatal("cleanup-only failure returned nil")
	}
	text := output.String()
	if !strings.Contains(text, `"cycle_id":"cycle-123"`) || !strings.Contains(text, `"outcome":"failed"`) || !strings.Contains(text, `"level":"error"`) {
		t.Fatalf("cleanup-only failure log = %q", text)
	}
}

func TestOperationalShortID(t *testing.T) {
	if shortID("1234567890123456") != "123456789012" || shortID("short") != "short" {
		t.Fatal("shortID did not preserve expected forms")
	}
}
