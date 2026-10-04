package app

import (
	"context"
	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/logging"
	"github.com/rs/zerolog"
	"io"
	"strings"
	"testing"
)

func TestPositionalArgumentsRejectedBeforeSideEffects(t *testing.T) {
	for _, args := range [][]string{{"--once", "extra"}, {"--version", "extra"}, {"--", "secret"}, {"--once", strings.Repeat("x", 65536)}} {
		deps := testDependencies(nil)
		deps.NewLogger = func(config.LogConfig, io.Writer) (zerolog.Logger, *logging.LevelController, func() error, error) {
			t.Fatal("unexpected logger initialization")
			return zerolog.Nop(), nil, nil, nil
		}
		if err := RunWithDependencies(context.Background(), args, io.Discard, io.Discard, deps); err == nil || !strings.Contains(err.Error(), "positional arguments") {
			t.Fatalf("error = %v", err)
		}
	}
}
