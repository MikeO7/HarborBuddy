package docker

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
)

func TestBackupNamesRecordSuppressedRestartPolicy(t *testing.T) {
	now := time.Unix(0, 42)
	tests := []struct {
		suppressed bool
		policy     containertypes.RestartPolicy
		want       string
	}{
		{suppressed: false, policy: containertypes.RestartPolicy{Name: "always"}, want: "app-harborbuddy-backup-42"},
		{suppressed: true, policy: containertypes.RestartPolicy{Name: "unless-stopped"}, want: "app-harborbuddy-backup-42.unless-stopped"},
		{suppressed: true, policy: containertypes.RestartPolicy{Name: "on-failure", MaximumRetryCount: 5}, want: "app-harborbuddy-backup-42.on-failure.5"},
		{suppressed: true, policy: containertypes.RestartPolicy{Name: "on-failure"}, want: "app-harborbuddy-backup-42.on-failure"},
	}
	for _, test := range tests {
		got := backupContainerName("app", now, test.suppressed, test.policy)
		if got != test.want {
			t.Fatalf("backupContainerName() = %q, want %q", got, test.want)
		}
		original, policy, ok := parseBackupName(got)
		if !ok || original != "app" {
			t.Fatalf("parseBackupName(%q) = %q, %v", got, original, ok)
		}
		if test.suppressed != (policy != nil) || (policy != nil && *policy != test.policy) {
			t.Fatalf("parseBackupName(%q) policy = %+v, want %+v", got, policy, test.policy)
		}
	}
	for _, invalid := range []string{"app", "app-harborbuddy-backup-", "app-harborbuddy-backup-x", "app-harborbuddy-backup-1.bogus", "app-harborbuddy-backup-1.on-failure.99999999999999999999999"} {
		if _, _, ok := parseBackupName(invalid); ok {
			t.Fatalf("parseBackupName(%q) accepted an invalid name", invalid)
		}
	}
	if original, _, ok := parseBackupName("my-harborbuddy-backup-1-harborbuddy-backup-2"); !ok || original != "my-harborbuddy-backup-1" {
		t.Fatalf("nested backup name parsed as %q, %v", original, ok)
	}
}

func TestTransactionRegistryAndBudgets(t *testing.T) {
	release := beginTransaction("txn")
	if !transactionActive("txn") {
		t.Fatal("transaction not registered")
	}
	release()
	if transactionActive("txn") {
		t.Fatal("transaction not released")
	}
	options := normalizeReplaceOptions(ReplaceOptions{})
	if got := transactionTimeout(options); got != 2*10*time.Second+30*time.Second+2*time.Second+time.Minute {
		t.Fatalf("transactionTimeout() = %s", got)
	}
	if got := rollbackTimeout(10); got != 40*time.Second {
		t.Fatalf("rollbackTimeout() = %s", got)
	}
}

func TestReplaceContainerDoesNotStartAfterShutdown(t *testing.T) {
	transport := newMockTransport()
	client := testDockerClient(t, transport)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := client.ReplaceContainer(ctx, replacementFixture(), ImageInfo{ID: "sha256:new"}, ReplaceOptions{})
	if err == nil || result.FailureStage != "cancelled_before_start" || result.RollbackAttempted {
		t.Fatalf("ReplaceContainer(cancelled) = %+v, %v", result, err)
	}
	for _, call := range transport.getCalls() {
		if !strings.HasPrefix(call, "GET ") {
			t.Fatalf("cancelled replacement mutated Docker: %v", transport.getCalls())
		}
	}
}

func TestReplaceContainerFinishesTransactionAfterShutdownSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := newMockTransport()
	transport.register("POST", "/v1.41/containers/old/update", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, containertypes.UpdateResponse{})
	})
	transport.register("POST", "/v1.41/containers/old/stop", noContent)
	transport.register("POST", "/v1.41/containers/old/rename", func(request *http.Request) (*http.Response, error) {
		if name := request.URL.Query().Get("name"); !strings.HasSuffix(name, ".always") {
			t.Errorf("backup name %q does not record the suppressed restart policy", name)
		}
		return noContent(request)
	})
	transport.register("POST", "/v1.41/containers/create", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusCreated, containertypes.CreateResponse{ID: "new"})
	})
	transport.register("GET", "/v1.41/containers/new/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspectedContainer("new", "app", "sha256:new", true))
	})
	transport.register("POST", "/v1.41/containers/new/start", func(request *http.Request) (*http.Response, error) {
		if !transactionActive("old") {
			t.Error("transaction was not registered while in flight")
		}
		cancel() // SIGTERM arrives while the replacement is starting.
		return noContent(request)
	})
	transport.register("DELETE", "/v1.41/containers/old", noContent)
	client := testDockerClient(t, transport)
	current := replacementFixture()
	current.Host.RestartPolicy = containertypes.RestartPolicy{Name: "always"}

	result, err := client.ReplaceContainer(ctx, current, ImageInfo{ID: "sha256:new"}, ReplaceOptions{
		StopTimeout: time.Second, StartupTimeout: time.Second, StabilizationTime: 20 * time.Millisecond, PollInterval: time.Millisecond,
	})
	if err != nil || result.NewContainerID != "new" || result.RollbackAttempted {
		t.Fatalf("ReplaceContainer() after shutdown signal = %+v, %v; want completed transaction", result, err)
	}
	if !slices.Contains(transport.getCalls(), "DELETE /v1.41/containers/old") {
		t.Fatalf("transaction did not finish; calls = %v", transport.getCalls())
	}
	if transactionActive("old") {
		t.Fatal("transaction registry not released")
	}
}
