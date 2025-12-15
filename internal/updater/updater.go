package updater

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	"github.com/MikeO7/HarborBuddy/pkg/log"
	"github.com/rs/zerolog"
)

// shortID returns a shortened version of a Docker ID, safe for any length
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

type pullCacheEntry struct {
	info  docker.ImageInfo
	err   error
	ready chan struct{}
}

// SafePullCache handles concurrent image pulls, ensuring only one pull per image happens at a time.
type SafePullCache struct {
	mu    sync.Mutex
	cache map[string]*pullCacheEntry
}

// NewSafePullCache creates a new SafePullCache
func NewSafePullCache() *SafePullCache {
	return &SafePullCache{
		cache: make(map[string]*pullCacheEntry),
	}
}

// GetOrPull returns the image info from cache or executes the pull function.
// If multiple goroutines request the same image, only one executes pullFunc, others wait.
func (c *SafePullCache) GetOrPull(ctx context.Context, image string, pullFunc func() (docker.ImageInfo, error)) (docker.ImageInfo, error, bool) {
	c.mu.Lock()
	entry, exists := c.cache[image]
	if !exists {
		// Create entry with open channel
		entry = &pullCacheEntry{
			ready: make(chan struct{}),
		}
		c.cache[image] = entry
		c.mu.Unlock()

		// Perform the pull (without lock)
		info, err := pullFunc()

		// Update entry and close channel
		// We don't need to lock to write to the entry fields because we are the only writer
		// (others are waiting on the channel), but for visibility/correctness we should
		// ensure the writes happen-before the close. The close happens-before the receive returns.
		entry.info = info
		entry.err = err
		close(entry.ready)

		return info, err, false
	}
	c.mu.Unlock()

	// Wait for the pull to complete
	select {
	case <-entry.ready:
		return entry.info, entry.err, true
	case <-ctx.Done():
		return docker.ImageInfo{}, ctx.Err(), false
	}
}

// RunUpdateCycle performs one complete update cycle
func RunUpdateCycle(ctx context.Context, cfg config.Config, dockerClient docker.Client) error {
	startTime := time.Now()
	log.Info("Starting update cycle")

	// Discovery phase: list all containers
	// Note: ListContainers is optimized to return a shallow list (no detailed Config/HostConfig)
	listStart := time.Now()
	containers, err := dockerClient.ListContainers(ctx)
	if err != nil {
		log.ErrorErr("Failed to list containers", err)
		return err
	}

	log.Infof("Found %d running containers (in %v)", len(containers), time.Since(listStart))

	updatedCount := 0
	skippedCount := 0

	// Cache for image pulls to avoid redundant network calls
	pullCache := NewSafePullCache()

	// Candidate list for updates
	type updateCandidate struct {
		container docker.ContainerInfo
		logger    *zerolog.Logger
	}
	candidates := make([]updateCandidate, 0)
	var candidatesMu sync.Mutex

	// Concurrency control
	concurrencyLimit := 5
	semaphore := make(chan struct{}, concurrencyLimit)
	var wg sync.WaitGroup

	// Phase 1: Check for updates in parallel
	for _, container := range containers {
		if err := ctx.Err(); err != nil {
			log.Warn("Update cycle interrupted")
			return err
		}

		// Create contextual logger for this container
		containerLogger := log.WithContainer(shortID(container.ID), container.Name)

		// Determine eligibility
		decision := DetermineEligibility(container, cfg.Updates)

		if !decision.Eligible {
			containerLogger.Debug().Msgf("Skipping container: %s", decision.Reason)
			skippedCount++
			continue
		}

		wg.Add(1)
		go func(c docker.ContainerInfo, l *zerolog.Logger) {
			defer wg.Done()
			semaphore <- struct{}{} // Acquire token
			defer func() { <-semaphore }() // Release token

			l.Debug().Msgf("Checking container for updates (Image: %s)", c.Image)

			// Check for updates
			needsUpdate, err := checkForUpdate(ctx, dockerClient, c, cfg.Updates.DryRun, l, pullCache)
			if err != nil {
				l.Error().Err(err).Msg("Failed to check for updates")
				return
			}

			if needsUpdate {
				candidatesMu.Lock()
				candidates = append(candidates, updateCandidate{container: c, logger: l})
				candidatesMu.Unlock()
			} else {
				l.Debug().Msg("Container is up to date")
			}
		}(container, containerLogger)
	}

	wg.Wait()

	// Phase 2: Apply updates sequentially
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			log.Warn("Update cycle interrupted during update phase")
			return err
		}

		container := candidate.container
		containerLogger := candidate.logger

		// Apply update
		if cfg.Updates.DryRun {
			containerLogger.Info().Msgf("[DRY-RUN] Would update container with image %s", container.Image)
			updatedCount++
		} else {
			// **CRITICAL**: ListContainers returns shallow info. Before acting, we MUST inspect the container
			// to get its full configuration (Env, Ports, Volumes, etc.).
			containerLogger.Debug().Msg("Fetching full container details before update...")
			fullContainer, err := dockerClient.InspectContainer(ctx, container.ID)
			if err != nil {
				containerLogger.Error().Err(err).Msg("Failed to inspect container for update details")
				continue
			}
			// Use the fully populated struct from here on
			container = fullContainer

			// Check if self-update
			isSelf, err := isSelf(container.ID)
			if err != nil {
				containerLogger.Warn().Err(err).Msg("Failed to check if container is self")
			}

			if isSelf {
				containerLogger.Info().Msg("Self-update detected! Triggering helper...")
				// selfupdate.Trigger exits the process on success, so we won't return here.
				if err := selfupdate.Trigger(ctx, dockerClient, container, container.Image); err != nil {
					containerLogger.Error().Err(err).Msg("Failed to trigger self-update")
				}
				// If we are here, it failed.
				continue
			}

			containerLogger.Info().Msgf("Updating container with image %s", container.Image)
			if err := updateContainer(ctx, cfg, dockerClient, container, containerLogger); err != nil {
				containerLogger.Error().Err(err).Msg("Failed to update container")
				continue
			}
			containerLogger.Info().Msg("Successfully updated container")
			updatedCount++
		}
	}

	log.Infof("Update cycle complete: %d updated, %d skipped, %d total (in %v)",
		updatedCount, skippedCount, len(containers), time.Since(startTime))
	return nil
}

