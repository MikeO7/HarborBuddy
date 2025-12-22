package selfupdate

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/pkg/log"
)

// ExitFunc is the function called to exit the process. It can be overridden in tests.
var ExitFunc = os.Exit

// RunUpdater is the entrypoint for the temporary helper container
func RunUpdater(ctx context.Context, client docker.Client, targetID string, newImage string) error {
	log.Info("Updater: 🔄 Started. Waiting for target to stop...")

	// 1. Wait for the target container to stop
	// We give it a generous timeout to shut down gracefully
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	targetStopped := false
	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timeout waiting for target %s to stop", targetID)
		case <-ticker.C:
			info, err := client.InspectContainer(ctx, targetID)
			if err != nil {
				// If error is "Not Found", it's already gone (maybe manual rm?), which is fine-ish
				// but usually we expect it to exist but be stopped.
				// For now, let's assume if we can't inspect it, we can't copy its config, so that's a fatal error
				// UNLESS we passed the config to the helper.
				// However, the plan is to inspect it to get the config.
				// So the target must exist.
				log.ErrorErr("Updater: Failed to inspect target", err)
				return err
			}

			if !info.State.Running {
				targetStopped = true
				break
			}
			log.Debugf("Updater: Target %s is still running...", targetID)
		}
		if targetStopped {
			break
		}
	}

	log.Info("Updater: Target stopped. Inspecting configuration...")

	// 2. Inspect to get config for recreation
	// Note: We inspect AFTER it stops to get the final state, although config shouldn't change much.
	oldContainer, err := client.InspectContainer(ctx, targetID)
	if err != nil {
		return fmt.Errorf("failed to inspect stopped target: %w", err)
	}

	// 3. Remove the old container
	log.Info("Updater: Removing old container...")
	if err := client.RemoveContainer(ctx, targetID); err != nil {
		return fmt.Errorf("failed to remove old container: %w", err)
	}

	// 4. Create the new container
	log.Info("Updater: Creating new container...")
	// We use the same name as the old one (which is now free since we removed it)
	// CreateContainerLike needs to be slightly smarter to handle "use the old name"
	// Currently CreateContainerLike creates with "tempName".
	// We might need to adjust CreateContainerLike or do a rename.
	// Let's check docker/containers.go... it creates with `old.Name + "-new"`.
	// We want the EXACT same name.

	// We will use a modified approach here or add a param to CreateContainerLike.
	// For now, let's assume we use CreateContainerLike and then Rename.

	tempID, err := client.CreateContainerLike(ctx, oldContainer, newImage)
	if err != nil {
		return fmt.Errorf("failed to create new container: %w", err)
	}

	// Rename tempID to oldContainer.Name
	// But wait, CreateContainerLike creates "Name-new".
	// We want "Name".
	// Since "Name" is free (we removed oldContainer), we can rename immediately.

	log.Info("Updater: Renaming new container to original name...")
	if err := client.RenameContainer(ctx, tempID, oldContainer.Name); err != nil {
		// Try to remove the temp one if rename fails
		_ = client.RemoveContainer(ctx, tempID)
		return fmt.Errorf("failed to rename new container: %w", err)
	}

	// 5. Start the new container
	log.Info("Updater: 🚀 Starting new container...")
	if err := client.StartContainer(ctx, tempID); err != nil {
		return fmt.Errorf("failed to start new container: %w", err)
	}

	log.Info("Updater: ✅ Update complete. Exiting.")
	return nil
}

// Trigger starts the update process
func Trigger(ctx context.Context, client docker.Client, myContainer docker.ContainerInfo, newImage string) error {
	log.Info("Self-Update: Triggering helper process...")

	// We need to spawn a container that runs:
	// /app/harborbuddy --updater-mode --target-container-id <myID> --new-image-id <newImage>

	// We reuse the current configuration for the helper, but we need to ensure it has:
	// 1. Docker socket mounted
	// 2. The same image (or the NEW image, which we have pulled)

	// Ideally, the helper uses the NEW image. We already pulled it.

	// Override entrypoint/cmd
	cmd := []string{
		"/app/harborbuddy", // Assuming binary path, need to verify
		"--updater-mode",
		"--target-container-id", myContainer.ID,
		"--new-image-id", newImage,
	}

	// Create the helper container
	// We need a specialized create function or use the raw client, but we are in `internal`.
	// Let's add a method to `docker.Client` to spawn a helper.
	// Or we can construct a ContainerInfo and use CreateContainerLike?
	// No, CreateContainerLike clones the *target's* config (ports, envs, etc).
	// The helper doesn't need ports, just the socket.

	// For simplicity, let's assume the helper is a clone of the current container
	// (so it has the socket mount) but with overridden CMD/Entrypoint.
	// This is safe because the helper is short-lived.

	helperName := fmt.Sprintf("%s-updater-%d", myContainer.Name, time.Now().Unix())

	helperID, err := client.CreateHelperContainer(ctx, myContainer, newImage, helperName, cmd)
	if err != nil {
		return fmt.Errorf("failed to create helper: %w", err)
	}

	log.Infof("Self-Update: 🚀 Helper %s created. Starting...", helperID)

	if err := client.StartContainer(ctx, helperID); err != nil {
		return fmt.Errorf("failed to start helper: %w", err)
	}

	log.Info("Self-Update: 🔄 Helper started. Shutting down self to allow update to proceed.")

	// We exit successfully. The helper is waiting for us to stop.
	ExitFunc(0)

	return nil
}
