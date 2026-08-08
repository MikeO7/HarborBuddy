package updater

import (
	"context"
	"sync"

	"github.com/MikeO7/HarborBuddy/internal/docker"
)

type pullCacheEntry struct {
	info  docker.ImageInfo
	err   error
	ready chan struct{}
}

type SafePullCache struct {
	mu      sync.Mutex
	entries map[string]*pullCacheEntry
}

func NewSafePullCache() *SafePullCache {
	return &SafePullCache{entries: make(map[string]*pullCacheEntry)}
}

func (c *SafePullCache) GetOrPull(ctx context.Context, imageRef string, pull func() (docker.ImageInfo, error)) (docker.ImageInfo, error, bool) {
	c.mu.Lock()
	entry, exists := c.entries[imageRef]
	if !exists {
		entry = &pullCacheEntry{ready: make(chan struct{})}
		c.entries[imageRef] = entry
		c.mu.Unlock()

		entry.info, entry.err = pull()
		close(entry.ready)
		return entry.info, entry.err, false
	}
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return docker.ImageInfo{}, ctx.Err(), true
	case <-entry.ready:
		return entry.info, entry.err, true
	}
}