// isSelf checks if the given container ID matches the current container's ID
func isSelf(id string) (bool, error) {
	// Try to read /etc/hostname
	hostname, err := os.Hostname()
	if err != nil {
		return false, err
	}

	// If hostname is the short ID (12 chars), we need to check if container.ID starts with it
	if strings.HasPrefix(id, hostname) {
		return true, nil
	}

	// If hostname is NOT the ID (custom hostname), we can try to read /proc/self/cgroup
	// This is more reliable.
	data, err := os.ReadFile("/proc/self/cgroup")
	if err == nil {
		content := string(data)
		if strings.Contains(content, id) {
			return true, nil
		}
	}

	return false, nil
}

// checkForUpdate checks if a container needs updating
func checkForUpdate(ctx context.Context, dockerClient docker.Client, container docker.ContainerInfo, dryRun bool, logger *zerolog.Logger, pullCache *SafePullCache) (bool, error) {
	// Get current image ID
	currentImageID := container.ImageID

	if dryRun {
		// In dry-run mode, we can't actually pull to check for updates
		// We log this limitation to be clear
		logger.Debug().Msgf("Pulling image %s", container.Image)
		logger.Info().Msgf("[DRY-RUN] Skipping image pull for %s. Cannot determine if update is available without pulling.", container.Image)
		return false, nil
	}

	// Get image info from cache or pull
	newImage, err, hit := pullCache.GetOrPull(ctx, container.Image, func() (docker.ImageInfo, error) {
		logger.Debug().Msgf("Pulling image %s", container.Image)
		return dockerClient.PullImage(ctx, container.Image)
	})

	if err != nil {
		return false, fmt.Errorf("failed to pull image: %w", err)
	}

	if hit {
		logger.Debug().Msgf("Using cached pull result for %s", container.Image)
	}

	// Compare image IDs
	if currentImageID == newImage.ID {
		logger.Debug().Msgf("Image IDs match: %s", shortID(currentImageID))
		return false, nil
	}

	logger.Info().Msgf("New image available for %s: %s -> %s", container.Image, shortID(currentImageID), shortID(newImage.ID))
	return true, nil
}

// updateContainer updates a container with a new image
func updateContainer(ctx context.Context, cfg config.Config, dockerClient docker.Client, container docker.ContainerInfo, logger *zerolog.Logger) error {
	logger.Info().Msg("Stopping container")

	// Create new container with updated image
	newID, err := dockerClient.CreateContainerLike(ctx, container, container.Image)
	if err != nil {
		return fmt.Errorf("failed to create new container: %w", err)
	}

	// Replace the old container with the new one
	if err := dockerClient.ReplaceContainer(ctx, container.ID, newID, container.Name, cfg.Updates.StopTimeout); err != nil {
		// The new ReplaceContainer handles its own rollback and cleanup.
		// We just need to check if the error is a warning or a fatal error.
		if err.Error()[0:7] == "warning" {
			logger.Warn().Msg(err.Error())
			return nil // Not a fatal error
		}
		return fmt.Errorf("failed to replace container: %w", err)
	}

	logger.Info().Msgf("Container updated successfully (old: %s, new: %s)", shortID(container.ID), shortID(newID))
	return nil
}
