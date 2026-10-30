package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/health"
	"github.com/rs/zerolog"
)

func writeHealthFile(t *testing.T, heartbeat time.Time) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "health.json")
	content := `{"heartbeat":"` + heartbeat.UTC().Format(time.RFC3339Nano) + `"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHealthcheckModeReportsDaemonState(t *testing.T) {
	fresh := writeHealthFile(t, time.Now())
	var stdout bytes.Buffer
	deps := testDependencies(map[string]string{health.PathEnv: fresh})
	if err := RunWithDependencies(context.Background(), []string{"--healthcheck"}, &stdout, &bytes.Buffer{}, deps); err != nil || stdout.String() != "healthy\n" {
		t.Fatalf("healthy check = %q, %v", stdout.String(), err)
	}

	stale := writeHealthFile(t, time.Now().Add(-time.Hour))
	deps = testDependencies(map[string]string{health.PathEnv: stale})
	err := RunWithDependencies(context.Background(), []string{"--healthcheck"}, &bytes.Buffer{}, &bytes.Buffer{}, deps)
	if err == nil || !strings.Contains(err.Error(), "unhealthy: last successful Docker ping") {
		t.Fatalf("stale check error = %v", err)
	}

	deps = testDependencies(map[string]string{health.PathEnv: fresh})
	if err := RunWithDependencies(context.Background(), []string{"--healthcheck"}, failingWriter{}, &bytes.Buffer{}, deps); err == nil {
		t.Fatal("healthcheck ignored a stdout write failure")
	}
}

type sequencePinger struct {
	mu      sync.Mutex
	results []error
	calls   int
}

func (p *sequencePinger) Ping(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.calls > len(p.results) {
		return nil
	}
	return p.results[p.calls-1]
}

func TestHealthMonitorLogsTransitionsAndTracksCycles(t *testing.T) {
	previous := healthInterval
	healthInterval = time.Millisecond
	t.Cleanup(func() { healthInterval = previous })

	path := filepath.Join(t.TempDir(), "health.json")
	deps := testDependencies(map[string]string{health.PathEnv: path})
	var logs bytes.Buffer
	logger := zerolog.New(&logs)
	pinger := &sequencePinger{results: []error{errors.New("daemon down")}}

	ctx, stop := startHealthMonitor(context.Background(), config.Default(), deps, pinger, logger)
	deadline := time.Now().Add(5 * time.Second)
	for health.Check(path, time.Now()) != nil {
		if time.Now().After(deadline) {
			t.Fatalf("health file never became healthy: %v", health.Check(path, time.Now()))
		}
		time.Sleep(time.Millisecond)
	}
	health.TrackCycle(ctx)()
	stop()

	for _, event := range []string{"health_heartbeat_failed", "health_heartbeat_recovered"} {
		if !strings.Contains(logs.String(), event) {
			t.Fatalf("logs missing %s: %s", event, logs.String())
		}
	}
}

func TestHealthMonitorIsSkippedForOneShotModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "health.json")
	deps := testDependencies(map[string]string{health.PathEnv: path})
	for _, cfg := range []config.Config{{RunOnce: true}, {CleanupOnly: true}} {
		ctx, stop := startHealthMonitor(context.Background(), cfg, deps, &sequencePinger{}, zerolog.Nop())
		stop()
		if ctx != context.Background() {
			t.Fatalf("one-shot mode %+v got a monitored context", cfg)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("one-shot mode wrote a health file: %v", err)
	}
}

func TestDaemonSchedulerRunsWithHealthMonitor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "health.json")
	deps := testDependencies(map[string]string{health.PathEnv: path})
	client := &fakeDockerClient{pingOK: true}
	deps.NewDockerClient = func(context.Context, string) (DockerClient, error) { return client, nil }
	deps.RunScheduler = func(ctx context.Context, _ config.Config, _ docker.Client, _ zerolog.Logger) error {
		deadline := time.Now().Add(5 * time.Second)
		for health.Check(path, time.Now()) != nil {
			if time.Now().After(deadline) {
				return errors.New("daemon never wrote a healthy status")
			}
			time.Sleep(time.Millisecond)
		}
		return nil
	}
	if err := runDaemon(context.Background(), &bytes.Buffer{}, deps, config.Default()); err != nil {
		t.Fatalf("runDaemon() error = %v", err)
	}
}
