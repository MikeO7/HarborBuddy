package scheduler

import (
	"context"
	"errors"
	"fmt"

	"github.com/MikeO7/HarborBuddy/internal/cleanup"
	"github.com/MikeO7/HarborBuddy/internal/updater"
	"github.com/rs/zerolog"
)

// partialFailureError reports a cycle that ran to the end while individual
// containers or resources failed. The daemon keeps running; a one-shot run
// exits non-zero.
type partialFailureError struct{ message string }

func (e *partialFailureError) Error() string { return e.message }

// cycleCompletion picks the log level and outcome for a finished cycle.
func cycleCompletion(ctx context.Context, logger zerolog.Logger, err error, partial bool) (*zerolog.Event, string) {
	switch {
	case ctx.Err() != nil:
		return logger.Info(), "cancelled"
	case err == nil:
		return logger.Info(), "completed"
	case partial:
		return logger.Warn().Err(err), "partial_failure"
	default:
		return logger.Error().Err(err), "failed"
	}
}

// onlyPartialFailures reports whether every error is a partialFailureError.
func onlyPartialFailures(errs ...error) bool {
	for _, err := range errs {
		var partial *partialFailureError
		if !errors.As(err, &partial) {
			return false
		}
	}
	return true
}

// cleanupOptionsFrom keeps containers whose replacement failed or was rolled
// back this cycle away from stopped-container cleanup: a failed rollback can
// leave the user's original container stopped. A failure before the
// replacement began, such as a failed pull, never touched the container.
func cleanupOptionsFrom(report updater.Report) cleanup.Options {
	var options cleanup.Options
	for _, result := range report.Results {
		if result.RollbackTried || (result.Status == updater.StatusFailed && result.FailureStage != "") {
			options.ProtectedContainerIDs = append(options.ProtectedContainerIDs, result.Container.ID)
		}
	}
	return options
}

// updatePhaseError turns the update phase's result into the cycle's error: a
// phase that could not run is a failure, and one that ran while individual
// containers failed is a partial failure.
func updatePhaseError(report updater.Report, err error) error {
	if err != nil {
		return fmt.Errorf("update cycle: %w", err)
	}
	if failed := report.Count(updater.StatusFailed); failed > 0 {
		return &partialFailureError{message: fmt.Sprintf("%d container update(s) failed", failed)}
	}
	return nil
}
