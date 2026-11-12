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

type safePullCache struct {
	mu      sync.Mutex
	entries map[string]*pullCacheEntry
}

func newSafePullCache() *safePullCache {
	return &safePullCache{entries: make(map[string]*pullCacheEntry)}
}

// GetOrPull deduplicates pulls by normalized reference, so "nginx",
// "nginx:latest", and "docker.io/library/nginx:latest" share one pull.
func (c *safePullCache) GetOrPull(ctx context.Context, imageRef string, pull func() (docker.ImageInfo, error)) (docker.ImageInfo, error) {
	key := imageref.Normalize(imageRef)
	c.mu.Lock()
	entry, exists := c.entries[key]
	if !exists {
		entry = &pullCacheEntry{ready: make(chan struct{})}
		c.entries[key] = entry
		c.mu.Unlock()

		entry.info, entry.err = pull()
		close(entry.ready)
		return entry.info, entry.err
	}
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return docker.ImageInfo{}, ctx.Err()
	case <-entry.ready:
		return entry.info, entry.err
	}
}
