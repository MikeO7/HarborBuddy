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
	cache := NewSafePullCache()
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
		first, firstErr, _ = cache.GetOrPull(context.Background(), "app:latest", pull)
	}()
	<-started

	secondDone := make(chan struct{})
	var second docker.ImageInfo
	var secondErr error
	var shared bool
	go func() {
		second, secondErr, shared = cache.GetOrPull(context.Background(), "app:latest", pull)
		close(secondDone)
	}()
	close(release)
	wait.Wait()
	<-secondDone

	if firstErr != nil || secondErr != nil || first.ID != "sha256:new" || second.ID != first.ID {
		t.Fatalf("shared pull results first=%+v/%v second=%+v/%v", first, firstErr, second, secondErr)
	}
	if !shared || calls.Load() != 1 {
		t.Fatalf("shared=%v calls=%d, want true/1", shared, calls.Load())
	}
}

func TestSafePullCacheWaiterCanCancel(t *testing.T) {
	cache := NewSafePullCache()
	started := make(chan struct{})
	release := make(chan struct{})
	pull := func() (docker.ImageInfo, error) {
		close(started)
		<-release
		return docker.ImageInfo{ID: "sha256:new"}, nil
	}

	firstDone := make(chan struct{})
	go func() {
		_, _, _ = cache.GetOrPull(context.Background(), "app:latest", pull)
		close(firstDone)
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err, shared := cache.GetOrPull(ctx, "app:latest", pull)
	if !shared || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter shared=%v error=%v", shared, err)
	}

	close(release)
	<-firstDone
	info, err, shared := cache.GetOrPull(context.Background(), "app:latest", func() (docker.ImageInfo, error) {
		t.Fatal("completed cache entry pulled again")
		return docker.ImageInfo{}, nil
	})
	if err != nil || !shared || info.ID != "sha256:new" {
		t.Fatalf("completed entry info=%+v err=%v shared=%v", info, err, shared)
	}
}
