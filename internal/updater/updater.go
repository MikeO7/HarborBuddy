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
	"github.com/MikeO7/HarborBuddy/pkg/util"
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

// RunUpdateCycle performs the update logic for all containers
func RunUpdateCycle(ctx context.Context, cfg config.Config, dockerClient docker.Client, logger *zerolog.Logger) error {
	startTime := time.Now()
	logger.Info().Msg("Starting update cycle")

	// Discovery phase: list all containers
	containers, err := dockerClient.ListContainers(ctx)
	if err != nil {
		log.ErrorWithHint("Failed to list containers", "Ensure Docker daemon is running and socket is accessible", err)
		return err
	}

	logger.Info().Msgf("🔎 Checking %d containers for updates...", len(containers))

	// Safe pull cache for this cycle
	pullCache := NewSafePullCache()

	// Use a mutex to protect shared counters if we were parallelizing (we aren't yet fully, but good practice)
	// Actually, we are running check in parallel!
	var candidatesMu sync.Mutex
	type updateCandidate struct {
		Container docker.ContainerInfo
		NewImage  docker.ImageInfo
		Logger    *zerolog.Logger
	}
	// Pre-allocate to avoid resizing during concurrent append
	updateCandidates := make([]updateCandidate, 0, len(containers))

	skippedCount := 0
	errorCount := 0
	updatedCount := 0

	// Parallel check
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5) // Concurrency limit

	for _, container := range containers {
		// Check for context cancellation
		if err := ctx.Err(); err != nil {
			logger.Warn().Msg("Update cycle interrupted")
			return err
		}

		// Determine eligibility
		decision := DetermineEligibility(container, cfg.Updates)

		if !decision.Eligible {
			// Optimization: Avoid creating a child logger just to skip
			logger.Debug().
				Str("container_id", shortID(container.ID)).
				Str("container_name", container.Name).
				Msgf("Skipping container: %s", decision.Reason)
			skippedCount++
			continue
		}

		// Create contextual logger for this container
		containerLogger := logger.With().
			Str("container_id", shortID(container.ID)).
			Str("container_name", container.Name).
			Logger()
		containerLoggerPtr := &containerLogger

		wg.Add(1)
		go func(c docker.ContainerInfo, l *zerolog.Logger) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			// Check updates
			needsUpdate, err := checkForUpdate(ctx, dockerClient, c, cfg.Updates.DryRun, l, pullCache)
			if err != nil {
				// We don't have access to ErrorWithHint on 'l' (zerolog logger) directly easily unless we wrap or use global
				// But we can just use normal logging here or improved message.
				// The global log.ErrorWithHint uses global logger.
				// We can mimic it: l.Error().Err(err).Str("hint", "...").Msg(...)

				// Provide hint for common pull errors
				hint := "Check image name spelling and registry credentials"
				if strings.Contains(err.Error(), "404") {
					hint = "Image not found"
				} else if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
					hint = "Authentication failed - check `config.json`"
				}

				l.Error().Err(err).Str("hint", hint).Msg("Failed to check for updates")
				candidatesMu.Lock()
				errorCount++
				candidatesMu.Unlock()
				return
			}

			if !needsUpdate {
				candidatesMu.Lock()
				skippedCount++
				candidatesMu.Unlock()
				return
			}

			// If needs update, add to candidates
			// We need to re-fetch the image info or just store what we found?
			// checkForUpdate returns bool, but we need the new image info to proceed?
			// Actually checkForUpdate logic just checks compatibility.
			// The current implementation re-pulls inside checkForUpdate but doesn't return the ImageInfo.
			// We should probably rely on updateContainer doing the work or refactor.
			// Currently updateContainer re-pulls/creates.

			// For now, just add to candidates list
			candidatesMu.Lock()
			updateCandidates = append(updateCandidates, updateCandidate{
				Container: c,
				Logger:    l,
			})
			candidatesMu.Unlock()

		}(container, containerLoggerPtr)
	}

	wg.Wait()

	// Apply updates sequentially
	if len(updateCandidates) > 0 {
		logger.Info().Msgf("♻️  Found %d containers to update. Applying updates...", len(updateCandidates))

		for _, candidate := range updateCandidates {
			if err := ctx.Err(); err != nil {
				logger.Warn().Msg("Update cycle interrupted during application")
				return err
			}

			container := candidate.Container
			containerLogger := candidate.Logger

			// Double check if it's a self-update situation
			// Note: isSelf is likely a helper in this package
			isSelf, err := isSelf(container.ID)
			if err != nil {
				containerLogger.Warn().Err(err).Msg("Failed to check if container is self")
				errorCount++
			}

			if isSelf {
				containerLogger.Info().Msg("Self-update detected! Triggering helper...")
				if err := selfupdate.Trigger(ctx, dockerClient, container, container.Image); err != nil {
					containerLogger.Error().Err(err).Msg("Failed to trigger self-update")
					errorCount++
				}
				continue
			}

			if err := updateContainer(ctx, cfg, dockerClient, container, containerLogger); err != nil {
				containerLogger.Error().Err(err).Msg("Failed to update container")
				errorCount++
				continue
			}

			// Friendly update message implied by updateContainer success
			// logger.Info().Msgf("✅ Updated %s to ...", ...) -- updateContainer does this
			updatedCount++
		}
	}

	logger.Info().Msgf("✨ Update cycle complete: %d updated, %d skipped, %d errors, %d total (taken %v)",
		updatedCount, skippedCount, errorCount, len(containers), time.Since(startTime).Round(time.Millisecond))
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

// checkIsSelf is the core logic for checking if we are running in the target container
func checkIsSelf(targetID string, hostname string, cgroupContent string) bool {
	// 1. Check if hostname matches short ID
	if len(targetID) >= 12 && strings.HasPrefix(targetID, hostname) && len(hostname) > 0 {
		return true
	}

	// 2. Check cgroup content (more reliable for Docker)
	if strings.Contains(cgroupContent, targetID) {
		return true
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

	friendlyName := util.GetImageFriendlyName(newImage.Labels)
	displayImg := newImage.ID
	if friendlyName != "" {
		displayImg = friendlyName
	}
	// Fallback to shortID if no friendly name but keep ID for ref
	if friendlyName == "" {
		displayImg = shortID(newImage.ID)
	}

	logger.Info().
		Str("container_name", container.Name).
		Str("image", container.Image).
		Str("current_id", shortID(currentImageID)).
		Str("new_id", displayImg).
		Msg("🚀 Update found")
	return true, nil
}

// updateContainer updates a container with a new image
func updateContainer(ctx context.Context, cfg config.Config, dockerClient docker.Client, container docker.ContainerInfo, logger *zerolog.Logger) error {
	// We need full container info (Config, HostConfig, etc.) which ListContainers doesn't provide
	// So we inspect the container first
	fullContainer, err := dockerClient.InspectContainer(ctx, container.ID)
	if err != nil {
		return fmt.Errorf("failed to inspect container for update: %w", err)
	}

	logger.Info().
		Str("container", fullContainer.Name).
		Msg("Stopping container")

	// Create new container with updated image
	newID, err := dockerClient.CreateContainerLike(ctx, fullContainer, fullContainer.Image)
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

	logger.Info().
		Str("container_name", container.Name).
		Str("old_id", shortID(container.ID)).
		Str("new_id", shortID(newID)).
		Msg("✅  Container replacement successful")
	return nil
}
