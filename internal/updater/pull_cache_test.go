package updater

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MikeO7/HarborBuddy/internal/docker"
)

func TestSafePullCacheSharesResult(t *testing.T) {
	cache := newSafePullCache()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	pull := func() (docker.ImageInfo, error) {
		calls.Add(1)
		close(started)
		<-release
		return docker.ImageInfo{ID: "sha256:new"}, nil
	}

	var first docker.ImageInfo
	var firstErr error
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		first, firstErr = cache.GetOrPull(context.Background(), "app:latest", pull)
	}()
	<-started

	secondDone := make(chan struct{})
	var second docker.ImageInfo
	var secondErr error
	go func() {
		second, secondErr = cache.GetOrPull(context.Background(), "app:latest", pull)
		close(secondDone)
	}()
	close(release)
	wait.Wait()
	<-secondDone

	if firstErr != nil || secondErr != nil || first.ID != "sha256:new" || second.ID != first.ID {
		t.Fatalf("shared pull results first=%+v/%v second=%+v/%v", first, firstErr, second, secondErr)
	}
	if calls.Load() != 1 {
		t.Fatalf("pull calls = %d, want one shared pull", calls.Load())
	}
}

func TestSafePullCacheSharesNormalizedReferences(t *testing.T) {
	cache := newSafePullCache()
	var calls atomic.Int32
	pull := func() (docker.ImageInfo, error) {
		calls.Add(1)
		return docker.ImageInfo{ID: "sha256:nginx"}, nil
	}
	for _, ref := range []string{"nginx", "nginx:latest", "docker.io/library/nginx:latest"} {
		if info, err := cache.GetOrPull(context.Background(), ref, pull); err != nil || info.ID != "sha256:nginx" {
			t.Fatalf("GetOrPull(%q) = %+v, %v", ref, info, err)
		}
	}
	if _, err := cache.GetOrPull(context.Background(), "nginx:1.27", pull); err != nil {
		t.Fatalf("GetOrPull(distinct tag) error = %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("pull calls = %d, want 2", calls.Load())
	}
}

func TestSafePullCacheWaiterCanCancel(t *testing.T) {
	cache := newSafePullCache()
	started := make(chan struct{})
	release := make(chan struct{})
	pull := func() (docker.ImageInfo, error) {
		close(started)
		<-release
		return docker.ImageInfo{ID: "sha256:new"}, nil
	}

	firstDone := make(chan struct{})
	go func() {
		_, _ = cache.GetOrPull(context.Background(), "app:latest", pull)
		close(firstDone)
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := cache.GetOrPull(ctx, "app:latest", pull)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter error = %v", err)
	}

	close(release)
	<-firstDone
	info, err := cache.GetOrPull(context.Background(), "app:latest", func() (docker.ImageInfo, error) {
		t.Fatal("completed cache entry pulled again")
		return docker.ImageInfo{}, nil
	})
	if err != nil || info.ID != "sha256:new" {
		t.Fatalf("completed entry info=%+v err=%v", info, err)
	}
}
