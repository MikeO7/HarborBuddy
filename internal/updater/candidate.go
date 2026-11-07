package updater

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	"github.com/rs/zerolog"
)

func processCandidates(
	ctx context.Context,
	cfg config.Config,
	client docker.Client,
	results []ContainerResult,
	selfID string,
	logger zerolog.Logger,
) error {
	settledHelper, err := settleActiveHelper(ctx, client, results, selfID, logger)
	if err != nil {
		return err
	}
	history := loadHelperHistory(ctx, cfg, client, selfID, logger)
	markFinished(history, settledHelper)
	selfIndex := -1
	for index := range results {
		if selfID != "" && results[index].Container.ID == selfID {
			selfIndex = index
			continue
		}
		_ = processCandidate(ctx, cfg, client, &results[index], false)
	}
	if selfIndex < 0 || skipSelfCandidate(cfg, &results[selfIndex], history, logger) {
		return nil
	}
	return processCandidate(ctx, cfg, client, &results[selfIndex], true)
}

func processCandidate(ctx context.Context, cfg config.Config, client docker.Client, result *ContainerResult, isSelf bool) error {
	if result.Status != StatusWouldUpdate {
		return nil
	}
	if skipBeforeTransaction(ctx, result, isSelf) {
		return nil
	}

	details, ok := reinspectCandidate(ctx, client, result)
	if !ok || !checkCandidateReplacement(ctx, client, details, result) || cfg.Updates.DryRun {
		return nil
	}
	if isSelf {
		return processSelfCandidate(ctx, cfg, client, details, result)
	}
	processOrdinaryCandidate(ctx, cfg, client, details, result)
	return nil
}

// skipBeforeTransaction classifies candidates that must not start a
// replacement: shutdown stops new transactions (one already running finishes),
// and a target image that already failed readiness is not retried.
func skipBeforeTransaction(ctx context.Context, result *ContainerResult, isSelf bool) bool {
	switch {
	case ctx.Err() != nil:
		result.Status = StatusCancelled
		result.Reason = "shutdown requested before the update started"
	case !isSelf && failedTargets.failed(result.Container.Name, result.TargetImageID):
		result.Status = StatusPreviouslyFailed
		result.Reason = "a replacement from this target image previously failed readiness and was rolled back; waiting for a different image"
	default:
		return false
	}
	return true
}

func reinspectCandidate(ctx context.Context, client docker.Client, result *ContainerResult) (docker.ContainerDetails, bool) {
	details, err := client.InspectContainer(ctx, result.Container.ID)
	if err != nil {
		result.Status = StatusFailed
		result.Err = fmt.Errorf("inspect before update: %w", err)
		return docker.ContainerDetails{}, false
	}
	if candidateUnchanged(details, result.Container) {
		return details, true
	}
	result.Status = StatusChangedExternally
	result.Reason = "container changed after update discovery"
	return docker.ContainerDetails{}, false
}

func candidateUnchanged(details docker.ContainerDetails, discovered docker.ContainerSummary) bool {
	return details.State != nil &&
		details.State.Running &&
		details.Summary.ImageID == discovered.ImageID &&
		details.Summary.ImageRef == discovered.ImageRef
}

func checkCandidateReplacement(ctx context.Context, client docker.Client, details docker.ContainerDetails, result *ContainerResult) bool {
	err := client.CheckReplacement(details, candidateTarget(result))
	if checker, ok := client.(docker.NamespaceDependentsChecker); ok && err == nil {
		err = checker.CheckNamespaceDependents(ctx, details)
	}
	if err == nil {
		return true
	}
	if docker.IsUnsupported(err) {
		result.Status = StatusUnsupported
		result.Reason = err.Error()
	} else {
		result.Status = StatusFailed
		result.Err = err
	}
	return false
}

func processSelfCandidate(
	ctx context.Context,
	cfg config.Config,
	client docker.Client,
	details docker.ContainerDetails,
	result *ContainerResult,
) error {
	starter, ok := client.(selfupdate.HelperStarter)
	if !ok {
		result.Status = StatusFailed
		result.Err = fmt.Errorf("docker client %T cannot start a self-update helper", client)
		return nil
	}

	err := selfupdate.Trigger(ctx, starter, details, candidateTarget(result), selfupdate.TriggerOptions{
		DockerHost:             cfg.Docker.Host,
		StopTimeout:            cfg.Updates.StopTimeout,
		StartupTimeout:         cfg.Updates.StartupTimeout,
		RollbackImageRetention: cfg.Updates.RollbackImageRetention,
	})
	if signal, ok := selfupdate.AsShutdownRequired(err); ok {
		result.Status = StatusSelfUpdateStarted
		result.HelperID = signal.HelperContainerID
		return err
	}
	result.Status = StatusFailed
	result.Err = err
	return nil
}

func processOrdinaryCandidate(
	ctx context.Context,
	cfg config.Config,
	client docker.Client,
	details docker.ContainerDetails,
	result *ContainerResult,
) {
	replaced, err := client.ReplaceContainer(ctx, details, candidateTarget(result), docker.ReplaceOptions{
		StopTimeout:            cfg.Updates.StopTimeout,
		StartupTimeout:         cfg.Updates.StartupTimeout,
		StabilizationTime:      2 * time.Second,
		RollbackImageRetention: cfg.Updates.RollbackImageRetention,
	})
	result.FailureStage = replaced.FailureStage
	result.RollbackTried = replaced.RollbackAttempted
	result.RollbackErr = replaced.RollbackErr
	result.Warning = errors.Join(replaced.ConfigWarning, replaced.BackupCleanupErr, replaced.RollbackImageRetentionErr)
	failedTargets.observe(result.Container.Name, result.TargetImageID, replaced, err)
	switch {
	case err != nil && replaced.FailureStage == docker.FailureStageCancelledBeforeStart:
		result.Status = StatusCancelled
		result.Reason = "shutdown requested before the update started"
	case err != nil:
		result.Status = StatusFailed
		result.Err = err
	default:
		result.Status = StatusUpdated
	}
}

func candidateTarget(result *ContainerResult) docker.ImageInfo {
	return docker.ImageInfo{ID: result.TargetImageID}
}
