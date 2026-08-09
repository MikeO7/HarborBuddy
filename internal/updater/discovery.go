package updater

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
)

const maxConcurrentPulls = 5

func sortContainers(containers []docker.ContainerSummary) {
	sort.Slice(containers, func(i, j int) bool {
		if containers[i].Name == containers[j].Name {
			return containers[i].ID < containers[j].ID
		}
		return containers[i].Name < containers[j].Name
	})
}

func newResults(containers []docker.ContainerSummary) []ContainerResult {
	results := make([]ContainerResult, len(containers))
	for index, container := range containers {
		results[index].Container = container
	}
	return results
}

func discoverCandidates(
	ctx context.Context,
	cfg config.UpdatesConfig,
	client docker.Client,
	results []ContainerResult,
	selfID string,
) {
	cache := NewSafePullCache()
	semaphore := make(chan struct{}, maxConcurrentPulls)
	var wait sync.WaitGroup

	for index := range results {
		if excludeCandidate(cfg, &results[index], selfID) {
			continue
		}
		wait.Add(1)
		go discoverCandidate(ctx, client, cache, semaphore, &wait, &results[index])
	}
	wait.Wait()
}

func excludeCandidate(cfg config.UpdatesConfig, result *ContainerResult, selfID string) bool {
	isSelf := selfID != "" && result.Container.ID == selfID
	switch {
	case result.Container.Labels[RoleLabel] == DaemonRole && !isSelf:
		result.Status = StatusExcluded
		result.Reason = "HarborBuddy daemon identity is not the current process"
		return true
	case isSelf && !cfg.SelfUpdate:
		result.Status = StatusExcluded
		result.Reason = "self-update is disabled"
		return true
	}

	decision := DetermineEligibility(result.Container, cfg)
	if decision.Eligible {
		return false
	}
	result.Status = StatusExcluded
	result.Reason = decision.Reason
	return true
}

func discoverCandidate(
	ctx context.Context,
	client docker.Client,
	cache *SafePullCache,
	semaphore chan struct{},
	wait *sync.WaitGroup,
	result *ContainerResult,
) {
	defer wait.Done()
	image, err, _ := cache.GetOrPull(ctx, result.Container.ImageRef, func() (docker.ImageInfo, error) {
		return pullImage(ctx, client, semaphore, result.Container.ImageRef)
	})
	if err != nil {
		setPullError(ctx, result, err)
		return
	}

	result.TargetImageID = image.ID
	if image.ID == result.Container.ImageID {
		result.Status = StatusCurrent
		result.Reason = "already using the pulled image"
		return
	}
	result.Status = StatusWouldUpdate
}

func pullImage(ctx context.Context, client docker.Client, semaphore chan struct{}, imageRef string) (docker.ImageInfo, error) {
	select {
	case semaphore <- struct{}{}:
		defer func() { <-semaphore }()
	case <-ctx.Done():
		return docker.ImageInfo{}, ctx.Err()
	}
	return client.PullImage(ctx, imageRef)
}

func setPullError(ctx context.Context, result *ContainerResult, err error) {
	if ctx.Err() != nil {
		result.Status = StatusCancelled
	} else {
		result.Status = StatusFailed
	}
	result.Err = fmt.Errorf("pull %s: %w", result.Container.ImageRef, err)
}
