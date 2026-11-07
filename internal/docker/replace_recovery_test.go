package docker

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

func strandedBackup(id, name, mode string) containertypes.InspectResponse {
	inspected := inspectedContainer(id, name, "sha256:old", false)
	inspected.HostConfig = &containertypes.HostConfig{NetworkMode: containertypes.NetworkMode(mode)}
	inspected.Config.Labels = map[string]string{composeServiceLabel: "web"}
	return inspected
}

// withRecoveryClock pins recovery time near the fixture backup timestamps,
// which are small nanosecond counts.
func withRecoveryClock(t *testing.T, now time.Time) {
	t.Helper()
	previous := recoveryClock
	recoveryClock = func() time.Time { return now }
	t.Cleanup(func() { recoveryClock = previous })
}

func registerInspect(transport *mockTransport, inspected containertypes.InspectResponse) {
	transport.register("GET", "/v1.41/containers/"+inspected.ID+"/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspected)
	})
}

func TestRecoverStrandedBackupsRestoresOnlyUnambiguousBackups(t *testing.T) {
	withRecoveryClock(t, time.Unix(0, 0).Add(time.Hour))
	transport := newMockTransport()
	transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []containertypes.Summary{
			{ID: "web-backup", Names: []string{"/consumer/web-harborbuddy-backup-9", "/web-harborbuddy-backup-1.unless-stopped"}, State: containertypes.StateExited},
			{ID: "db", Names: []string{"/db"}, State: containertypes.StateRunning},
			{ID: "db-backup", Names: []string{"/db-harborbuddy-backup-2"}, State: containertypes.StateExited},
			{ID: "run-backup", Names: []string{"/run-harborbuddy-backup-3.always"}, State: containertypes.StateRunning},
			{ID: "busy-backup", Names: []string{"/busy-harborbuddy-backup-4"}, State: containertypes.StateExited},
			{ID: "plain", Names: []string{"/plain"}, State: containertypes.StateExited},
			{ID: "nameless"},
		})
	})
	registerInspect(transport, strandedBackup("web-backup", "web-harborbuddy-backup-1.unless-stopped", "proj_default"))
	var renamed, policy string
	var aliases []string
	transport.register("POST", "/v1.41/containers/web-backup/rename", func(request *http.Request) (*http.Response, error) {
		renamed = request.URL.Query().Get("name")
		return noContent(request)
	})
	transport.register("POST", "/v1.41/networks/proj_default/connect", func(request *http.Request) (*http.Response, error) {
		var body struct{ EndpointConfig *network.EndpointSettings }
		_ = decodeJSON(request, &body)
		if body.EndpointConfig != nil {
			aliases = body.EndpointConfig.Aliases
		}
		return noContent(request)
	})
	transport.register("POST", "/v1.41/containers/web-backup/update", func(request *http.Request) (*http.Response, error) {
		var body containertypes.UpdateConfig
		_ = decodeJSON(request, &body)
		policy = string(body.RestartPolicy.Name)
		return jsonResponse(http.StatusOK, containertypes.UpdateResponse{})
	})
	transport.register("POST", "/v1.41/containers/web-backup/start", noContent)
	// A running backup was renamed just before a crash: it only needs its name
	// and recorded policy back, and must not be reconnected or restarted.
	registerInspect(transport, strandedBackup("run-backup", "run-harborbuddy-backup-3.always", "proj_default"))
	var runRenamed, runPolicy string
	transport.register("POST", "/v1.41/containers/run-backup/rename", func(request *http.Request) (*http.Response, error) {
		runRenamed = request.URL.Query().Get("name")
		return noContent(request)
	})
	transport.register("POST", "/v1.41/containers/run-backup/update", func(request *http.Request) (*http.Response, error) {
		var body containertypes.UpdateConfig
		_ = decodeJSON(request, &body)
		runPolicy = string(body.RestartPolicy.Name)
		return jsonResponse(http.StatusOK, containertypes.UpdateResponse{})
	})
	client := testDockerClient(t, transport)
	defer beginTransaction("busy-backup")()

	recoveries, err := client.RecoverStrandedBackups(context.Background(), false)
	if err != nil || len(recoveries) != 4 {
		t.Fatalf("RecoverStrandedBackups() = %+v, %v", recoveries, err)
	}
	web := recoveries[0]
	if !web.Restored || web.Err != nil || web.OriginalName != "web" || renamed != "web" || policy != "unless-stopped" || !slices.Equal(aliases, []string{"web"}) {
		t.Fatalf("web recovery = %+v renamed=%q policy=%q aliases=%v", web, renamed, policy, aliases)
	}
	if web.Warning == nil || !strings.Contains(web.Warning.Error(), "primary network proj_default") {
		t.Fatalf("web recovery warning = %v", web.Warning)
	}
	if run := recoveries[2]; !run.Restored || run.Err != nil || run.Warning != nil || runRenamed != "run" || runPolicy != "always" {
		t.Fatalf("running backup recovery = %+v renamed=%q policy=%q", run, runRenamed, runPolicy)
	}
	for index, want := range map[int]string{1: "original name db is in use", 3: "in progress"} {
		if recoveries[index].Restored || !strings.Contains(recoveries[index].Reason, want) {
			t.Fatalf("recovery %d = %+v, want reason %q", index, recoveries[index], want)
		}
	}
}

