package docker

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
)

func TestReadinessStateValidationAndHealthClassification(t *testing.T) {
	for _, test := range []struct {
		name  string
		state *containertypes.State
		want  string
	}{
		{name: "missing", want: "state is unavailable"},
		{name: "exited", state: &containertypes.State{ExitCode: 7, Error: "crashed"}, want: "container exited with code 7"},
		{name: "restarting", state: &containertypes.State{Running: true, Restarting: true, Status: "restarting"}, want: "unstable state"},
		{name: "paused", state: &containertypes.State{Running: true, Paused: true, Status: "paused"}, want: "unstable state"},
		{name: "dead", state: &containertypes.State{Running: true, Dead: true, Status: "dead"}, want: "unstable state"},
		{name: "oom", state: &containertypes.State{Running: true, OOMKilled: true, Status: "dead"}, want: "unstable state"},
		{name: "healthy running", state: &containertypes.State{Running: true, Status: "running"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateRunningState(test.state)
			if test.want == "" && err != nil {
				t.Fatalf("validateRunningState() error = %v", err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("validateRunningState() error = %v, want %q", err, test.want)
			}
		})
	}

	for _, test := range []struct {
		name   string
		health *containertypes.HealthConfig
		state  *containertypes.State
		ready  bool
		err    string
	}{
		{name: "no health state", health: &containertypes.HealthConfig{Test: []string{"CMD-SHELL", "true"}}, state: &containertypes.State{}},
		{name: "starting", health: &containertypes.HealthConfig{Test: []string{"CMD-SHELL", "true"}}, state: &containertypes.State{Health: &containertypes.Health{Status: "starting"}}},
		{name: "healthy", health: &containertypes.HealthConfig{Test: []string{"CMD-SHELL", "true"}}, state: &containertypes.State{Health: &containertypes.Health{Status: "healthy"}}, ready: true},
		{name: "unhealthy", health: &containertypes.HealthConfig{Test: []string{"CMD-SHELL", "true"}}, state: &containertypes.State{Health: &containertypes.Health{Status: "unhealthy"}}, err: "reported unhealthy"},
		{name: "unknown", health: &containertypes.HealthConfig{Test: []string{"CMD-SHELL", "true"}}, state: &containertypes.State{Health: &containertypes.Health{Status: "unknown"}}},
	} {
		t.Run("health "+test.name, func(t *testing.T) {
			ready, err := healthReady(test.state)
			if ready != test.ready || (test.err == "" && err != nil) || (test.err != "" && (err == nil || !strings.Contains(err.Error(), test.err))) {
				t.Fatalf("healthReady() = %v, %v; want %v and %q", ready, err, test.ready, test.err)
			}
		})
	}
	if healthcheckEnabled(nil) || healthcheckEnabled(&containertypes.HealthConfig{}) || healthcheckEnabled(&containertypes.HealthConfig{Test: []string{"NONE"}}) {
		t.Fatal("healthcheckEnabled() accepted disabled health checks")
	}
	if !healthcheckEnabled(&containertypes.HealthConfig{Test: []string{"CMD", "true"}}) {
		t.Fatal("healthcheckEnabled() rejected active health check")
	}
}

func TestReadinessTrackerResetsOnlyWhenProcessChanges(t *testing.T) {
	tracker := readinessTracker{}
	first := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	state := &containertypes.State{Pid: 10, StartedAt: "first"}
	tracker.observeProcess(state, first)
	if !tracker.stableSince.Equal(first) {
		t.Fatalf("first stableSince = %v, want %v", tracker.stableSince, first)
	}
	tracker.observeProcess(state, first.Add(time.Minute))
	if !tracker.stableSince.Equal(first) {
		t.Fatalf("unchanged process reset stableSince to %v", tracker.stableSince)
	}
	state.StartedAt = "second"
	tracker.observeProcess(state, first.Add(2*time.Minute))
	if !tracker.stableSince.Equal(first.Add(2 * time.Minute)) {
		t.Fatalf("changed process stableSince = %v", tracker.stableSince)
	}
}

func TestInspectReadinessReportsInspectAndStabilizationResults(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/containers/new/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspectedContainer("new", "app", "sha256:new", true))
	})
	client := testDockerClient(t, transport)
	tracker := &readinessTracker{stabilization: time.Hour}
	ready, err := client.inspectReadiness(context.Background(), "new", tracker)
	if err != nil || ready {
		t.Fatalf("inspectReadiness() = %v, %v; want not ready", ready, err)
	}
	tracker.stableSince = time.Now().Add(-time.Hour)
	ready, err = client.inspectReadiness(context.Background(), "new", tracker)
	if err != nil || !ready {
		t.Fatalf("stabilized inspectReadiness() = %v, %v; want ready", ready, err)
	}

	transport.register("GET", "/v1.41/containers/missing/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNotFound, map[string]string{"message": "missing"})
	})
	if _, err := client.inspectReadiness(context.Background(), "missing", tracker); err == nil || !strings.Contains(err.Error(), "inspect container") {
		t.Fatalf("inspectReadiness() missing error = %v", err)
	}
}

func TestWaitUntilReadyTimesOutAndPropagatesHealthFailure(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/containers/new/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspectedContainer("new", "app", "sha256:new", true))
	})
	client := testDockerClient(t, transport)
	err := client.waitUntilReady(context.Background(), "new", nil, ReplaceOptions{
		StartupTimeout:    5 * time.Millisecond,
		PollInterval:      time.Millisecond,
		StabilizationTime: time.Hour,
	})
	if err == nil || !strings.Contains(err.Error(), "startup timeout") {
		t.Fatalf("waitUntilReady() error = %v, want startup timeout", err)
	}

	transport.register("GET", "/v1.41/containers/unhealthy/json", func(*http.Request) (*http.Response, error) {
		response := inspectedContainer("unhealthy", "app", "sha256:new", true)
		response.Config.Healthcheck = &containertypes.HealthConfig{Test: []string{"CMD", "true"}}
		response.State.Health = &containertypes.Health{Status: "unhealthy"}
		return jsonResponse(http.StatusOK, response)
	})
	err = client.waitUntilReady(context.Background(), "unhealthy", &containertypes.HealthConfig{Test: []string{"CMD", "true"}}, ReplaceOptions{
		StartupTimeout: time.Second,
		PollInterval:   time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "unhealthy") {
		t.Fatalf("waitUntilReady() unhealthy error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = client.waitUntilReady(ctx, "new", nil, ReplaceOptions{StartupTimeout: time.Second, PollInterval: time.Millisecond, StabilizationTime: time.Hour})
	if err == nil || !strings.Contains(err.Error(), "startup timeout") {
		t.Fatalf("waitUntilReady() canceled error = %v", err)
	}
}
