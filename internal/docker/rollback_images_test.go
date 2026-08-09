package docker

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
)

func TestRollbackImageRepositoryUsesFullReferenceWithoutCollisions(t *testing.T) {
	first := rollbackImageRepository("registry.example/team/app:stable")
	if first != rollbackImageRepository("registry.example/team/app:stable") {
		t.Fatal("rollback repository is not deterministic")
	}
	for _, other := range []string{
		"registry.example/team/app:latest",
		"registry.example/other/app:stable",
		"other.example/team/app:stable",
	} {
		if first == rollbackImageRepository(other) {
			t.Fatalf("rollback repository collision for %q", other)
		}
	}
	if len(first) != len(rollbackImageRepositoryPrefix)+64 {
		t.Fatalf("rollback repository = %q, want fixed safe SHA-256 suffix", first)
	}
}

func TestRetainRollbackImageReplacesOldestSlotAndCleansSupersededImage(t *testing.T) {
	transport := newMockTransport()
	repository := rollbackImageRepository("registry.example/team/app:stable")
	slot := repository + ":1"
	transport.register("GET", "/v1.41/images/"+slot+"/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{"Id": "sha256:previous", "RepoTags": []string{slot}})
	})
	transport.register("GET", "/v1.41/containers/json", func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("all") != "1" {
			t.Fatalf("container safety query all = %q, want 1", r.URL.Query().Get("all"))
		}
		return jsonResponse(http.StatusOK, []containertypes.Summary{})
	})
	transport.register("DELETE", "/v1.41/images/"+slot, func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []map[string]string{{"Untagged": slot}, {"Deleted": "sha256:previous"}})
	})
	transport.register("POST", "/v1.41/images/sha256:old/tag", func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("repo") != repository || r.URL.Query().Get("tag") != "1" {
			t.Fatalf("tag query = %q, want rollback repository slot 1", r.URL.RawQuery)
		}
		return jsonResponse(http.StatusCreated, nil)
	})
	client := testDockerClient(t, transport)

	if err := client.retainRollbackImage(context.Background(), "registry.example/team/app:stable", "sha256:old", 1); err != nil {
		t.Fatalf("retainRollbackImage() error = %v", err)
	}
}

func TestRetainRollbackImageDisabledMakesNoDockerCalls(t *testing.T) {
	transport := newMockTransport()
	client := testDockerClient(t, transport)
	if err := client.retainRollbackImage(context.Background(), "app:latest", "sha256:old", 0); err != nil {
		t.Fatalf("retainRollbackImage() error = %v", err)
	}
	if calls := transport.getCalls(); len(calls) != 0 {
		t.Fatalf("disabled retention made Docker calls: %v", calls)
	}
}

func TestRetainRollbackImagePreservesOverflowUsedByStoppedContainer(t *testing.T) {
	transport := newMockTransport()
	repository := rollbackImageRepository("registry.example/team/app:stable")
	slot := rollbackSlot(repository, 1)
	transport.register("GET", "/v1.41/images/"+slot+"/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{"Id": "sha256:protected", "RepoTags": []string{slot}})
	})
	transport.register("GET", "/v1.41/containers/json", func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("all") != "1" {
			t.Fatalf("container safety query all = %q, want 1", r.URL.Query().Get("all"))
		}
		return jsonResponse(http.StatusOK, []containertypes.Summary{{ID: "stopped", ImageID: "sha256:protected", State: "exited"}})
	})
	transport.register("POST", "/v1.41/images/sha256:old/tag", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusCreated, nil)
	})
	client := testDockerClient(t, transport)

	err := client.retainRollbackImage(context.Background(), "registry.example/team/app:stable", "sha256:old", 1)
	if err == nil || !errors.Is(err, ErrRollbackImageInUse) {
		t.Fatalf("retainRollbackImage() error = %v, want in-use warning", err)
	}
	if slices.Contains(transport.getCalls(), "DELETE /v1.41/images/"+slot) {
		t.Fatalf("used image was removed: %v", transport.getCalls())
	}
}

func TestRetainRollbackImageRotatesMultipleSlotsAndBoundsBurst(t *testing.T) {
	transport := newMockTransport()
	repository := rollbackImageRepository("registry.example/team/app:stable")
	for slot, id := range map[int]string{1: "sha256:one", 2: "sha256:two"} {
		name := rollbackSlot(repository, slot)
		transport.register("GET", "/v1.41/images/"+name+"/json", func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, map[string]any{"Id": id, "RepoTags": []string{name}})
		})
	}
	transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []containertypes.Summary{})
	})
	transport.register("DELETE", "/v1.41/images/"+rollbackSlot(repository, 2), func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []map[string]string{})
	})
	for source := range map[string]bool{"sha256:one": true, "sha256:new": true} {
		transport.register("POST", "/v1.41/images/"+source+"/tag", func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusCreated, nil)
		})
	}
	client := testDockerClient(t, transport)
	if err := client.retainRollbackImage(context.Background(), "registry.example/team/app:stable", "sha256:new", 2); err != nil {
		t.Fatalf("retainRollbackImage() error = %v", err)
	}
	calls := transport.getCalls()
	if !slices.Contains(calls, "DELETE /v1.41/images/"+rollbackSlot(repository, 2)) ||
		!slices.Contains(calls, "POST /v1.41/images/sha256:one/tag") ||
		!slices.Contains(calls, "POST /v1.41/images/sha256:new/tag") {
		t.Fatalf("rotation calls = %v", calls)
	}
}

