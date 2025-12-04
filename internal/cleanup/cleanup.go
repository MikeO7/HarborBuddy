package cleanup

import (
	"context"
	"time"

	"github.com/mikeo/harborbuddy/internal/config"
	"github.com/mikeo/harborbuddy/internal/docker"
	"github.com/mikeo/harborbuddy/pkg/log"
)

// RunCleanup performs image cleanup based on configuration
func RunCleanup(ctx context.Context, cfg config.Config, dockerClient docker.Client) error {
	if !cfg.Cleanup.Enabled {
		log.Debug("Cleanup is disabled")
		return nil
	}

	startTime := time.Now()
	log.Info("Starting image cleanup")

	// List all images
	listStart := time.Now()
	images, err := dockerClient.ListImages(ctx)
	if err != nil {
		log.ErrorErr("Failed to list images", err)
		return err
	}

	log.Infof("Found %d images (in %v)", len(images), time.Since(listStart))

	minAge := time.Duration(cfg.Cleanup.MinAgeHours) * time.Hour
	removedCount := 0
	skippedCount := 0

	for _, image := range images {
		if err := ctx.Err(); err != nil {
			log.Warn("Cleanup interrupted")
			return err
		}

		// Check if image is eligible for cleanup
		if !isEligibleForCleanup(image, cfg.Cleanup, minAge) {
			skippedCount++
			continue
		}

		// Try to remove the image
		shortID := image.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		log.Infof("Removing image %s (tags: %v)", shortID, image.RepoTags)
		if err := dockerClient.RemoveImage(ctx, image.ID); err != nil {
			log.Errorf("Failed to remove image %s: %v", shortID, err)
			skippedCount++
			continue
		}

		log.Infof("Successfully removed image %s", shortID)
		removedCount++
	}

	log.Infof("Cleanup complete: %d removed, %d skipped, %d total (in %v)",
		removedCount, skippedCount, len(images), time.Since(startTime))
	return nil
}

// isEligibleForCleanup determines if an image is eligible for cleanup
func isEligibleForCleanup(image docker.ImageInfo, cfg config.CleanupConfig, minAge time.Duration) bool {
	// Check if image is old enough
	age := time.Since(image.CreatedAt)
	if age < minAge {
		shortID := image.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		log.Debugf("Image %s is too new (age: %v, min: %v)", shortID, age, minAge)
		return false
	}

	// If dangling_only mode, only consider dangling images
	if cfg.DanglingOnly {
		if !image.Dangling {
			shortID := image.ID
			if len(shortID) > 12 {
				shortID = shortID[:12]
			}
			log.Debugf("Image %s is not dangling", shortID)
			return false
		}
	}

	return true
}
