package cleanup

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/rs/zerolog"
)

type Result struct {
	Resource    docker.CleanupResource
	ReferenceAt time.Time
	AgeHours    int64
	Eligible    bool
	Removed     bool
	WouldRemove bool
	Reason      string
	Err         error
}

type Report struct {
	Results        []Result
	ReclaimedBytes int64
	Duration       time.Duration
}

type resourceCleaner interface {
	ListCleanupResources(context.Context, docker.CleanupResourceKind) ([]docker.CleanupResource, error)
	RemoveCleanupResource(context.Context, docker.CleanupResource) error
	PruneBuildCache(context.Context, time.Time) (docker.CleanupPruneResult, error)
}

func RunCleanup(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger) (Report, error) {
	return runCleanupAt(ctx, cfg, client, logger, time.Now())
}

func runCleanupAt(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger, now time.Time) (report Report, returnErr error) {
	started := time.Now()
	defer func() {
		report.Duration = time.Since(started)
		logReport(logger, report, cfg.Updates.DryRun, returnErr)
	}()
	report, err := cleanupImages(ctx, cfg, client, logger, now)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return report, errors.Join(err, ctxErr)
	}
	kinds := enabledResourceKinds(cfg.Cleanup)
	if len(kinds) == 0 {
		return report, err
	}

	cleaner, ok := client.(resourceCleaner)
	if !ok {
		return report, errors.Join(err, fmt.Errorf("docker client %T does not support extended cleanup", client))
	}
	for _, kind := range kinds {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return report, errors.Join(err, ctxErr)
		}
		var kindErr error
		report, kindErr = cleanupResourceKind(ctx, cfg, cleaner, logger, now, kind, report)
		err = errors.Join(err, kindErr)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return report, errors.Join(err, ctxErr)
	}
	return report, err
}

func cleanupImages(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger, now time.Time) (Report, error) {
	if cfg.Cleanup.All {
		if cleaner, ok := client.(resourceCleaner); ok {
			resources, err := cleaner.ListCleanupResources(ctx, docker.CleanupImage)
			if err != nil {
				return Report{}, fmt.Errorf("list images for cleanup: %w", err)
			}
			return removeResources(ctx, cfg, cleaner, logger, now, docker.CleanupImage, resources, Report{}), nil
		}
	}
	var images []docker.ImageInfo
	var err error
	danglingOnly := cfg.Cleanup.DanglingOnly && !cfg.Cleanup.All
	if danglingOnly {
		images, err = client.ListDanglingImages(ctx)
	} else {
		images, err = client.ListImages(ctx)
	}
	if err != nil {
		return Report{}, fmt.Errorf("list images for cleanup: %w", err)
	}
	resources := make([]docker.CleanupResource, 0, len(images))
	for _, image := range images {
		resources = append(resources, docker.CleanupResource{
			Kind: docker.CleanupImage, ID: image.ID, Name: strings.Join(image.RepoTags, ","),
			CreatedAt: image.CreatedAt, Size: image.Size, Dangling: image.Dangling,
			Protected: image.Protected,
		})
	}
	return removeResources(ctx, cfg, legacyImageCleaner{client}, logger, now, docker.CleanupImage, resources, Report{}), nil
}

type legacyImageCleaner struct{ docker.Client }

func (c legacyImageCleaner) RemoveCleanupResource(ctx context.Context, resource docker.CleanupResource) error {
	return c.RemoveImage(ctx, resource.ID)
}

func cleanupResourceKind(
	ctx context.Context,
	cfg config.Config,
	cleaner resourceCleaner,
	logger zerolog.Logger,
	now time.Time,
	kind docker.CleanupResourceKind,
	report Report,
) (Report, error) {
	resources, err := cleaner.ListCleanupResources(ctx, kind)
	if err != nil {
		return report, fmt.Errorf("list %s resources: %w", kind, err)
	}
	if kind != docker.CleanupBuildCache || cfg.Updates.DryRun {
		return removeResources(ctx, cfg, cleaner, logger, now, kind, resources, report), nil
	}
	return pruneBuildCache(ctx, cfg, cleaner, logger, now, resources, report)
}

type remover interface {
	RemoveCleanupResource(context.Context, docker.CleanupResource) error
}

