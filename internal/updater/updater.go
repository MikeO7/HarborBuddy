package updater

import (
	"context"
	"fmt"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/pkg/log"
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

		// Determine eligibility
		decision := DetermineEligibility(container, cfg.Updates)

		if !decision.Eligible {
			log.Infof("Skipping container %s (%s): %s", container.Name, shortID(container.ID), decision.Reason)
			skippedCount++
			continue
		}

		log.Infof("Checking container %s (%s) for updates", container.Name, shortID(container.ID))

		// Check for updates
		needsUpdate, err := checkForUpdate(ctx, dockerClient, container, cfg.Updates.DryRun)
		if err != nil {
			log.Errorf("Failed to check for updates for container %s: %v", container.Name, err)
			continue
		}

		if !needsUpdate {
			log.Infof("Container %s is up to date", container.Name)
			continue
		}

		// Apply update
		if cfg.Updates.DryRun {
			log.Infof("[DRY-RUN] Would update container %s with image %s", container.Name, container.Image)
			updatedCount++
		} else {
			log.Infof("Updating container %s with image %s", container.Name, container.Image)
			if err := updateContainer(ctx, cfg, dockerClient, container); err != nil {
				log.Errorf("Failed to update container %s: %v", container.Name, err)
				continue
			}
			log.Infof("Successfully updated container %s", container.Name)
			updatedCount++
		}
	}

	log.Infof("Update cycle complete: %d updated, %d skipped, %d total (in %v)",
		updatedCount, skippedCount, len(containers), time.Since(startTime))
	return nil
}

// checkForUpdate checks if a container needs updating
func checkForUpdate(ctx context.Context, dockerClient docker.Client, container docker.ContainerInfo, dryRun bool) (bool, error) {
	// Get current image ID
	currentImageID := container.ImageID

	// Pull the latest version of the image
	log.Debugf("Pulling image %s", container.Image)

	if dryRun {
		// In dry-run mode, we can't actually pull to check for updates
		// We log this limitation to be clear
		log.Infof("[DRY-RUN] Skipping image pull for %s. Cannot determine if update is available without pulling.", container.Image)
		return false, nil
	}

	newImage, err := dockerClient.PullImage(ctx, container.Image)
	if err != nil {
		return false, fmt.Errorf("failed to pull image: %w", err)
	}

	// Compare image IDs
	if currentImageID == newImage.ID {
		log.Debugf("Image IDs match: %s", shortID(currentImageID))
		return false, nil
	}

	log.Infof("New image available for %s: %s -> %s", container.Image, shortID(currentImageID), shortID(newImage.ID))
	return true, nil
}

// updateContainer updates a container with a new image
func updateContainer(ctx context.Context, cfg config.Config, dockerClient docker.Client, container docker.ContainerInfo) error {
	log.Infof("Stopping container %s", container.Name)

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
			log.Warn(err.Error())
			return nil // Not a fatal error
		}
		return fmt.Errorf("failed to replace container: %w", err)
	}

	log.Infof("Container %s updated successfully (old: %s, new: %s)", container.Name, shortID(container.ID), shortID(newID))
	return nil
}
