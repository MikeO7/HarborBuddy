package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/docker"
)

func waitForCalls(t *testing.T, mu *sync.Mutex, calls *int, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := *calls
		mu.Unlock()
		if got >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("calls did not reach %d", want)
}

type fixedClock struct {
	now    time.Time
	timer  timer
	ticker ticker
}

func (c fixedClock) Now() time.Time { return c.now }
func (c fixedClock) NewTimer(time.Duration) timer {
	if c.timer != nil {
		return c.timer
	}
	return &fakeTimer{channel: make(chan time.Time)}
}
func (c fixedClock) NewTicker(time.Duration) ticker {
	if c.ticker != nil {
		return c.ticker
	}
	return &fakeTicker{channel: make(chan time.Time)}
}

type fakeTimer struct{ channel chan time.Time }

func (t *fakeTimer) C() <-chan time.Time { return t.channel }
func (t *fakeTimer) Stop() bool          { return true }

type recordingTimer struct {
	channel chan time.Time
	stopped bool
}

func (t *recordingTimer) C() <-chan time.Time { return t.channel }
func (t *recordingTimer) Stop() bool {
	t.stopped = true
	return true
}

type fakeTicker struct{ channel chan time.Time }

func (t *fakeTicker) C() <-chan time.Time { return t.channel }
func (t *fakeTicker) Stop()               {}

type noopClient struct{}

func (noopClient) ListContainers(context.Context) ([]docker.ContainerSummary, error) { return nil, nil }
func (noopClient) InspectContainer(context.Context, string) (docker.ContainerDetails, error) {
	return docker.ContainerDetails{}, nil
}
func (noopClient) PullImage(context.Context, string) (docker.ImageInfo, error) {
	return docker.ImageInfo{}, nil
}
func (noopClient) CheckReplacement(docker.ContainerDetails, docker.ImageInfo) error { return nil }
func (noopClient) ReplaceContainer(context.Context, docker.ContainerDetails, docker.ImageInfo, docker.ReplaceOptions) (docker.ReplaceResult, error) {
	return docker.ReplaceResult{}, nil
}
func (noopClient) ListImages(context.Context) ([]docker.ImageInfo, error) { return nil, nil }
func (noopClient) ListDanglingImages(context.Context) ([]docker.ImageInfo, error) {
	return nil, nil
}
func (noopClient) RemoveImage(context.Context, string) error { return nil }
