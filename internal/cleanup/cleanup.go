package cleanup

import (
	"context"
	"strings"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
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

// RunCleanup performs image cleanup based on configuration
func RunCleanup(ctx context.Context, cfg config.Config, dockerClient docker.Client) error {
	if !cfg.Cleanup.Enabled {
		log.Debug("Cleanup is disabled")
		return nil
	}

	startTime := time.Now()
	log.Info("Starting image cleanup")

	// List images
	listStart := time.Now()
	var images []docker.ImageInfo
	var err error

	if cfg.Cleanup.DanglingOnly {
		log.Debug("Listing only dangling images")
		images, err = dockerClient.ListDanglingImages(ctx)
	} else {
		log.Debug("Listing all images")
		images, err = dockerClient.ListImages(ctx)
	}

	if err != nil {
		log.ErrorErr("Failed to list images", err)
		return err
	}

	log.Infof("Found %d images (in %v)", len(images), time.Since(listStart))

	minAge := time.Duration(cfg.Cleanup.MinAgeHours) * time.Hour
	removedCount := 0
	skippedCount := 0
	var totalReclaimed int64

	for _, image := range images {
		if err := ctx.Err(); err != nil {
			log.Warn("Cleanup interrupted")
			return err
		}

		// Create contextual logger for this image
		imageTag := "none"
		if len(image.RepoTags) > 0 {
			imageTag = strings.Join(image.RepoTags, ",")
		}
		imageLogger := log.WithImage(shortID(image.ID), imageTag)

		// Check if image is eligible for cleanup
		if !isEligibleForCleanup(image, cfg.Cleanup, minAge, imageLogger) {
			skippedCount++
			continue
		}

		sizeStr := util.FormatBytes(image.Size)
		imageLogger.Info().Msgf("Removing image (tags: %v, size: %s)", image.RepoTags, sizeStr)
		if err := dockerClient.RemoveImage(ctx, image.ID); err != nil {
			imageLogger.Error().Err(err).Msg("Failed to remove image")
			skippedCount++
			continue
		}

		imageLogger.Info().Msgf("Successfully removed image. Reclaimed %s", sizeStr)
		removedCount++
		totalReclaimed += image.Size
	}

	log.Infof("Cleanup complete: %d removed, %d skipped, %d total. Total space reclaimed: %s (in %v)",
		removedCount, skippedCount, len(images), util.FormatBytes(totalReclaimed), time.Since(startTime))
	return nil
}

// isEligibleForCleanup determines if an image is eligible for cleanup
func isEligibleForCleanup(image docker.ImageInfo, cfg config.CleanupConfig, minAge time.Duration, logger *zerolog.Logger) bool {
	// Check if image is old enough
	age := time.Since(image.CreatedAt)
	if age < minAge {
		logger.Debug().Msgf("Image is too new (age: %v, min: %v)", age, minAge)
		return false
	}

	// If dangling_only mode, only consider dangling images
	if cfg.DanglingOnly {
		if !image.Dangling {
			logger.Debug().Msg("Image is not dangling")
			return false
		}
	}

	return true
}
