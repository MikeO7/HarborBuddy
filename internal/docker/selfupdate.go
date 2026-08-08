package docker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	containertypes "github.com/docker/docker/api/types/container"
)

const (
	selfUpdateBinary            = "/harborbuddy"
	helperLabelKey              = "com.harborbuddy.self-update-helper"
	SelfUpdateHelperReadyMarker = "HARBORBUDDY_SELF_UPDATE_HELPER_READY"
)

// SelfUpdateHelperRequest describes the short-lived helper that replaces the
// currently running HarborBuddy container after its process exits.
type SelfUpdateHelperRequest struct {
	Name              string
	TargetContainerID string
	TargetImageID     string
	DockerHost        string
	StopTimeout       time.Duration
	StartupTimeout    time.Duration
}

// StartSelfUpdateHelper creates and starts an auto-removed helper from the
// already-pulled target image. It intentionally copies only Docker connection
// state from the current HarborBuddy container.
func (d *DockerClient) StartSelfUpdateHelper(ctx context.Context, current ContainerDetails, request SelfUpdateHelperRequest) (string, error) {
	containerConfig, hostConfig, networkConfig, err := selfUpdateHelperConfig(current, request)
	if err != nil {
		return "", err
	}

	created, err := d.cli.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, request.Name)
	if err != nil {
		return "", fmt.Errorf("create self-update helper: %w", err)
	}
	if err := d.cli.ContainerStart(ctx, created.ID, containertypes.StartOptions{}); err != nil {
		cleanupErr := d.removeFailedHelper(ctx, created.ID)
		return "", errors.Join(fmt.Errorf("start self-update helper %s: %w", created.ID, err), cleanupErr)
	}
	if err := d.waitSelfUpdateHelperReady(ctx, created.ID); err != nil {
		cleanupErr := d.removeFailedHelper(ctx, created.ID)
		return "", errors.Join(fmt.Errorf("self-update helper %s failed readiness: %w", created.ID, err), cleanupErr)
	}
	return created.ID, nil
}

func (d *DockerClient) waitSelfUpdateHelperReady(ctx context.Context, helperID string) (waitErr error) {
	readyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	logs, err := d.cli.ContainerLogs(readyCtx, helperID, containertypes.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       "all",
	})
	if err != nil {
		return fmt.Errorf("open helper logs: %w", err)
	}
	defer func() {
		if err := logs.Close(); err != nil && waitErr == nil {
			waitErr = fmt.Errorf("close helper logs: %w", err)
		}
	}()

	scanner := bufio.NewScanner(logs)
	for scanner.Scan() {
		if !strings.Contains(scanner.Text(), SelfUpdateHelperReadyMarker) {
			continue
		}
		return d.verifyHelperRunning(readyCtx, helperID)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read helper readiness logs: %w", err)
	}
	if err := readyCtx.Err(); err != nil {
		return err
	}
	return errors.New("helper exited before acknowledging readiness")
}

func (d *DockerClient) verifyHelperRunning(ctx context.Context, helperID string) error {
	details, err := d.InspectContainer(ctx, helperID)
	if err != nil {
		return err
	}
	if details.State == nil || !details.State.Running || details.State.Restarting || details.State.Dead {
		return errors.New("helper acknowledged readiness but is not running stably")
	}
	return nil
}

func (d *DockerClient) removeFailedHelper(parent context.Context, helperID string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), 15*time.Second)
	defer cancel()
	if err := d.cli.ContainerRemove(cleanupCtx, helperID, containertypes.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove failed helper %s: %w", helperID, err)
	}
	return nil
}

// WaitContainerExit waits for the target container process to stop. Docker's
// wait API observes the exit event even when a restart policy starts it again.
func (d *DockerClient) WaitContainerExit(ctx context.Context, containerID string) error {
	resultCh, errCh := d.cli.ContainerWait(ctx, containerID, containertypes.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("wait for container %s to exit: %w", containerID, err)
		}
		return nil
	case result := <-resultCh:
		if result.Error != nil {
			return fmt.Errorf("wait for container %s to exit: %s", containerID, result.Error.Message)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for container %s to exit: %w", containerID, ctx.Err())
	}
}
