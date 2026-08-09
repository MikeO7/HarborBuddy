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

	sortContainers(containers)
	results := newResults(containers)
	selfID := detectCurrentContainer(containers)
	discoverCandidates(ctx, cfg.Updates, client, results, selfID)

	if err := ctx.Err(); err != nil {
		return finishCycle(logger, results, started, cfg.Updates.DryRun), err
	}

	shutdownSignal := processCandidates(ctx, cfg, client, results, selfID)
	return finishCycle(logger, results, started, cfg.Updates.DryRun), shutdownSignal
}

func finishCycle(logger zerolog.Logger, results []ContainerResult, started time.Time, dryRun bool) Report {
	report := Report{Results: results, Duration: time.Since(started)}
	logReport(logger, report, dryRun)
	return report
}
