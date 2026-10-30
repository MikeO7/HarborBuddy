package docker

import (
	"context"
	"errors"
	"fmt"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

// RemoveImage deletes an image by ID without forcing. Docker refuses to delete
// an image by ID while two or more tags reference it, so on that conflict the
// extra tags of an unused image are removed first and the delete is retried.
func (d *DockerClient) RemoveImage(ctx context.Context, imageID string) error {
	_, err := d.cli.ImageRemove(ctx, imageID, client.ImageRemoveOptions{Force: false, PruneChildren: true})
	if err == nil {
		return nil
	}
	removeErr := fmt.Errorf("remove image %s: %w", imageID, err)
	if !errdefs.IsConflict(err) {
		return removeErr
	}
	untagged, untagErr := d.untagExtraReferences(ctx, imageID)
	if untagErr != nil {
		return errors.Join(removeErr, untagErr)
	}
	if !untagged {
		return removeErr
	}
	if _, err := d.cli.ImageRemove(ctx, imageID, client.ImageRemoveOptions{Force: false, PruneChildren: true}); err != nil {
		return fmt.Errorf("remove image %s after removing extra tags: %w", imageID, err)
	}
	return nil
}

// untagExtraReferences removes every tag but the first from an image that no
// container uses. It reports false when there was nothing safe to untag.
func (d *DockerClient) untagExtraReferences(ctx context.Context, imageID string) (bool, error) {
	image, err := d.inspectImage(ctx, imageID)
	if err != nil {
		return false, err
	}
	if len(image.RepoTags) < 2 {
		return false, nil
	}
	inUse, err := d.imageUsedByAnyContainer(ctx, image.ID)
	if err != nil || inUse {
		return false, err
	}
	for _, tag := range image.RepoTags[1:] {
		if err := d.untagIfCurrent(ctx, tag, image.ID); err != nil {
			return false, err
		}
	}
	return true, nil
}

// untagIfCurrent removes tag only while it still points at imageID, so a tag
// moved to a newer image in the meantime is left alone.
func (d *DockerClient) untagIfCurrent(ctx context.Context, tag, imageID string) error {
	current, err := d.cli.ImageInspect(ctx, tag)
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect image tag %s: %w", tag, err)
	}
	if current.ID != imageID {
		return nil
	}
	if _, err := d.cli.ImageRemove(ctx, tag, client.ImageRemoveOptions{Force: false}); err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("remove image tag %s: %w", tag, err)
	}
	return nil
}
