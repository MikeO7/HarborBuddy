package docker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	imagetypes "github.com/moby/moby/api/types/image"
)

func TestNewClientPingsConfiguredHostAndCloseIsNilSafe(t *testing.T) {
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := <-requests; got == "" {
		t.Fatal("Docker ping path was empty")
	}
	if err := (*DockerClient)(nil).Close(); err != nil {
		t.Fatalf("nil DockerClient.Close() error = %v", err)
	}
}

func TestNewClientReportsInvalidHostConfiguration(t *testing.T) {
	if _, err := NewClient(context.Background(), "not-a-docker-host"); err == nil || !strings.Contains(err.Error(), "create Docker client") {
		t.Fatalf("NewClient(invalid host) error = %v", err)
	}
}

func TestNewClientReportsPingFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "daemon unavailable", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	_, err := NewClient(context.Background(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "ping Docker daemon") {
		t.Fatalf("NewClient() error = %v, want ping failure", err)
	}
}

func TestContainerAdapterReportsErrorsAndNormalizesIncompleteInspect(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "list failed"})
	})
	transport.register("GET", "/v1.41/containers/bare/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, containertypes.InspectResponse{
			ID: "bare", Name: "/bare", Image: "sha256:image", Created: "not-a-time",
		})
	})
	client := testDockerClient(t, transport)
	if _, err := client.ListContainers(context.Background()); err == nil || !strings.Contains(err.Error(), "list running containers") {
		t.Fatalf("ListContainers() error = %v", err)
	}
	details, err := client.InspectContainer(context.Background(), "bare")
	if err != nil {
		t.Fatalf("InspectContainer() error = %v", err)
	}
	if details.Summary.Name != "bare" || !details.Summary.CreatedAt.IsZero() || details.Config != nil || details.Host != nil || details.State != nil || details.Networks != nil {
		t.Fatalf("incomplete inspect was not normalized: %+v", details)
	}
	transport.register("GET", "/v1.41/containers/missing/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNotFound, map[string]string{"message": "missing"})
	})
	if _, err := client.InspectContainer(context.Background(), "missing"); err == nil || !strings.Contains(err.Error(), "inspect container missing") {
		t.Fatalf("InspectContainer() missing error = %v", err)
	}
}

func TestListContainersRecoversConfiguredReferenceAfterTagMoves(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []containertypes.Summary{{
			ID: "stale", Names: []string{"/app"}, Image: "sha256:old", ImageID: "sha256:old",
		}})
	})
	transport.register("GET", "/v1.41/containers/stale/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspectedContainer("stale", "app", "sha256:old", true))
	})
	client := testDockerClient(t, transport)

	containers, err := client.ListContainers(context.Background())
	if err != nil || len(containers) != 1 {
		t.Fatalf("ListContainers() = %+v, %v", containers, err)
	}
	if containers[0].ImageRef != "app:latest" {
		t.Fatalf("recovered image reference = %q, want app:latest", containers[0].ImageRef)
	}
}

func TestListContainersKeepsImageIDWhenReferenceRecoveryFails(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []containertypes.Summary{{
			ID: "gone", Image: "sha256:old", ImageID: "sha256:old",
		}})
	})
	transport.register("GET", "/v1.41/containers/gone/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNotFound, map[string]string{"message": "gone"})
	})
	client := testDockerClient(t, transport)

	containers, err := client.ListContainers(context.Background())
	if err != nil || len(containers) != 1 {
		t.Fatalf("ListContainers() = %+v, %v", containers, err)
	}
	if containers[0].ImageRef != "sha256:old" {
		t.Fatalf("fallback image reference = %q, want sha256:old", containers[0].ImageRef)
	}
}

