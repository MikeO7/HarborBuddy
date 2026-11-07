package docker

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// The backup name records the restart policy, so it must be applied before
// the policy is suppressed or the original is stopped: a crash after either
// step then still leaves a container that recovery can find.
func TestReplaceContainerRenamesBeforeStoppingAndUndoesRenameOnStopFailure(t *testing.T) {
	var calls []string
	transport := newMockTransport()
	transport.register("POST", "/v1.41/containers/old/rename", func(r *http.Request) (*http.Response, error) {
		calls = append(calls, "rename:"+r.URL.Query().Get("name"))
		return noContent(r)
	})
	transport.register("POST", "/v1.41/containers/old/update", func(*http.Request) (*http.Response, error) {
		calls = append(calls, "suppress")
		return jsonResponse(http.StatusOK, map[string][]string{"Warnings": {}})
	})
	transport.register("POST", "/v1.41/containers/old/stop", func(*http.Request) (*http.Response, error) {
		calls = append(calls, "stop")
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "stop failed"})
	})
	current := replacementFixture()
	current.Host.RestartPolicy.Name = "always"

	result, err := testDockerClient(t, transport).ReplaceContainer(context.Background(), current, ImageInfo{ID: "sha256:new"}, ReplaceOptions{StopTimeout: time.Second, StartupTimeout: time.Second})
	if err == nil || result.FailureStage != "stop_old_container" || !result.RollbackAttempted {
		t.Fatalf("ReplaceContainer() = %+v, %v; want stop failure with rollback", result, err)
	}
	if len(calls) < 4 || !strings.HasPrefix(calls[0], "rename:app-harborbuddy-backup-") || !strings.HasSuffix(calls[0], ".always") || calls[1] != "suppress" || calls[2] != "stop" {
		t.Fatalf("calls = %v; want backup rename, suppress, stop first", calls)
	}
	if !slices.Contains(calls[3:], "rename:app") {
		t.Fatalf("calls = %v; want the original name restored after the stop failed", calls)
	}
}
