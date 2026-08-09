package docker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
)

func TestStartSelfUpdateHelperReportsCreateAndReadinessFailures(t *testing.T) {
	transport := newMockTransport()
	client := testDockerClient(t, transport)
	current := ContainerDetails{Config: &containertypes.Config{}, Host: &containertypes.HostConfig{}}
	request := SelfUpdateHelperRequest{Name: "helper", TargetContainerID: "target", TargetImageID: "image"}
	if _, err := client.StartSelfUpdateHelper(context.Background(), ContainerDetails{}, request); err == nil || !strings.Contains(err.Error(), "inspection is incomplete") {
		t.Fatalf("invalid helper config error = %v", err)
	}
	if _, err := client.StartSelfUpdateHelper(context.Background(), current, request); err == nil || !strings.Contains(err.Error(), "create self-update helper") {
		t.Fatalf("create failure = %v", err)
	}

	transport.register("GET", "/v1.41/containers/helper-id/logs", func(*http.Request) (*http.Response, error) {
		return nil, errors.New("logs unavailable")
	})
	if err := client.waitSelfUpdateHelperReady(context.Background(), "helper-id"); err == nil || !strings.Contains(err.Error(), "open helper logs") {
		t.Fatalf("logs open failure = %v", err)
	}
	transport.register("GET", "/v1.41/containers/helper-id/logs", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: &errorReadCloser{readErr: errors.New("logs read failed")}, Header: make(http.Header)}, nil
	})
	if err := client.waitSelfUpdateHelperReady(context.Background(), "helper-id"); err == nil || !strings.Contains(err.Error(), "read helper readiness logs") {
		t.Fatalf("logs read failure = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	transport.register("GET", "/v1.41/containers/helper-id/logs", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})
	if err := client.waitSelfUpdateHelperReady(ctx, "helper-id"); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("canceled helper readiness = %v", err)
	}

	transport.register("GET", "/v1.41/containers/helper-id/logs", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: &noiseThenMarkerReader{}, Header: make(http.Header)}, nil
	})
	transport.register("GET", "/v1.41/containers/helper-id/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspectedContainer("helper-id", "helper", "image", true))
	})
	if err := client.waitSelfUpdateHelperReady(context.Background(), "helper-id"); err != nil {
		t.Fatalf("noise before readiness marker error = %v", err)
	}

	transport.register("GET", "/v1.41/containers/helper-id/logs", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: &closeErrorReader{Reader: strings.NewReader(SelfUpdateHelperReadyMarker + "\n")}, Header: make(http.Header)}, nil
	})
	transport.register("GET", "/v1.41/containers/helper-id/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspectedContainer("helper-id", "helper", "image", true))
	})
	transport.register("GET", "/v1.41/containers/helper-id/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspectedContainer("helper-id", "helper", "image", true))
	})
	transport.register("GET", "/v1.41/containers/helper-id/logs", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: &markerCloseErrorReader{closeErr: errors.New("logs close failed")}, Header: make(http.Header)}, nil
	})
	if err := client.waitSelfUpdateHelperReady(context.Background(), "helper-id"); err == nil || !strings.Contains(err.Error(), "close helper logs") {
		t.Fatalf("logs close failure = %v", err)
	}
}

func TestStartSelfUpdateHelperCleansUpWhenRestartSuppressionFails(t *testing.T) {
	transport := newMockTransport()
	transport.register("POST", "/v1.41/containers/create", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusCreated, containertypes.CreateResponse{ID: "helper-id"})
	})
	transport.register("POST", "/v1.41/containers/helper-id/start", noContent)
	transport.register("GET", "/v1.41/containers/helper-id/logs", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(SelfUpdateHelperReadyMarker + "\n")), Header: make(http.Header)}, nil
	})
	transport.register("GET", "/v1.41/containers/helper-id/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, inspectedContainer("helper-id", "helper", "image", true))
	})
	transport.register("POST", "/v1.41/containers/target/update", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "update failed"})
	})
	transport.register("DELETE", "/v1.41/containers/helper-id", noContent)
	client := testDockerClient(t, transport)
	_, err := client.StartSelfUpdateHelper(context.Background(), ContainerDetails{
		Summary: ContainerSummary{ID: "target"},
		Config:  &containertypes.Config{},
		Host: &containertypes.HostConfig{
			RestartPolicy: containertypes.RestartPolicy{Name: "unless-stopped"},
		},
	}, SelfUpdateHelperRequest{Name: "helper", TargetContainerID: "target", TargetImageID: "image"})
	if err == nil || !strings.Contains(err.Error(), "prepare target for self-update handoff") {
		t.Fatalf("restart suppression failure = %v", err)
	}
	if !slices.Contains(transport.getCalls(), "DELETE /v1.41/containers/helper-id") {
		t.Fatalf("helper was not cleaned up after handoff failure: %v", transport.getCalls())
	}
}

type closeErrorReader struct {
	*strings.Reader
}

func (*closeErrorReader) Close() error { return errors.New("logs close failed") }

