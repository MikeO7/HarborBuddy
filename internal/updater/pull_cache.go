package updater

import (
	"context"
	"sync"

	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/imageref"
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

// GetOrPull deduplicates pulls by normalized reference, so "nginx",
// "nginx:latest", and "docker.io/library/nginx:latest" share one pull.
func (c *SafePullCache) GetOrPull(ctx context.Context, imageRef string, pull func() (docker.ImageInfo, error)) (docker.ImageInfo, error, bool) {
	key := imageref.Normalize(imageRef)
	c.mu.Lock()
	entry, exists := c.entries[key]
	if !exists {
		entry = &pullCacheEntry{ready: make(chan struct{})}
		c.entries[key] = entry
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
