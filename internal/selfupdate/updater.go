package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/docker"
)

const helperExitTimeout = 5 * time.Minute

// HelperStarter is implemented by Docker clients that can launch the
// short-lived, auto-removed self-update helper.
type HelperStarter interface {
	StartSelfUpdateHelper(context.Context, docker.ContainerDetails, docker.SelfUpdateHelperRequest) (string, error)
}

// ContainerExitWaiter is the self-update-specific Docker wait operation used
// inside helper mode.
type ContainerExitWaiter interface {
	WaitContainerExit(context.Context, string) error
}

// TriggerOptions contains the replacement policy that must survive the handoff
// from the daemon to the short-lived helper.
type TriggerOptions struct {
	DockerHost     string
	StopTimeout    time.Duration
	StartupTimeout time.Duration
}

// UpdaterRequest identifies the stopped daemon and the replacement policy the
// helper must use. Keeping this as one value avoids positional helper arguments
// drifting apart as the self-update protocol evolves.
type UpdaterRequest struct {
	TargetContainerID string
	TargetImageID     string
	StopTimeout       time.Duration
	StartupTimeout    time.Duration
}

// ShutdownRequiredError signals that the helper is running and the current
// HarborBuddy process should complete a normal, successful shutdown.
type ShutdownRequiredError struct {
	TargetContainerID string
	HelperContainerID string
}

func (e *ShutdownRequiredError) Error() string {
	return fmt.Sprintf("self-update helper %s started for container %s; graceful shutdown required", e.HelperContainerID, e.TargetContainerID)
}

// AsShutdownRequired returns the typed self-update signal, including when it is
// wrapped by scheduler or application code.
func AsShutdownRequired(err error) (*ShutdownRequiredError, bool) {
	var signal *ShutdownRequiredError
	ok := errors.As(err, &signal)
	return signal, ok
}

// Trigger starts the helper from the pulled target image. It never terminates
// the current process; callers must treat the returned typed error as success
// and shut down normally.
func Trigger(ctx context.Context, client HelperStarter, current docker.ContainerDetails, target docker.ImageInfo, options TriggerOptions) error {
	if current.Summary.ID == "" || current.Summary.Name == "" {
		return errors.New("start self-update helper: current container identity is incomplete")
	}
	if target.ID == "" {
		return errors.New("start self-update helper: target image identity is missing")
	}

	request := docker.SelfUpdateHelperRequest{
		Name:              fmt.Sprintf("%s-harborbuddy-updater-%d", current.Summary.Name, time.Now().UnixNano()),
		TargetContainerID: current.Summary.ID,
		TargetImageID:     target.ID,
		DockerHost:        options.DockerHost,
		StopTimeout:       options.StopTimeout,
		StartupTimeout:    options.StartupTimeout,
	}
	helperID, err := client.StartSelfUpdateHelper(ctx, current, request)
	if err != nil {
		return fmt.Errorf("start self-update helper for %s: %w", current.Summary.ID, err)
	}
	if helperID == "" {
		return fmt.Errorf("start self-update helper for %s: Docker returned an empty helper ID", current.Summary.ID)
	}
	return &ShutdownRequiredError{TargetContainerID: current.Summary.ID, HelperContainerID: helperID}
}

// RunUpdater is the entry point for helper mode. It waits for the original
// HarborBuddy process to exit, re-inspects the target, and delegates replacement
// to the same transactional path used for ordinary containers.
func RunUpdater(ctx context.Context, client docker.Client, request UpdaterRequest) error {
	if request.TargetContainerID == "" || request.TargetImageID == "" {
		return errors.New("self-update helper requires target container and image IDs")
	}
	waiter, ok := client.(ContainerExitWaiter)
	if !ok {
		return fmt.Errorf("docker client %T cannot wait for the target container to exit", client)
	}
	waitCtx, cancel := context.WithTimeout(ctx, helperExitTimeout)
	defer cancel()
	if err := waiter.WaitContainerExit(waitCtx, request.TargetContainerID); err != nil {
		return fmt.Errorf("wait for HarborBuddy container to exit: %w", err)
	}

	current, err := client.InspectContainer(ctx, request.TargetContainerID)
	if err != nil {
		return fmt.Errorf("inspect HarborBuddy container after exit: %w", err)
	}
	target := docker.ImageInfo{ID: request.TargetImageID}
	options := docker.ReplaceOptions{
		StopTimeout:           request.StopTimeout,
		StartupTimeout:        request.StartupTimeout,
		CurrentAlreadyStopped: current.State != nil && !current.State.Running,
	}
	result, err := client.ReplaceContainer(ctx, current, target, options)
	if err != nil {
		return fmt.Errorf("transactionally replace HarborBuddy container: %w", err)
	}
	// Backup cleanup failures are warnings for ordinary updates and remain
	// non-fatal here so a healthy replacement is not reported as failed.
	_ = result.BackupCleanupErr
	return nil
}
