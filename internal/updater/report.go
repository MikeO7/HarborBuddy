package updater

import (
	"strings"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/docker"
	operational "github.com/MikeO7/HarborBuddy/internal/logging"
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
	HelperID      string
	FailureStage  string
	RollbackTried bool
	RollbackErr   error
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

func (r Report) WarningCount() int {
	count := 0
	for _, result := range r.Results {
		if result.Warning != nil {
			count++
		}
	}
	return count
}

func logReport(logger zerolog.Logger, report Report, dryRun bool) {
	for _, result := range report.Results {
		logResult(logger, result, dryRun)
	}
	event := logger.Info()
	if report.Count(StatusFailed) > 0 {
		event = logger.Error()
	} else if report.WarningCount()+report.Count(StatusUnsupported)+report.Count(StatusCancelled)+report.Count(StatusChangedExternally) > 0 {
		event = logger.Warn()
	}
	event.
		Str("event", "update_complete").
		Str("outcome", updateOutcome(report)).
		Int("total", len(report.Results)).
		Int("current", report.Count(StatusCurrent)).
		Int("excluded", report.Count(StatusExcluded)).
		Int("updated", report.Count(StatusUpdated)).
		Int("self_update_started", report.Count(StatusSelfUpdateStarted)).
		Int("would_update", report.Count(StatusWouldUpdate)).
		Int("unsupported", report.Count(StatusUnsupported)).
		Int("failed", report.Count(StatusFailed)).
		Int("warnings", report.WarningCount()).
		Int("cancelled", report.Count(StatusCancelled)).
		Int("changed_externally", report.Count(StatusChangedExternally)).
		Int64("duration_ms", report.Duration.Milliseconds()).
		Bool("dry_run", dryRun).
		Msg("Update cycle complete")
}

func logResult(logger zerolog.Logger, result ContainerResult, dryRun bool) {
	imageRef, imageRefTruncated := operational.BoundedField(result.Container.ImageRef)
	event := resultEvent(logger, result).
		Str("event", "container_update_result").
		Str("container_id", shortID(result.Container.ID)).
		Str("container_name", result.Container.Name).
		Str("image_ref", imageRef).
		Bool("image_ref_truncated", imageRefTruncated).
		Str("current_image_id", shortID(result.Container.ImageID)).
		Str("result", string(result.Status))
	if result.TargetImageID != "" {
		event = event.Str("target_image_id", shortID(result.TargetImageID))
		event = event.Str("transaction_id", transactionID(result))
	}
	if result.HelperID != "" {
		event = event.Str("helper_container_id", shortID(result.HelperID))
	}
	if result.Reason != "" {
		event = event.Str("reason", result.Reason)
	}
	if result.FailureStage != "" {
		event = event.Str("failure_stage", result.FailureStage)
	}
	if result.RollbackTried {
		outcome := "succeeded"
		if result.RollbackErr != nil {
			outcome = "failed"
			event = event.Str("rollback_error", result.RollbackErr.Error())
		}
		event = event.Bool("rollback_attempted", true).Str("rollback_outcome", outcome)
	}
	if result.Err != nil {
		event = event.Err(result.Err)
	}
	if result.Warning != nil {
		event = event.Str("warning", result.Warning.Error())
	}
	event.Msg(resultMessage(result, dryRun))
}

func resultEvent(logger zerolog.Logger, result ContainerResult) *zerolog.Event {
	switch {
	case result.Status == StatusFailed:
		return logger.Error()
	case result.Warning != nil || result.Status == StatusUnsupported || result.Status == StatusCancelled || result.Status == StatusChangedExternally:
		return logger.Warn()
	case result.Status == StatusCurrent || result.Status == StatusExcluded:
		return logger.Debug()
	default:
		return logger.Info()
	}
}

func updateOutcome(report Report) string {
	switch {
	case report.Count(StatusFailed) > 0:
		return "partial_failure"
	case report.Count(StatusCancelled) > 0:
		return "cancelled"
	case report.Count(StatusSelfUpdateStarted) > 0:
		return "self_update_handoff"
	default:
		return "success"
	}
}

func transactionID(result ContainerResult) string {
	if result.TargetImageID == "" {
		return ""
	}
	return shortID(result.Container.ID) + "-" + shortID(result.TargetImageID)
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