func removeResources(
	ctx context.Context,
	cfg config.Config,
	cleaner remover,
	logger zerolog.Logger,
	now time.Time,
	kind docker.CleanupResourceKind,
	resources []docker.CleanupResource,
	report Report,
) Report {
	sortResources(resources)
	for _, resource := range resources {
		if ctx.Err() != nil {
			break
		}
		result := classify(resource, cfg.Cleanup, now)
		if result.Eligible {
			switch {
			case cfg.Updates.DryRun:
				result.WouldRemove = true
			case kind == docker.CleanupBuildCache:
				result.Reason = "awaiting build-cache prune"
			default:
				result.Err = cleaner.RemoveCleanupResource(ctx, resource)
				if result.Err == nil {
					result.Removed = true
					report.ReclaimedBytes += resource.Size
				}
			}
		}
		report.Results = append(report.Results, result)
		if kind != docker.CleanupBuildCache || cfg.Updates.DryRun {
			logResult(logger, result)
		}
	}
	return report
}

func pruneBuildCache(
	ctx context.Context,
	cfg config.Config,
	cleaner resourceCleaner,
	logger zerolog.Logger,
	now time.Time,
	resources []docker.CleanupResource,
	report Report,
) (Report, error) {
	eligible := make(map[string]struct{})
	start := len(report.Results)
	report = removeResources(ctx, cfg, cleaner, logger, now, docker.CleanupBuildCache, resources, report)
	for _, result := range report.Results[start:] {
		if result.Eligible {
			eligible[result.Resource.ID] = struct{}{}
		}
	}
	if len(eligible) == 0 {
		for index := start; index < len(report.Results); index++ {
			logResult(logger, report.Results[index])
		}
		return report, nil
	}
	pruned, err := cleaner.PruneBuildCache(ctx, now.Add(-time.Duration(cfg.Cleanup.MinAgeHours)*time.Hour))
	if err != nil {
		for index := start; index < len(report.Results); index++ {
			if report.Results[index].Eligible {
				report.Results[index].Err = err
				report.Results[index].Reason = ""
			}
			logResult(logger, report.Results[index])
		}
		return report, err
	}
	deleted := make(map[string]struct{}, len(pruned.Deleted))
	for _, id := range pruned.Deleted {
		deleted[id] = struct{}{}
	}
	for index := start; index < len(report.Results); index++ {
		if _, ok := deleted[report.Results[index].Resource.ID]; ok {
			report.Results[index].Removed = true
			report.Results[index].Reason = ""
		} else if report.Results[index].Eligible {
			report.Results[index].Reason = "Docker prune retained the eligible cache record"
		}
		logResult(logger, report.Results[index])
	}
	report.ReclaimedBytes += pruned.ReclaimedBytes
	return report, nil
}

func classify(resource docker.CleanupResource, cfg config.CleanupConfig, now time.Time) Result {
	result := Result{Resource: resource}
	referenceTime := resource.CreatedAt
	if !resource.LastUsedAt.IsZero() {
		referenceTime = resource.LastUsedAt
	}
	result.ReferenceAt = referenceTime
	if !referenceTime.IsZero() {
		result.AgeHours = int64(now.Sub(referenceTime).Hours())
	}
	switch {
	case resource.Protected:
		result.Reason = "resource is protected"
	case resource.InUse:
		result.Reason = "resource is in use"
	case referenceTime.IsZero():
		result.Reason = "resource age is unavailable"
	case now.Sub(referenceTime) < time.Duration(cfg.MinAgeHours)*time.Hour:
		result.Reason = "resource is newer than the minimum age"
	case resource.Kind == docker.CleanupImage && cfg.DanglingOnly && !cfg.All && !resource.Dangling:
		result.Reason = "image is not dangling"
	default:
		result.Eligible = true
	}
	return result
}

func enabledResourceKinds(cfg config.CleanupConfig) []docker.CleanupResourceKind {
	kinds := make([]docker.CleanupResourceKind, 0, 4)
	if cfg.All || cfg.StoppedContainers {
		kinds = append(kinds, docker.CleanupContainer)
	}
	if cfg.All || cfg.UnusedNetworks {
		kinds = append(kinds, docker.CleanupNetwork)
	}
	if cfg.All || cfg.UnusedVolumes {
		kinds = append(kinds, docker.CleanupVolume)
	}
	if cfg.All || cfg.BuildCache {
		kinds = append(kinds, docker.CleanupBuildCache)
	}
	return kinds
}

func sortResources(resources []docker.CleanupResource) {
	sort.Slice(resources, func(i, j int) bool {
		left, right := resources[i].CreatedAt, resources[j].CreatedAt
		if left.Equal(right) {
			return resources[i].ID < resources[j].ID
		}
		return left.Before(right)
	})
}
