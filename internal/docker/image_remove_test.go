package docker

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	imagetypes "github.com/moby/moby/api/types/image"
)

const multiTagImage = "sha256:multi"

func conflictResponse() (*http.Response, error) {
	return jsonResponse(http.StatusConflict, map[string]string{
		"message": "conflict: unable to delete multi (must be forced) - image is referenced in multiple repositories",
	})
}

func deletedResponse(id string) (*http.Response, error) {
	return jsonResponse(http.StatusOK, []imagetypes.DeleteResponse{{Untagged: id}})
}

func registerTagInspect(transport *mockTransport, tag string, status int, id string) {
	transport.register("GET", "/v1.41/images/"+tag+"/json", func(*http.Request) (*http.Response, error) {
		if status != http.StatusOK {
			return jsonResponse(status, map[string]string{"message": "tag inspect " + tag})
		}
		return jsonResponse(http.StatusOK, map[string]any{"Id": id, "RepoTags": []string{tag}})
	})
}

// multiTagTransport serves an image tagged app:one, app:two, other:moved and
// other:gone that conflicts on the first delete by ID and succeeds after.
func multiTagTransport(t *testing.T, retryStatus int) (*mockTransport, *atomic.Int32) {
	t.Helper()
	transport := newMockTransport()
	deletes := &atomic.Int32{}
	transport.register("DELETE", "/v1.41/images/"+multiTagImage, func(*http.Request) (*http.Response, error) {
		if deletes.Add(1) == 1 {
			return conflictResponse()
		}
		if retryStatus != http.StatusOK {
			return jsonResponse(retryStatus, map[string]string{"message": "retry failed"})
		}
		return deletedResponse(multiTagImage)
	})
	transport.register("GET", "/v1.41/images/"+multiTagImage+"/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{
			"Id": multiTagImage, "RepoTags": []string{"app:one", "app:two", "other:moved", "other:gone"},
		})
	})
	transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []containertypes.Summary{{ImageID: "sha256:unrelated"}})
	})
	registerTagInspect(transport, "app:two", http.StatusOK, multiTagImage)
	registerTagInspect(transport, "other:moved", http.StatusOK, "sha256:newer")
	registerTagInspect(transport, "other:gone", http.StatusNotFound, "")
	transport.register("DELETE", "/v1.41/images/app:two", func(*http.Request) (*http.Response, error) {
		return deletedResponse("app:two")
	})
	return transport, deletes
}

func TestRemoveImageUntagsExtraReferencesOfUnusedImage(t *testing.T) {
	transport, deletes := multiTagTransport(t, http.StatusOK)
	client := testDockerClient(t, transport)
	if err := client.RemoveImage(context.Background(), multiTagImage); err != nil {
		t.Fatalf("RemoveImage() error = %v", err)
	}
	calls := transport.getCalls()
	if deletes.Load() != 2 || !slices.Contains(calls, "DELETE /v1.41/images/app:two") {
		t.Fatalf("deletes = %d, calls = %v", deletes.Load(), calls)
	}
	for _, untouched := range []string{"DELETE /v1.41/images/app:one", "DELETE /v1.41/images/other:moved", "DELETE /v1.41/images/other:gone"} {
		if slices.Contains(calls, untouched) {
			t.Fatalf("unexpected %s in %v", untouched, calls)
		}
	}
}

func TestRemoveImageReportsConflictsItCannotResolve(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*mockTransport)
		want  []string
	}{
		{name: "single tag", setup: func(transport *mockTransport) {
			transport.register("GET", "/v1.41/images/"+multiTagImage+"/json", func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, map[string]any{"Id": multiTagImage, "RepoTags": []string{"app:one"}})
			})
		}, want: []string{"remove image sha256:multi", "must be forced"}},
		{name: "in use", setup: func(transport *mockTransport) {
			transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, []containertypes.Summary{{ImageID: multiTagImage, State: "exited"}})
			})
		}, want: []string{"must be forced"}},
		{name: "inspect", setup: func(transport *mockTransport) {
			registerServerError(transport, "GET", "/v1.41/images/"+multiTagImage+"/json", "inspect failed")
		}, want: []string{"must be forced", "inspect image sha256:multi"}},
		{name: "container list", setup: func(transport *mockTransport) {
			registerServerError(transport, "GET", "/v1.41/containers/json", "list failed")
		}, want: []string{"must be forced", "list all containers"}},
		{name: "tag inspect", setup: func(transport *mockTransport) {
			registerTagInspect(transport, "other:moved", http.StatusInternalServerError, "")
		}, want: []string{"must be forced", "inspect image tag other:moved"}},
		{name: "untag", setup: func(transport *mockTransport) {
			registerServerError(transport, "DELETE", "/v1.41/images/app:two", "untag failed")
		}, want: []string{"must be forced", "remove image tag app:two"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, deletes := multiTagTransport(t, http.StatusOK)
			test.setup(transport)
			err := testDockerClient(t, transport).RemoveImage(context.Background(), multiTagImage)
			if err == nil || deletes.Load() != 1 {
				t.Fatalf("RemoveImage() error = %v, deletes = %d", err, deletes.Load())
			}
			for _, want := range test.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("RemoveImage() error = %v; want %q", err, want)
				}
			}
		})
	}
}

func TestRemoveImageReportsRetryFailure(t *testing.T) {
	transport, deletes := multiTagTransport(t, http.StatusConflict)
	err := testDockerClient(t, transport).RemoveImage(context.Background(), multiTagImage)
	if err == nil || !strings.Contains(err.Error(), "after removing extra tags") || deletes.Load() != 2 {
		t.Fatalf("RemoveImage() error = %v, deletes = %d", err, deletes.Load())
	}
}

func TestUntagIfCurrentIgnoresTagRemovedConcurrently(t *testing.T) {
	transport := newMockTransport()
	registerTagInspect(transport, "app:two", http.StatusOK, multiTagImage)
	transport.register("DELETE", "/v1.41/images/app:two", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNotFound, map[string]string{"message": "No such image: app:two"})
	})
	if err := testDockerClient(t, transport).untagIfCurrent(context.Background(), "app:two", multiTagImage); err != nil {
		t.Fatalf("untagIfCurrent() error = %v", err)
	}
}
