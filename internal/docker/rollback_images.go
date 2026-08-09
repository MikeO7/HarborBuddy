package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

const rollbackImageRepositoryPrefix = "localhost/harborbuddy-rollback/"

var ErrRollbackImageInUse = errors.New("rollback image is used by a container")

func rollbackImageRepository(imageRef string) string {
	digest := sha256.Sum256([]byte(imageRef))
	return rollbackImageRepositoryPrefix + hex.EncodeToString(digest[:])
}

func (d *DockerClient) retainRollbackImage(ctx context.Context, imageRef, imageID string, retention int) error {
	if retention <= 0 {
		return nil
	}
	repository := rollbackImageRepository(imageRef)
	var warnings []error

	if overflow, ok, err := d.rollbackSlotImage(ctx, repository, retention); err != nil {
		warnings = append(warnings, err)
	} else if ok {
		inUse, checkErr := d.imageUsedByAnyContainer(ctx, overflow)
		if checkErr != nil {
			warnings = append(warnings, checkErr)
		} else if inUse {
			warnings = append(warnings, fmt.Errorf("%w: preserve %s", ErrRollbackImageInUse, overflow))
		} else if _, removeErr := d.cli.ImageRemove(ctx, rollbackSlot(repository, retention), client.ImageRemoveOptions{Force: false, PruneChildren: true}); removeErr != nil {
			warnings = append(warnings, fmt.Errorf("clean rollback slot %d: %w", retention, removeErr))
		}
	}

	for slot := retention - 1; slot >= 1; slot-- {
		image, ok, err := d.rollbackSlotImage(ctx, repository, slot)
		if err != nil {
			warnings = append(warnings, err)
			continue
		}
		if ok {
			if _, err := d.cli.ImageTag(ctx, client.ImageTagOptions{Source: image, Target: rollbackSlot(repository, slot+1)}); err != nil {
				warnings = append(warnings, fmt.Errorf("rotate rollback slot %d to %d: %w", slot, slot+1, err))
			}
		}
	}
	if _, err := d.cli.ImageTag(ctx, client.ImageTagOptions{Source: imageID, Target: rollbackSlot(repository, 1)}); err != nil {
		warnings = append(warnings, fmt.Errorf("tag last-known-working image %s: %w", imageID, err))
	}
	return errors.Join(warnings...)
}

func (d *DockerClient) rollbackSlotImage(ctx context.Context, repository string, slot int) (string, bool, error) {
	name := rollbackSlot(repository, slot)
	image, err := d.cli.ImageInspect(ctx, name)
	if errdefs.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("inspect rollback slot %d: %w", slot, err)
	}
	return image.ID, true, nil
}

func (d *DockerClient) imageUsedByAnyContainer(ctx context.Context, imageID string) (bool, error) {
	containers, err := d.cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return false, fmt.Errorf("list all containers before rollback image cleanup: %w", err)
	}
	for _, container := range containers.Items {
		if container.ImageID == imageID {
			return true, nil
		}
	}
	return false, nil
}

func rollbackSlot(repository string, slot int) string {
	return repository + ":" + strconv.Itoa(slot)
}
