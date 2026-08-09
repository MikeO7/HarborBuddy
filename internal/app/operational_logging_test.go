package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/logging"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	"github.com/rs/zerolog"
)

func TestEffectiveConfigurationListsEnabledCleanupCategories(t *testing.T) {
	cfg := config.Default()
	cfg.Cleanup.StoppedContainers = true
	cfg.Cleanup.UnusedNetworks = true
	cfg.Cleanup.UnusedVolumes = true
	cfg.Cleanup.BuildCache = true
	if got := cleanupCategories(cfg.Cleanup); got != "images,containers,networks,volumes,build_cache" {
		t.Fatalf("cleanupCategories() = %q", got)
	}
	cfg.Cleanup = config.CleanupConfig{All: true}
	if got := cleanupCategories(cfg.Cleanup); got != "images,containers,networks,volumes,build_cache" {
		t.Fatalf("cleanupCategories(all) = %q", got)
	}
}

func TestEffectiveConfigurationLogsRollbackImageRetention(t *testing.T) {
	var output bytes.Buffer
	cfg := config.Default()
	cfg.Updates.RollbackImageRetention = 2
	logEffectiveConfig(zerolog.New(&output), cfg)
	if text := output.String(); !strings.Contains(text, `"rollback_image_retention":2`) {
		t.Fatalf("effective configuration log = %q", text)
	}
}

func TestHelperCompletionLogsWarningsAndRollbackOutcomes(t *testing.T) {
	for _, test := range []struct {
		result docker.ReplaceResult
		want   string
	}{
		{result: docker.ReplaceResult{}, want: "not_attempted"},
		{result: docker.ReplaceResult{RollbackAttempted: true}, want: "succeeded"},
		{result: docker.ReplaceResult{RollbackAttempted: true, RollbackErr: errors.New("rollback")}, want: "failed"},
	} {
		if got := rollbackOutcome(test.result); got != test.want {
			t.Errorf("rollbackOutcome(%+v) = %q", test.result, got)
		}
	}
	if shortOperationalID("sha256:1234567890123456") != "123456789012" || shortOperationalID("short") != "short" {
		t.Fatal("shortOperationalID did not normalize IDs")
	}

	var output bytes.Buffer
	deps := testDependencies(nil)
	deps.NewLogger = func(config.LogConfig, io.Writer) (zerolog.Logger, *logging.LevelController, func() error, error) {
		return zerolog.New(&output), &logging.LevelController{}, func() error { return nil }, nil
	}
	deps.RunHelper = func(context.Context, docker.Client, selfupdate.UpdaterRequest) (docker.ReplaceResult, error) {
		return docker.ReplaceResult{NewContainerID: "new-container", BackupCleanupErr: errors.New("backup retained")}, nil
	}
	values := &flagValues{targetContainer: "target", newImage: "image"}
	if err := runHelper(context.Background(), &output, deps, values); err != nil {
		t.Fatal(err)
	}
	if text := output.String(); !strings.Contains(text, `"level":"warn"`) || !strings.Contains(text, "backup retained") || !strings.Contains(text, "self_update_helper_complete") {
		t.Fatalf("helper completion log = %q", text)
	}
}

func TestHelperRejectsInvalidLoggingEnvironment(t *testing.T) {
	deps := testDependencies(map[string]string{"HARBORBUDDY_LOG_JSON": "not-a-boolean"})
	err := runHelper(context.Background(), &bytes.Buffer{}, deps, &flagValues{targetContainer: "target", newImage: "image"})
	if err == nil || !strings.Contains(err.Error(), "load helper environment") {
		t.Fatalf("runHelper() error = %v", err)
	}
}