func TestRollbackSlotImageHandlesMissingAndInspectErrors(t *testing.T) {
	repository := rollbackImageRepository("app:latest")
	t.Run("missing", func(t *testing.T) {
		client := testDockerClient(t, newMockTransport())
		image, ok, err := client.rollbackSlotImage(context.Background(), repository, 1)
		if err != nil || ok || image != "" {
			t.Fatalf("rollbackSlotImage() = %q, %v, %v; want missing", image, ok, err)
		}
	})
	t.Run("inspect error", func(t *testing.T) {
		transport := newMockTransport()
		registerServerError(transport, "GET", "/v1.41/images/"+rollbackSlot(repository, 1)+"/json", "inspect failed")
		client := testDockerClient(t, transport)
		_, _, err := client.rollbackSlotImage(context.Background(), repository, 1)
		if err == nil || !strings.Contains(err.Error(), "inspect rollback slot 1") {
			t.Fatalf("rollbackSlotImage() error = %v", err)
		}
	})
}

func TestImageUsedByAnyContainerCoversStatesReferencesAndErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		containers []containertypes.Summary
		want       bool
		wantErr    string
	}{
		{name: "running", status: http.StatusOK, containers: []containertypes.Summary{{ImageID: "sha256:wanted", State: "running"}}, want: true},
		{name: "isolated reference", status: http.StatusOK, containers: []containertypes.Summary{{ImageID: "sha256:other", Image: "app:latest"}}},
		{name: "list error", status: http.StatusInternalServerError, wantErr: "list all containers"},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := newMockTransport()
			transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
				if test.status != http.StatusOK {
					return jsonResponse(test.status, map[string]string{"message": "list failed"})
				}
				return jsonResponse(http.StatusOK, test.containers)
			})
			client := testDockerClient(t, transport)
			got, err := client.imageUsedByAnyContainer(context.Background(), "sha256:wanted")
			if got != test.want || (test.wantErr == "" && err != nil) || (test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr))) {
				t.Fatalf("imageUsedByAnyContainer() = %v, %v; want %v, %q", got, err, test.want, test.wantErr)
			}
		})
	}
}

func TestRetainRollbackImageReportsDockerFailures(t *testing.T) {
	imageRef := "registry.example/team/app:stable"
	repository := rollbackImageRepository(imageRef)
	for _, test := range []struct {
		name  string
		setup func(*mockTransport)
		want  string
	}{
		{name: "overflow inspect", setup: func(transport *mockTransport) {
			registerServerError(transport, "GET", "/v1.41/images/"+rollbackSlot(repository, 2)+"/json", "inspect failed")
		}, want: "inspect rollback slot 2"},
		{name: "container list", setup: func(transport *mockTransport) {
			registerImageInspect(transport, rollbackSlot(repository, 2), "sha256:overflow")
			registerServerError(transport, "GET", "/v1.41/containers/json", "list failed")
		}, want: "list all containers"},
		{name: "remove", setup: func(transport *mockTransport) {
			registerImageInspect(transport, rollbackSlot(repository, 2), "sha256:overflow")
			transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, []containertypes.Summary{})
			})
			registerServerError(transport, "DELETE", "/v1.41/images/"+rollbackSlot(repository, 2), "remove failed")
		}, want: "clean rollback slot 2"},
		{name: "rotation inspect", setup: func(transport *mockTransport) {
			registerServerError(transport, "GET", "/v1.41/images/"+rollbackSlot(repository, 1)+"/json", "inspect failed")
		}, want: "inspect rollback slot 1"},
		{name: "rotation tag", setup: func(transport *mockTransport) {
			registerImageInspect(transport, rollbackSlot(repository, 1), "sha256:one")
			registerServerError(transport, "POST", "/v1.41/images/sha256:one/tag", "rotate failed")
		}, want: "rotate rollback slot 1 to 2"},
		{name: "new image tag", setup: func(*mockTransport) {}, want: "tag last-known-working image sha256:new"},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := newMockTransport()
			test.setup(transport)
			client := testDockerClient(t, transport)
			err := client.retainRollbackImage(context.Background(), imageRef, "sha256:new", 2)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("retainRollbackImage() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestRetainRollbackImageWithMissingOverflowTagsFirstSlot(t *testing.T) {
	transport := newMockTransport()
	transport.register("POST", "/v1.41/images/sha256:new/tag", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusCreated, nil)
	})
	client := testDockerClient(t, transport)
	if err := client.retainRollbackImage(context.Background(), "isolated:ref", "sha256:new", 1); err != nil {
		t.Fatalf("retainRollbackImage() error = %v", err)
	}
}

func registerImageInspect(transport *mockTransport, name, id string) {
	transport.register("GET", "/v1.41/images/"+name+"/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{"Id": id, "RepoTags": []string{name}})
	})
}

func registerServerError(transport *mockTransport, method, path, message string) {
	transport.register(method, path, func(*http.Request) (*http.Response, error) {
		return nil, errors.New(message)
	})
}