type markerCloseErrorReader struct {
	closeErr error
	read     bool
}

func (r *markerCloseErrorReader) Read(buffer []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	return copy(buffer, []byte(SelfUpdateHelperReadyMarker+"\n")), nil
}

func (r *markerCloseErrorReader) Close() error { return r.closeErr }

type noiseThenMarkerReader struct{ sent bool }

func (r *noiseThenMarkerReader) Read(buffer []byte) (int, error) {
	if r.sent {
		return 0, io.EOF
	}
	r.sent = true
	return copy(buffer, []byte("noise\n"+SelfUpdateHelperReadyMarker+"\n")), nil
}

func (r *noiseThenMarkerReader) Close() error { return nil }

func TestVerifyHelperRunningClassifiesStateAndInspectErrors(t *testing.T) {
	transport := newMockTransport()
	client := testDockerClient(t, transport)
	transport.register("GET", "/v1.41/containers/helper/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "inspect failed"})
	})
	if err := client.verifyHelperRunning(context.Background(), "helper"); err == nil || !strings.Contains(err.Error(), "inspect container") {
		t.Fatalf("inspect helper error = %v", err)
	}
	for _, test := range []struct {
		name  string
		state *containertypes.State
		want  string
	}{
		{name: "nil", want: "not running stably"},
		{name: "stopped", state: &containertypes.State{Running: false}, want: "not running stably"},
		{name: "restarting", state: &containertypes.State{Running: true, Restarting: true}, want: "not running stably"},
		{name: "dead", state: &containertypes.State{Running: true, Dead: true}, want: "not running stably"},
		{name: "stable", state: &containertypes.State{Running: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport.register("GET", "/v1.41/containers/helper/json", func(*http.Request) (*http.Response, error) {
				response := inspectedContainer("helper", "helper", "image", true)
				response.State = test.state
				return jsonResponse(http.StatusOK, response)
			})
			err := client.verifyHelperRunning(context.Background(), "helper")
			if test.want == "" && err != nil {
				t.Fatalf("verifyHelperRunning() error = %v", err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("verifyHelperRunning() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRemoveFailedHelperReportsDockerFailure(t *testing.T) {
	transport := newMockTransport()
	transport.register("DELETE", "/v1.41/containers/helper", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "remove failed"})
	})
	client := testDockerClient(t, transport)
	if err := client.removeFailedHelper(context.Background(), "helper"); err == nil || !strings.Contains(err.Error(), "remove failed helper") {
		t.Fatalf("removeFailedHelper() error = %v", err)
	}
}

func TestWaitContainerExitHandlesResultErrorAndCancellation(t *testing.T) {
	transport := newMockTransport()
	transport.register("POST", "/v1.41/containers/target/wait", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{"StatusCode": 0})
	})
	client := testDockerClient(t, transport)
	if err := client.WaitContainerExit(context.Background(), "target"); err != nil {
		t.Fatalf("WaitContainerExit(success) error = %v", err)
	}

	transport.register("POST", "/v1.41/containers/error/wait", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{"StatusCode": 1, "Error": map[string]string{"Message": "wait result failed"}})
	})
	if err := client.WaitContainerExit(context.Background(), "error"); err == nil || !strings.Contains(err.Error(), "wait result failed") {
		t.Fatalf("WaitContainerExit(result error) = %v", err)
	}
	transport.register("POST", "/v1.41/containers/transport-error/wait", func(*http.Request) (*http.Response, error) {
		return nil, errors.New("wait transport failed")
	})
	if err := client.WaitContainerExit(context.Background(), "transport-error"); err == nil || !strings.Contains(err.Error(), "wait for container transport-error to exit") {
		t.Fatalf("WaitContainerExit(transport error) = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.WaitContainerExit(ctx, "target"); err == nil || !strings.Contains(err.Error(), "wait for container target to exit") {
		t.Fatalf("WaitContainerExit(canceled) = %v", err)
	}
	lateCancel := newCancellationAfterCheckContext()
	if err := waitForContainerExit(lateCancel, "late-cancel", make(chan containertypes.WaitResponse), make(chan error)); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("waitForContainerExit(late cancellation) = %v", err)
	}
	result := make(chan containertypes.WaitResponse, 1)
	errCh := make(chan error, 1)
	errCh <- nil
	if err := waitForContainerExit(context.Background(), "nil-error", result, errCh); err != nil {
		t.Fatalf("waitForContainerExit(nil error) = %v", err)
	}
}

type cancellationAfterCheckContext struct {
	context.Context
	done     chan struct{}
	errCalls int
}

func newCancellationAfterCheckContext() *cancellationAfterCheckContext {
	done := make(chan struct{})
	close(done)
	return &cancellationAfterCheckContext{Context: context.Background(), done: done}
}

func (c *cancellationAfterCheckContext) Done() <-chan struct{} { return c.done }

func (c *cancellationAfterCheckContext) Err() error {
	c.errCalls++
	if c.errCalls == 1 {
		return nil
	}
	return context.Canceled
}
