package updater

import (
	"context"
	"fmt"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	"github.com/rs/zerolog"
)

var detectCurrentContainer = selfupdate.DetectCurrentContainer

func RunUpdateCycle(ctx context.Context, cfg config.Config, client docker.Client, logger zerolog.Logger) (Report, error) {
	started := time.Now()
	containers, err := client.ListContainers(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("list containers: %w", err)
	}
	recoverStrandedBackups(ctx, client, cfg.Updates.DryRun, logger)

	sortContainers(containers)
	results := newResults(containers)
	selfID := detectCurrentContainer(containers)
	// An active handoff ends this cycle, so it is settled before any pull.
	settledHelper, err := settleActiveHelper(ctx, client, results, selfID, logger)
	if err != nil {
		return finishCycle(logger, results, started, cfg.Updates.DryRun), err
	}
	discoverCandidates(ctx, cfg, client, results, selfID)

	if err := ctx.Err(); err != nil {
		return finishCycle(logger, results, started, cfg.Updates.DryRun), err
	}

	shutdownSignal := processCandidates(ctx, cfg, client, results, selfID, settledHelper, logger)
	return finishCycle(logger, results, started, cfg.Updates.DryRun), shutdownSignal
}

func finishCycle(logger zerolog.Logger, results []ContainerResult, started time.Time, dryRun bool) Report {
	report := Report{Results: results, Duration: time.Since(started)}
	logReport(logger, report, dryRun)
	return report
}
