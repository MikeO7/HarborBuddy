package cleanup

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/rs/zerolog"
)

type ImageResult struct {
	Image       docker.ImageInfo
	Eligible    bool
	Removed     bool
	WouldRemove bool
	Reason      string
	Err         error
}

type Report struct {
	Results        []ImageResult
	ReclaimedBytes int64
}

func RunCleanup(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger) (Report, error) {
	return runCleanupAt(ctx, cfg, client, logger, time.Now())
}

func runCleanupAt(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger, now time.Time) (Report, error) {
	var images []docker.ImageInfo
	var err error
	if cfg.Cleanup.DanglingOnly {
		images, err = client.ListDanglingImages(ctx)
	} else {
		images, err = client.ListImages(ctx)
	}
	if err != nil {
		return Report{}, fmt.Errorf("list images for cleanup: %w", err)
	}
	sort.Slice(images, func(i, j int) bool {
		if images[i].CreatedAt.Equal(images[j].CreatedAt) {
			return images[i].ID < images[j].ID
		}
		return images[i].CreatedAt.Before(images[j].CreatedAt)
	})

	minimumAge := time.Duration(cfg.Cleanup.MinAgeHours) * time.Hour
	report := Report{Results: make([]ImageResult, 0, len(images))}
	for _, image := range images {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		result := ImageResult{Image: image}
		switch {
		case now.Sub(image.CreatedAt) < minimumAge:
			result.Reason = "image is newer than the minimum age"
		case cfg.Cleanup.DanglingOnly && !image.Dangling:
			result.Reason = "image is not dangling"
		default:
			result.Eligible = true
			if cfg.Updates.DryRun {
				result.WouldRemove = true
			} else if removeErr := client.RemoveImage(ctx, image.ID); removeErr != nil {
				result.Err = removeErr
			} else {
				result.Removed = true
				report.ReclaimedBytes += image.Size
			}
		}
		report.Results = append(report.Results, result)
		logImageResult(logger, result)
	}

	logger.Info().
		Int("images", len(report.Results)).
		Int64("reclaimed_bytes", report.ReclaimedBytes).
		Str("reclaimed", formatBytes(report.ReclaimedBytes)).
		Bool("dry_run", cfg.Updates.DryRun).
		Msg("Image cleanup complete")
	return report, nil
}

func logImageResult(logger zerolog.Logger, result ImageResult) {
	event := logger.Info().
		Str("image_id", shortID(result.Image.ID)).
		Str("image_tags", strings.Join(result.Image.RepoTags, ",")).
		Int64("image_size", result.Image.Size)
	switch {
	case result.Err != nil:
		event.Err(result.Err).Str("result", "failed").Msg("Image cleanup failed")
	case result.Removed:
		event.Str("result", "removed").Msg("Image removed")
	case result.WouldRemove:
		event.Str("result", "would_remove").Msg("Image would be removed")
	default:
		event.Str("result", "skipped").Str("reason", result.Reason).Msg("Image cleanup skipped")
	}
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	divisor, exponent := int64(unit), 0
	for value := bytes / unit; value >= unit; value /= unit {
		divisor *= unit
		exponent++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(divisor), "KMGTPE"[exponent])
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
