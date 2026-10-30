// Package health lets a container healthcheck confirm that the HarborBuddy
// daemon is alive, can reach Docker, and is not stuck in an update cycle.
//
// The daemon pings Docker on a fixed interval and records each successful
// ping, plus the start of any running cycle, in a small status file. The
// --healthcheck mode reads that file from a second process in the same
// container.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	// DefaultPath is the status file used when PathEnv is unset.
	DefaultPath = "/tmp/harborbuddy-health.json"
	// PathEnv overrides the status file path.
	PathEnv = "HARBORBUDDY_HEALTH_FILE"
	// HeartbeatInterval is how often the daemon pings Docker.
	HeartbeatInterval = 15 * time.Second
	// MaxHeartbeatAge is the oldest successful ping a healthy daemon may report.
	MaxHeartbeatAge = 60 * time.Second
	// MaxCycleDuration is the longest an update cycle may run before the
	// daemon counts as stuck.
	MaxCycleDuration = 2 * time.Hour

	pingTimeout = 10 * time.Second
)

// Status is the content of the status file.
type Status struct {
	Heartbeat    time.Time `json:"heartbeat"`
	CycleStarted time.Time `json:"cycle_started,omitzero"`
}

// Pinger checks that the Docker daemon answers.
type Pinger interface {
	Ping(context.Context) error
}

// Monitor writes the status file for a running daemon.
type Monitor struct {
	path   string
	pinger Pinger
	now    func() time.Time

	mu           sync.Mutex
	cycleStarted time.Time
}

// NewMonitor returns a monitor that writes path after each successful ping.
func NewMonitor(path string, pinger Pinger) *Monitor {
	return &Monitor{path: path, pinger: pinger, now: time.Now}
}

// PathFromEnv returns the status file path from PathEnv or DefaultPath.
func PathFromEnv(lookupEnv func(string) (string, bool)) string {
	if path, ok := lookupEnv(PathEnv); ok && path != "" {
		return path
	}
	return DefaultPath
}

// CycleStarted records the start of a cycle and returns a function that
// records its end. It is safe to call on a nil monitor.
func (m *Monitor) CycleStarted() func() {
	if m == nil {
		return func() {}
	}
	m.mu.Lock()
	m.cycleStarted = m.now()
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		m.cycleStarted = time.Time{}
		m.mu.Unlock()
	}
}

// Beat pings Docker and, when it answers, writes the status file.
func (m *Monitor) Beat(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := m.pinger.Ping(pingCtx); err != nil {
		return err
	}
	m.mu.Lock()
	status := Status{Heartbeat: m.now().UTC(), CycleStarted: m.cycleStarted.UTC()}
	m.mu.Unlock()
	return writeStatus(m.path, status)
}

// Run beats at once and then every interval until ctx ends. It calls report
// only when the heartbeat starts failing (with the error) or recovers (with
// nil), so a long Docker outage logs once.
func (m *Monitor) Run(ctx context.Context, interval time.Duration, report func(error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	failing := false
	for {
		// A ping cut short by shutdown is not a health failure.
		if err := m.Beat(ctx); ctx.Err() == nil && (err != nil) != failing {
			failing = err != nil
			report(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Check reads the status file at path and returns why the daemon is unhealthy,
// or nil when it is healthy at now.
func Check(path string, now time.Time) error {
	data, err := os.ReadFile(path) //nolint:gosec // The path comes from the operator's own environment.
	if err != nil {
		return fmt.Errorf("read health file: %w", err)
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		return fmt.Errorf("parse health file %s: %w", path, err)
	}
	if status.Heartbeat.IsZero() {
		return fmt.Errorf("health file %s has no heartbeat", path)
	}
	if age := now.Sub(status.Heartbeat); age > MaxHeartbeatAge {
		return fmt.Errorf("last successful Docker ping was %s ago; limit is %s", age.Round(time.Second), MaxHeartbeatAge)
	}
	if !status.CycleStarted.IsZero() {
		if running := now.Sub(status.CycleStarted); running > MaxCycleDuration {
			return fmt.Errorf("update cycle has run for %s; limit is %s", running.Round(time.Second), MaxCycleDuration)
		}
	}
	return nil
}

// writeStatus replaces path through a rename so a concurrent Check never
// reads a partial file.
func writeStatus(path string, status Status) error {
	// Status holds only time values, which always encode.
	data, _ := json.Marshal(status)
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil { //nolint:gosec // The status is not secret and the healthcheck may run as another user.
		return fmt.Errorf("write health file: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		return errors.Join(fmt.Errorf("replace health file: %w", err), os.Remove(temp))
	}
	return nil
}

type contextKey struct{}

// WithMonitor returns a context that carries m for TrackCycle.
func WithMonitor(ctx context.Context, m *Monitor) context.Context {
	return context.WithValue(ctx, contextKey{}, m)
}

// TrackCycle records a cycle start on the context's monitor, if any, and
// returns the function that records its end.
func TrackCycle(ctx context.Context) func() {
	m, _ := ctx.Value(contextKey{}).(*Monitor)
	return m.CycleStarted()
}
