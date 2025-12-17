package updater

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
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

		// Update entry and close channel.
		// Note: The channel close acts as a memory barrier, ensuring readers see these writes.
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
func RunUpdateCycle(ctx context.Context, cfg config.Config, dockerClient docker.Client, logger *zerolog.Logger) error {
	logger.Info().Msg("Starting update cycle")

	// Discovery phase: list all containers
	// Note: ListContainers is optimized to return a shallow list (no detailed Config/HostConfig)
	containers, err := dockerClient.ListContainers(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to list containers")
		return err
	}

	logger.Info().Msgf("🔎 Checking %d containers for updates...", len(containers))

	updatedCount := 0
	skippedCount := 0

	// Cache for image pulls to avoid redundant network calls
	pullCache := NewSafePullCache()

	// Candidate list for updates
	type updateCandidate struct {
		container docker.ContainerInfo
		logger    *zerolog.Logger
	}
	// Pre-allocate slice with capacity equal to total containers to avoid reallocations
	candidates := make([]updateCandidate, 0, len(containers))
	var candidatesMu sync.Mutex

	// Concurrency control
	concurrencyLimit := 5
	semaphore := make(chan struct{}, concurrencyLimit)
	var wg sync.WaitGroup

	// Phase 1: Check for updates in parallel
	for _, container := range containers {
		if err := ctx.Err(); err != nil {
			logger.Warn().Msg("Update cycle interrupted")
			return err
		}

		// Create contextual logger for this container
		// We use the passed logger instead of creating new one from global so we keep the cycle_id
		containerLogger := logger.With().
			Str("container_id", shortID(container.ID)).
			Str("container_name", container.Name).
			Logger()
		containerLoggerPtr := &containerLogger

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
			semaphore <- struct{}{}        // Acquire token
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
		}(container, containerLoggerPtr)
	}

	wg.Wait()

	// Phase 2: Apply updates sequentially
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			logger.Warn().Msg("Update cycle interrupted during update phase")
			return err
		}

		container := candidate.container
		containerLogger := candidate.logger

		// Apply update
		if cfg.Updates.DryRun {
			containerLogger.Info().Msgf("[DRY-RUN] Would update container with image %s", container.Image)
			updatedCount++
		} else {
			// Note: ListContainers returns shallow info. We must inspect the container
			// to get its full configuration (Env, Ports, Volumes, etc.) before updating.
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

			if err := updateContainer(ctx, cfg, dockerClient, container, containerLogger); err != nil {
				containerLogger.Error().Err(err).Msg("Failed to update container")
				continue
			}

			// Friendly update message
			// We can get the new image ID from the container we just associated with the name,
			// but we also have newID returned from CreateContainerLike.

			// We want: "✅ Updated <container_name> to <new_image_short_sha>"
			// Note: updateContainer doesn't return the new ID, so we can't easily print it here
			// unless we refactor updateContainer or rely on updateContainer to log it.
			// Actually, updateContainer DOES log the success message now (modified in previous step).
			// So we can just rely on that, or log a high level one.
			// Let's rely on updateContainer's message which we updated to be friendly.
			updatedCount++
		}
	}

	logger.Info().Msgf("✨ Update cycle complete: %d updated, %d skipped, %d total",
		updatedCount, skippedCount, len(containers))
	return nil
}

// isSelf checks if the given container ID matches the current container's ID
func isSelf(id string) (bool, error) {
	// Try to read /etc/hostname
	hostname, err := os.Hostname()
	if err != nil {
		return false, err
	}

	// Try to read /proc/self/cgroup
	cgroupContent := ""
	data, err := os.ReadFile("/proc/self/cgroup")
	if err == nil {
		cgroupContent = string(data)
	}

	return checkIsSelf(id, hostname, cgroupContent), nil
}

// checkIsSelf contains the logic for isSelf, separated for testing and security
func checkIsSelf(id, hostname, cgroupContent string) bool {
	// Security: If hostname is empty, we must NOT use it for prefix check.
	// strings.HasPrefix(id, "") is always true, which would cause all containers to match.
	if hostname != "" {
		// If hostname is the short ID (12 chars), we need to check if container.ID starts with it
		if strings.HasPrefix(id, hostname) {
			return true
		}
	}

	// Check cgroup content if available
	if cgroupContent != "" {
		if strings.Contains(cgroupContent, id) {
			return true
		}
	}

	return false
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

	logger.Info().Msgf("🚀 Update found for %s (%s): %s -> %s", container.Name, container.Image, shortID(currentImageID), shortID(newImage.ID))
	return true, nil
}

// updateContainer updates a container with a new image
func updateContainer(ctx context.Context, cfg config.Config, dockerClient docker.Client, container docker.ContainerInfo, logger *zerolog.Logger) error {
	logger.Info().Msgf("Stopping container %s", container.Name)

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

	logger.Info().Msgf("✅  Container replacement successful (old: %s, new: %s)", shortID(container.ID), shortID(newID))
	return nil
}
