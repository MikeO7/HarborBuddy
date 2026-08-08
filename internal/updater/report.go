package updater

import (
	"strings"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/rs/zerolog"
)

type Status string

const (
	StatusExcluded          Status = "excluded"
	StatusCurrent           Status = "current"
	StatusWouldUpdate       Status = "would_update"
	StatusUpdated           Status = "updated"
	StatusSelfUpdateStarted Status = "self_update_started"
	StatusUnsupported       Status = "unsupported"
	StatusFailed            Status = "failed"
	StatusCancelled         Status = "cancelled"
	StatusChangedExternally Status = "changed_externally"
)

type ContainerResult struct {
	Container     docker.ContainerSummary
	Status        Status
	Reason        string
	TargetImageID string
	Err           error
	Warning       error
}

type Report struct {
	Results  []ContainerResult
	Duration time.Duration
}

func (r Report) Count(status Status) int {
	count := 0
	for _, result := range r.Results {
		if result.Status == status {
			count++
		}
	}
	return count
}

func logReport(logger zerolog.Logger, report Report, dryRun bool) {
	for _, result := range report.Results {
		logResult(logger, result, dryRun)
	}
	logger.Info().
		Int("total", len(report.Results)).
		Int("updated", report.Count(StatusUpdated)).
		Int("self_update_started", report.Count(StatusSelfUpdateStarted)).
		Int("would_update", report.Count(StatusWouldUpdate)).
		Int("failed", report.Count(StatusFailed)).
		Int64("duration_ms", report.Duration.Milliseconds()).
		Msg("Update cycle complete")
}

func logResult(logger zerolog.Logger, result ContainerResult, dryRun bool) {
	event := logger.Info().
		Str("container_id", shortID(result.Container.ID)).
		Str("container_name", result.Container.Name).
		Str("image_ref", result.Container.ImageRef).
		Str("result", string(result.Status))
	if result.TargetImageID != "" {
		event = event.Str("target_image_id", shortID(result.TargetImageID))
	}
	if result.Err != nil {
		event = event.Err(result.Err)
	}
	if result.Warning != nil {
		event = event.Str("warning", result.Warning.Error())
	}
	event.Msg(resultMessage(result, dryRun))
}

func resultMessage(result ContainerResult, dryRun bool) string {
	switch result.Status {
	case StatusExcluded:
		return "Container excluded: " + result.Reason
	case StatusCurrent:
		return "Container image is current"
	case StatusWouldUpdate:
		if dryRun {
			return "Container would be updated"
		}
		return "Container update is available"
	case StatusUpdated:
		return "Container updated"
	case StatusSelfUpdateStarted:
		return "Self-update helper started; HarborBuddy will shut down gracefully"
	case StatusUnsupported:
		return "Container cannot be updated safely: " + result.Reason
	case StatusFailed:
		return "Container update failed"
	case StatusCancelled:
		return "Container check cancelled"
	case StatusChangedExternally:
		return "Container changed during the cycle; skipping"
	default:
		return "Container update result is invalid"
	}
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