func TestRecoverStrandedBackupsLeavesStaleBackups(t *testing.T) {
	created := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	withRecoveryClock(t, created.Add(staleBackupAge+time.Minute))
	items := []containertypes.Summary{
		{ID: "old", Names: []string{"/web" + backupNameMarker + strconv.FormatInt(created.UnixNano(), 10)}, State: containertypes.StateExited},
	}
	transport := newMockTransport()
	transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) { return jsonResponse(http.StatusOK, items) })
	recoveries, err := testDockerClient(t, transport).RecoverStrandedBackups(context.Background(), false)
	if err != nil || len(recoveries) != 1 || recoveries[0].Restored || !strings.Contains(recoveries[0].Reason, "older than 48 hours") {
		t.Fatalf("stale backup recovery = %+v, %v", recoveries, err)
	}
	if _, ok := backupNameTime("plain-name"); ok {
		t.Fatal("a non-backup name must not parse")
	}
	if _, ok := backupNameTime("web" + backupNameMarker + "99999999999999999999"); ok {
		t.Fatal("an overflowing backup timestamp must not parse")
	}
}

func TestRecoverStrandedBackupsDefersToDryRunHelpersAndShutdown(t *testing.T) {
	withRecoveryClock(t, time.Unix(0, 0).Add(time.Hour))
	items := []containertypes.Summary{{ID: "web-backup", Names: []string{"/web-harborbuddy-backup-1"}, State: containertypes.StateExited}}
	transport := newMockTransport()
	transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) { return jsonResponse(http.StatusOK, items) })
	client := testDockerClient(t, transport)

	if recoveries, err := client.RecoverStrandedBackups(context.Background(), true); err != nil || recoveries[0].Restored || !strings.Contains(recoveries[0].Reason, "dry-run") {
		t.Fatalf("dry-run recovery = %+v, %v", recoveries, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if reason := deferredRecovery(ctx, "web-backup", false); reason != "shutdown requested" {
		t.Fatalf("cancelled recovery reason = %q", reason)
	}
	items = append(items, containertypes.Summary{ID: "helper", Names: []string{"/hb-updater"}, State: containertypes.StateRunning, Labels: map[string]string{SelfUpdateHelperLabel: "true"}})
	if recoveries, err := client.RecoverStrandedBackups(context.Background(), false); err != nil || !recoveries[0].Deferred || !strings.Contains(recoveries[0].Reason, "self-update helper") {
		t.Fatalf("helper recovery = %+v, %v", recoveries, err)
	}
	for _, call := range transport.getCalls() {
		if !strings.HasPrefix(call, "GET ") {
			t.Fatalf("deferred recovery mutated Docker: %v", transport.getCalls())
		}
	}

	transport = newMockTransport()
	registerServerError(transport, "GET", "/v1.41/containers/json", "list failed")
	client = testDockerClient(t, transport)
	if _, err := client.RecoverStrandedBackups(context.Background(), false); err == nil || !strings.Contains(err.Error(), "stranded backup recovery") {
		t.Fatalf("list failure = %v", err)
	}
}

func TestRestoreBackupReportsEachFailure(t *testing.T) {
	policy := &containertypes.RestartPolicy{Name: "always"}
	tests := []struct {
		name     string
		register func(*mockTransport)
		restored bool
		err      string
		warning  string
	}{
		{name: "inspect", register: func(*mockTransport) {}, err: "inspect stranded backup"},
		{name: "incomplete", register: func(m *mockTransport) {
			inspected := strandedBackup("b", "web-harborbuddy-backup-1", "bridge")
			inspected.HostConfig = nil
			registerInspect(m, inspected)
		}, err: "inspection is incomplete"},
		{name: "rename", register: func(m *mockTransport) {
			registerInspect(m, strandedBackup("b", "web-harborbuddy-backup-1", "bridge"))
			registerServerError(m, "POST", "/v1.41/containers/b/rename", "rename failed")
		}, err: "rename stranded backup"},
		{name: "start and warnings", register: func(m *mockTransport) {
			registerInspect(m, strandedBackup("b", "web-harborbuddy-backup-1", "default"))
			m.register("POST", "/v1.41/containers/b/rename", noContent)
			registerServerError(m, "POST", "/v1.41/networks/bridge/connect", "connect failed")
			registerServerError(m, "POST", "/v1.41/containers/b/update", "update failed")
			registerServerError(m, "POST", "/v1.41/containers/b/start", "start failed")
		}, restored: true, err: "restart original container", warning: "restore original restart policy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newMockTransport()
			test.register(transport)
			client := testDockerClient(t, transport)
			restored, warning, err := client.restoreBackup(context.Background(), "b", "web", policy, false)
			if restored != test.restored || err == nil || !strings.Contains(err.Error(), test.err) {
				t.Fatalf("restoreBackup() = %v, %v, %v; want restored=%v err %q", restored, warning, err, test.restored, test.err)
			}
			if test.warning != "" && (warning == nil || !strings.Contains(warning.Error(), test.warning) || !strings.Contains(warning.Error(), "network bridge")) {
				t.Fatalf("restoreBackup() warning = %v, want %q", warning, test.warning)
			}
		})
	}
}

