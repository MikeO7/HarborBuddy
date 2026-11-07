package health

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type pingFunc func(context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }

func okPinger() Pinger { return pingFunc(func(context.Context) error { return nil }) }

func readStatus(t *testing.T, path string) Status {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // Test reads a file it created in t.TempDir.
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	return status
}

func TestPathFromEnv(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		set   bool
		want  string
	}{
		{name: "unset", want: DefaultPath},
		{name: "empty", set: true, want: DefaultPath},
		{name: "custom", value: "/run/hb.json", set: true, want: "/run/hb.json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := PathFromEnv(func(name string) (string, bool) {
				if name != PathEnv {
					t.Fatalf("looked up %q", name)
				}
				return test.value, test.set
			})
			if got != test.want {
				t.Fatalf("PathFromEnv() = %q; want %q", got, test.want)
			}
		})
	}
}

func TestBeatWritesHeartbeatAndRunningCycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "health.json")
	monitor := NewMonitor(path, okPinger())
	now := time.Date(2026, 10, 30, 4, 0, 0, 0, time.UTC)
	monitor.now = func() time.Time { return now }

	if err := monitor.Beat(context.Background()); err != nil {
		t.Fatalf("Beat() error = %v", err)
	}
	if status := readStatus(t, path); !status.Heartbeat.Equal(now) || !status.CycleStarted.IsZero() {
		t.Fatalf("idle status = %+v", status)
	}

	done := TrackCycle(WithMonitor(context.Background(), monitor))
	if err := monitor.Beat(context.Background()); err != nil {
		t.Fatalf("Beat() error = %v", err)
	}
	if status := readStatus(t, path); !status.CycleStarted.Equal(now) {
		t.Fatalf("cycle status = %+v", status)
	}
	done()
	if err := monitor.Beat(context.Background()); err != nil {
		t.Fatalf("Beat() error = %v", err)
	}
	if status := readStatus(t, path); !status.CycleStarted.IsZero() {
		t.Fatalf("finished cycle status = %+v", status)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Fatalf("left temporary files: %v", entries)
	}
}

func TestBeatReportsPingAndWriteFailures(t *testing.T) {
	failing := NewMonitor(filepath.Join(t.TempDir(), "health.json"), pingFunc(func(context.Context) error {
		return errors.New("daemon down")
	}))
	if err := failing.Beat(context.Background()); err == nil || err.Error() != "daemon down" {
		t.Fatalf("Beat() error = %v", err)
	}

	missingDir := NewMonitor(filepath.Join(t.TempDir(), "missing", "health.json"), okPinger())
	if err := missingDir.Beat(context.Background()); err == nil || !strings.Contains(err.Error(), "write health file") {
		t.Fatalf("Beat() error = %v", err)
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "health.json")
	if err := os.MkdirAll(filepath.Join(target, "occupied"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := NewMonitor(target, okPinger()).Beat(context.Background()); err == nil || !strings.Contains(err.Error(), "replace health file") {
		t.Fatalf("Beat() error = %v", err)
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file left behind: %v", err)
	}
}

func TestTrackCycleWithoutMonitorIsNoop(t *testing.T) {
	TrackCycle(context.Background())()
	var monitor *Monitor
	monitor.CycleStarted()()
}

func TestRunReportsOnlyStateChangesAndStops(t *testing.T) {
	path := filepath.Join(t.TempDir(), "health.json")
	results := []error{errors.New("down"), errors.New("still down"), nil, nil, errors.New("down again")}
	var mu sync.Mutex
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	monitor := NewMonitor(path, pingFunc(func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if calls == len(results) {
			cancel()
			return context.Canceled
		}
		calls++
		return results[calls-1]
	}))

	var reports []string
	monitor.Run(ctx, time.Millisecond, func(err error) {
		if err == nil {
			reports = append(reports, "recovered")
			return
		}
		reports = append(reports, err.Error())
	})
	want := []string{"down", "recovered", "down again"}
	if strings.Join(reports, "|") != strings.Join(want, "|") {
		t.Fatalf("reports = %q; want %q", reports, want)
	}
}

func TestRunStopsWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	beats := make(chan struct{}, 1)
	monitor := NewMonitor(filepath.Join(t.TempDir(), "health.json"), pingFunc(func(context.Context) error {
		beats <- struct{}{}
		return nil
	}))
	done := make(chan struct{})
	go func() {
		defer close(done)
		monitor.Run(ctx, time.Hour, func(error) { t.Error("unexpected report") })
	}()
	<-beats
	cancel()
	<-done
}

func TestCheck(t *testing.T) {
	now := time.Date(2026, 10, 30, 4, 0, 0, 0, time.UTC)
	write := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "health.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	encode := func(status Status) string {
		data, _ := json.Marshal(status)
		return string(data)
	}
	for _, test := range []struct {
		name    string
		path    func(*testing.T) string
		wantErr string
	}{
		{name: "missing", path: func(t *testing.T) string { return filepath.Join(t.TempDir(), "none.json") }, wantErr: "read health file"},
		{name: "malformed", path: func(t *testing.T) string { return write(t, "{") }, wantErr: "parse health file"},
		{name: "no heartbeat", path: func(t *testing.T) string { return write(t, "{}") }, wantErr: "has no heartbeat"},
		{name: "stale", path: func(t *testing.T) string {
			return write(t, encode(Status{Heartbeat: now.Add(-2 * time.Minute)}))
		}, wantErr: "last successful Docker ping was 2m0s ago"},
		{name: "stuck cycle", path: func(t *testing.T) string {
			return write(t, encode(Status{Heartbeat: now, CycleStarted: now.Add(-3 * time.Hour)}))
		}, wantErr: "update cycle has run for 3h0m0s"},
		{name: "running cycle", path: func(t *testing.T) string {
			return write(t, encode(Status{Heartbeat: now.Add(-30 * time.Second), CycleStarted: now.Add(-time.Hour)}))
		}},
		{name: "idle", path: func(t *testing.T) string { return write(t, encode(Status{Heartbeat: now})) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := Check(test.path(t), now)
			if test.wantErr == "" && err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("Check() error = %v; want %q", err, test.wantErr)
			}
		})
	}
}
