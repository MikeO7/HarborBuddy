package updater

import (
	"context"

	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/rs/zerolog"
)

// recoverStrandedBackups restores originals that an interrupted replacement
// (for example a host shutdown that outlasted the stop grace period) left
// stopped under their backup name. Recovered containers are not updated in the
// same cycle; the next cycle treats them like any other running container.
func recoverStrandedBackups(ctx context.Context, client docker.Client, dryRun bool, logger zerolog.Logger) {
	recoverer, ok := client.(docker.BackupRecoverer)
	if !ok {
		return
	}
	recoveries, err := recoverer.RecoverStrandedBackups(ctx, dryRun)
	if err != nil {
		logger.Error().Str("event", "backup_recovery_failed").Err(err).Msg("Could not check for stranded backup containers")
		return
	}
	for _, recovery := range recoveries {
		logRecovery(logger, recovery)
	}
}

func logRecovery(logger zerolog.Logger, recovery docker.BackupRecovery) {
	event, message := logger.Warn(), "HarborBuddy backup container left in place"
	switch {
	case recovery.Err != nil:
		event, message = logger.Error().Err(recovery.Err), "Stranded backup container could not be fully restored"
	case recovery.Restored:
		message = "Restored container left stopped by an interrupted update"
	case recovery.Deferred:
		event = logger.Debug()
	}
	event = event.Str("event", "backup_recovery").
		Str("container_id", shortID(recovery.ContainerID)).
		Str("backup_name", recovery.BackupName).
		Str("container_name", recovery.OriginalName).
		Bool("restored", recovery.Restored)
	if recovery.Reason != "" {
		event = event.Str("reason", recovery.Reason)
	}
	if recovery.Warning != nil {
		event = event.Str("warning", recovery.Warning.Error())
	}
	event.Msg(message)
}