func TestReconnectPrimaryNetworkSkipsAttachedAndSpecialModes(t *testing.T) {
	client := testDockerClient(t, newMockTransport())
	attached := replacementFixture()
	attached.Networks = map[string]*network.EndpointSettings{"net": {}}
	if err := client.reconnectPrimaryNetwork(context.Background(), attached); err != nil {
		t.Fatalf("attached container reconnect = %v", err)
	}
	for _, mode := range []string{"", "none", "host", "container:other"} {
		current := replacementFixture()
		current.Host.NetworkMode = containertypes.NetworkMode(mode)
		if err := client.reconnectPrimaryNetwork(context.Background(), current); err != nil {
			t.Fatalf("reconnect in mode %q = %v", mode, err)
		}
	}
	transport := newMockTransport()
	transport.register("POST", "/v1.41/networks/bridge/connect", func(request *http.Request) (*http.Response, error) {
		var body struct{ EndpointConfig *network.EndpointSettings }
		_ = decodeJSON(request, &body)
		if body.EndpointConfig != nil && len(body.EndpointConfig.Aliases) != 0 {
			t.Errorf("default bridge must not receive aliases: %v", body.EndpointConfig.Aliases)
		}
		return noContent(request)
	})
	client = testDockerClient(t, transport)
	current := replacementFixture()
	current.Host.NetworkMode = "bridge"
	current.Config.Labels = map[string]string{composeServiceLabel: "web"}
	if err := client.reconnectPrimaryNetwork(context.Background(), current); err == nil || !strings.Contains(err.Error(), "primary network bridge") {
		t.Fatalf("bridge reconnect = %v", err)
	}
}