func TestImageAdapterOperationsAndSummaries(t *testing.T) {
	transport := newMockTransport()
	transport.register("POST", "/v1.41/images/create", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]string{"status": "pulled"})
	})
	transport.register("GET", "/v1.41/images/app:latest/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{
			"Id": "sha256:image", "RepoTags": []string{"app:latest"}, "Created": "2026-07-30T12:00:00Z", "Size": int64(123),
			"Config": map[string]any{"User": "1000", "Env": []string{"A=B"}, "Entrypoint": []string{"/entry"}, "Cmd": []string{"run"}, "WorkingDir": "/work", "Labels": map[string]string{"custom": "value"}, "StopSignal": "SIGTERM"},
		})
	})
	transport.register("GET", "/v1.41/images/sha256:missing/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNotFound, map[string]string{"message": "missing"})
	})
	transport.register("GET", "/v1.41/images/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []imagetypes.Summary{
			{ID: "sha256:tagged", RepoTags: []string{"app:latest"}, Created: 10, Size: 10, Labels: map[string]string{"one": "two"}},
			{ID: "sha256:none", RepoTags: []string{"<none>:<none>"}, Created: 20, Size: 20},
		})
	})
	transport.register("DELETE", "/v1.41/images/sha256:tagged", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []imagetypes.DeleteResponse{{Deleted: "sha256:tagged"}})
	})
	client := testDockerClient(t, transport)
	info, err := client.PullImage(context.Background(), "app:latest")
	if err != nil {
		t.Fatalf("PullImage() error = %v", err)
	}
	if info.ID != "sha256:image" || info.Dangling || info.Size != 123 || info.Config == nil || info.Config.User != "1000" || info.Labels["custom"] != "value" {
		t.Fatalf("pulled image info = %+v", info)
	}
	images, err := client.ListImages(context.Background())
	if err != nil || len(images) != 2 || images[1].Dangling != true {
		t.Fatalf("ListImages() = %+v, %v", images, err)
	}
	dangling, err := client.ListDanglingImages(context.Background())
	if err != nil || len(dangling) != 2 || !dangling[0].Dangling {
		t.Fatalf("ListDanglingImages() = %+v, %v", dangling, err)
	}
	if err := client.RemoveImage(context.Background(), "sha256:tagged"); err != nil {
		t.Fatalf("RemoveImage() error = %v", err)
	}
	if _, err := client.inspectImage(context.Background(), "sha256:missing"); err == nil || !strings.Contains(err.Error(), "inspect image") {
		t.Fatalf("inspectImage() error = %v", err)
	}

	got := imageSummaries([]imagetypes.Summary{{ID: "id", RepoTags: nil, Created: 0}, {ID: "rollback", RepoTags: []string{rollbackImageRepositoryPrefix + "abc:1"}}}, true)
	if len(got) != 2 || !got[0].Dangling || got[0].Labels != nil || !got[1].Protected {
		t.Fatalf("forced dangling summary = %+v", got)
	}
}

func TestImageAdapterReportsPullAndListFailures(t *testing.T) {
	transport := newMockTransport()
	transport.register("POST", "/v1.41/images/create", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "pull failed"})
	})
	transport.register("GET", "/v1.41/images/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "list failed"})
	})
	transport.register("DELETE", "/v1.41/images/image", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "remove failed"})
	})
	transport.register("GET", "/v1.41/images/create/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNotFound, map[string]string{"message": "missing"})
	})
	client := testDockerClient(t, transport)
	if _, err := client.PullImage(context.Background(), "bad:image"); err == nil || !strings.Contains(err.Error(), "pull image bad:image") {
		t.Fatalf("PullImage() error = %v", err)
	}
	if _, err := client.ListImages(context.Background()); err == nil || !strings.Contains(err.Error(), "list images") {
		t.Fatalf("ListImages() error = %v", err)
	}
	transport.register("GET", "/v1.41/images/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "dangling failed"})
	})
	if _, err := client.ListDanglingImages(context.Background()); err == nil || !strings.Contains(err.Error(), "list dangling images") {
		t.Fatalf("ListDanglingImages() error = %v", err)
	}
	if err := client.RemoveImage(context.Background(), "image"); err == nil || !strings.Contains(err.Error(), "remove image") {
		t.Fatalf("RemoveImage() error = %v", err)
	}
}

func TestPullImageReportsReadAndCloseErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		body io.ReadCloser
		want string
	}{
		{name: "read", body: &errorReadCloser{readErr: errors.New("read failed")}, want: "read pull response"},
		{name: "close", body: &errorReadCloser{closeErr: errors.New("close failed")}, want: "close pull response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := newMockTransport()
			transport.register("POST", "/v1.41/images/create", func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: test.body, Header: make(http.Header)}, nil
			})
			client := testDockerClient(t, transport)
			if _, err := client.PullImage(context.Background(), "app:latest"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("PullImage() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestImageSummaryAndEndpointCopiesAreIndependent(t *testing.T) {
	labels := map[string]string{"key": "value"}
	summaries := imageSummaries([]imagetypes.Summary{{ID: "id", RepoTags: []string{"tag"}, Labels: labels}}, false)
	labels["key"] = "changed"
	if summaries[0].Labels["key"] != "value" {
		t.Fatal("image summary labels alias source map")
	}
	if cloneStringMap(nil) != nil {
		t.Fatal("cloneStringMap(nil) must return nil")
	}
}

type errorReadCloser struct {
	readErr  error
	closeErr error
}

func (r *errorReadCloser) Read([]byte) (int, error) {
	if r.readErr != nil {
		return 0, r.readErr
	}
	return 0, io.EOF
}

func (r *errorReadCloser) Close() error { return r.closeErr }
