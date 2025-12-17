package cleanup

import (
	"context"
	"strings"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
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
func RunCleanup(ctx context.Context, cfg config.Config, dockerClient docker.Client, logger *zerolog.Logger) error {
	if !cfg.Cleanup.Enabled {
		logger.Debug().Msg("Cleanup is disabled")
		return nil
	}

	logger.Info().Msg("Starting image cleanup")

	// List images
	listStart := time.Now()
	var images []docker.ImageInfo
	var err error

	if cfg.Cleanup.DanglingOnly {
		logger.Debug().Msg("Listing only dangling images")
		images, err = dockerClient.ListDanglingImages(ctx)
	} else {
		logger.Debug().Msg("Listing all images")
		images, err = dockerClient.ListImages(ctx)
	}

	if err != nil {
		logger.Error().Err(err).Msg("Failed to list images")
		return err
	}

	logger.Info().Int64("duration_ms", time.Since(listStart).Milliseconds()).Msgf("Found %d images (in %v)", len(images), time.Since(listStart))

	minAge := time.Duration(cfg.Cleanup.MinAgeHours) * time.Hour
	removedCount := 0
	skippedCount := 0
	var totalReclaimed int64

	for _, image := range images {
		if err := ctx.Err(); err != nil {
			logger.Warn().Msg("Cleanup interrupted")
			return err
		}

		// Create contextual logger for this image
		imageTag := "none"
		if len(image.RepoTags) > 0 {
			imageTag = strings.Join(image.RepoTags, ",")
		}

		// Derive from parent logger to keep cycle_id
		imageLogger := logger.With().
			Str("image_id", shortID(image.ID)).
			Str("image_tag", imageTag).
			Logger()
		imageLoggerPtr := &imageLogger

		// Check if image is eligible for cleanup
		if !isEligibleForCleanup(image, cfg.Cleanup, minAge, imageLoggerPtr) {
			skippedCount++
			continue
		}

		sizeStr := util.FormatBytes(image.Size)
		// Log attempt at Debug level to reduce noise
		imageLogger.Debug().Msgf("Attempting to remove image (tags: %v, size: %s)", image.RepoTags, sizeStr)

		if err := dockerClient.RemoveImage(ctx, image.ID); err != nil {
			imageLogger.Error().Err(err).Msg("Failed to remove image")
			skippedCount++
			continue
		}

		// Friendly "Removed" message
		tagDisplay := "Dangling"
		if len(image.RepoTags) > 0 {
			tagDisplay = strings.Join(image.RepoTags, ", ")
		}
		imageLogger.Info().Msgf("🗑️  Removed image %s (%s) | Reclaimed: %s", shortID(image.ID), tagDisplay, sizeStr)
		removedCount++
		totalReclaimed += image.Size
	}

	logger.Info().Msgf("✨ Cleanup complete: %d removed. Space Reclaimed: %s", removedCount, util.FormatBytes(totalReclaimed))
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
