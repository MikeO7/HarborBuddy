package updater

import (
	"context"
	"fmt"
	"os"
	"strings"
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

// RunUpdateCycle performs one complete update cycle
func RunUpdateCycle(ctx context.Context, cfg config.Config, dockerClient docker.Client) error {
	startTime := time.Now()
	log.Info("Starting update cycle")

	// Discovery phase: list all containers
	listStart := time.Now()
	containers, err := dockerClient.ListContainers(ctx)
	if err != nil {
		log.ErrorErr("Failed to list containers", err)
		return err
	}

	log.Infof("Found %d running containers (in %v)", len(containers), time.Since(listStart))

	updatedCount := 0
	skippedCount := 0

	// Process each container
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

		containerLogger.Debug().Msgf("Checking container for updates (Image: %s)", container.Image)

		// Check for updates
		needsUpdate, err := checkForUpdate(ctx, dockerClient, container, cfg.Updates.DryRun, containerLogger)
		if err != nil {
			containerLogger.Error().Err(err).Msg("Failed to check for updates")
			continue
		}

		if !needsUpdate {
			containerLogger.Debug().Msg("Container is up to date")
			continue
		}

		// Apply update
		if cfg.Updates.DryRun {
			containerLogger.Info().Msgf("[DRY-RUN] Would update container with image %s", container.Image)
			updatedCount++
		} else {
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
func checkForUpdate(ctx context.Context, dockerClient docker.Client, container docker.ContainerInfo, dryRun bool, logger *zerolog.Logger) (bool, error) {
	// Get current image ID
	currentImageID := container.ImageID

	// Pull the latest version of the image
	logger.Debug().Msgf("Pulling image %s", container.Image)

	if dryRun {
		// In dry-run mode, we can't actually pull to check for updates
		// We log this limitation to be clear
		logger.Info().Msgf("[DRY-RUN] Skipping image pull for %s. Cannot determine if update is available without pulling.", container.Image)
		return false, nil
	}

	newImage, err := dockerClient.PullImage(ctx, container.Image)
	if err != nil {
		return false, fmt.Errorf("failed to pull image: %w", err)
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
