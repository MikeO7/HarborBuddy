package app

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/logging"
	"github.com/rs/zerolog"
)

func TestRunDaemonLogsConfigurationWarnings(t *testing.T) {
	var output bytes.Buffer
	deps := testDependencies(nil)
	deps.NewLogger = func(config.LogConfig, io.Writer) (zerolog.Logger, *logging.LevelController, func() error, error) {
		return zerolog.New(&output), &logging.LevelController{}, func() error { return nil }, nil
	}
	deps.Environ = func() []string { return []string{"HARBORBUDDY_SELF_UPDATE=false", "HARBORBUDDY_LOG_LEVEL=info"} }
	cfg := config.Default()
	cfg.Updates.CheckInterval = 5 * time.Second
	if err := runDaemon(context.Background(), &bytes.Buffer{}, deps, cfg); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{
		`"level":"warn","event":"configuration_warning","setting":"updates.check_interval"`,
		`"setting":"HARBORBUDDY_SELF_UPDATE","message":"unrecognized environment variable is ignored; did you mean HARBORBUDDY_SELF_UPDATE_ENABLED?"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("daemon log lacks %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"setting":"HARBORBUDDY_LOG_LEVEL"`) {
		t.Fatalf("known variable was reported as unknown:\n%s", text)
	}
}

func TestDefaultDependenciesReadProcessEnvironment(t *testing.T) {
	t.Setenv("HARBORBUDDY_WARNING_PROBE", "1")
	found := false
	for _, entry := range defaultDependencies().Environ() {
		found = found || entry == "HARBORBUDDY_WARNING_PROBE=1"
	}
	if !found {
		t.Fatal("default Environ dependency does not read the process environment")
	}
}
