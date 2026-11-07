package selfupdate

import (
	"context"
	"errors"
	"fmt"

	"github.com/MikeO7/HarborBuddy/internal/docker"
	containertypes "github.com/moby/moby/api/types/container"
)

// TargetRecoverer restores a daemon that handed off to a helper which then
// failed: it reinstates the original restart policy and starts the daemon.
type TargetRecoverer interface {
	RecoverSelfUpdateTarget(context.Context, string, containertypes.RestartPolicy) error
}

// HandoffError reports a failed helper run together with the outcome of the
// attempt to bring the original daemon back.
type HandoffError struct {
	Err         error
	RecoveryErr error
}

func (e *HandoffError) Error() string {
	if e.RecoveryErr != nil {
		return fmt.Sprintf("%v; recovering the original HarborBuddy container failed: %v", e.Err, e.RecoveryErr)
	}
	return fmt.Sprintf("%v; the original HarborBuddy container was restored", e.Err)
}

func (e *HandoffError) Unwrap() []error {
	if e.RecoveryErr == nil {
		return []error{e.Err}
	}
	return []error{e.Err, e.RecoveryErr}
}

// RecoveryOutcome classifies an error returned by RunUpdater for logging.
func RecoveryOutcome(err error) string {
	var handoff *HandoffError
	switch {
	case !errors.As(err, &handoff):
		return "not_attempted"
	case handoff.RecoveryErr != nil:
		return "failed"
	default:
		return "succeeded"
	}
}

func recoverTarget(ctx context.Context, client docker.Client, request UpdaterRequest) error {
	recoverer, ok := client.(TargetRecoverer)
	if !ok {
		return fmt.Errorf("docker client %T cannot restore the original HarborBuddy container", client)
	}
	return recoverer.RecoverSelfUpdateTarget(ctx, request.TargetContainerID, request.RestartPolicy)
}
