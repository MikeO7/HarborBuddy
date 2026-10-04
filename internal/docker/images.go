package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	imagetypes "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/client"
)

func (d *DockerClient) PullImage(ctx context.Context, imageRef string) (ImageInfo, error) {
	reader, err := d.cli.ImagePull(ctx, imageRef, client.ImagePullOptions{})
	if err != nil {
		return ImageInfo{}, fmt.Errorf("pull image %s: %w", imageRef, err)
	}
	const maxPullResponse = 16 * 1024 * 1024
	response, readErr := io.ReadAll(io.LimitReader(reader, maxPullResponse+1))
	closeErr := reader.Close()
	if readErr != nil {
		return ImageInfo{}, fmt.Errorf("read pull response for %s: %w", imageRef, readErr)
	}
	if closeErr != nil {
		return ImageInfo{}, fmt.Errorf("close pull response for %s: %w", imageRef, closeErr)
	}
	if len(response) > maxPullResponse {
		return ImageInfo{}, fmt.Errorf("pull response exceeds %d bytes", maxPullResponse)
	}
	if len(bytes.TrimSpace(response)) == 0 {
		return ImageInfo{}, fmt.Errorf("empty pull response for %s", imageRef)
	}
	if err := validatePullResponse(response); err != nil {
		return ImageInfo{}, fmt.Errorf("read pull response for %s: %w", imageRef, err)
	}
	return d.inspectImage(ctx, imageRef)
}

func (d *DockerClient) inspectImage(ctx context.Context, imageRef string) (ImageInfo, error) {
	inspect, err := d.cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return ImageInfo{}, fmt.Errorf("inspect image %s: %w", imageRef, err)
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, inspect.Created)
	var imageConfig *containertypes.Config
	var labels map[string]string
	if inspect.Config != nil {
		imageConfig = &containertypes.Config{
			User:        inspect.Config.User,
			Env:         append([]string(nil), inspect.Config.Env...),
			Entrypoint:  append([]string(nil), inspect.Config.Entrypoint...),
			Cmd:         append([]string(nil), inspect.Config.Cmd...),
			WorkingDir:  inspect.Config.WorkingDir,
			Labels:      cloneStringMap(inspect.Config.Labels),
			StopSignal:  inspect.Config.StopSignal,
			Healthcheck: inspect.Config.Healthcheck,
		}
		labels = cloneStringMap(inspect.Config.Labels)
	}
	return ImageInfo{
		ID:        inspect.ID,
		RepoTags:  append([]string(nil), inspect.RepoTags...),
		Dangling:  len(inspect.RepoTags) == 0,
		CreatedAt: createdAt,
		Size:      inspect.Size,
		Labels:    labels,
		Config:    imageConfig,
	}, nil
}

func (d *DockerClient) ListImages(ctx context.Context) ([]ImageInfo, error) {
	images, err := d.cli.ImageList(ctx, client.ImageListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	return imageSummaries(images.Items, false), nil
}

func (d *DockerClient) ListDanglingImages(ctx context.Context) ([]ImageInfo, error) {
	args := make(client.Filters).Add("dangling", "true")
	images, err := d.cli.ImageList(ctx, client.ImageListOptions{All: true, Filters: args})
	if err != nil {
		return nil, fmt.Errorf("list dangling images: %w", err)
	}
	return imageSummaries(images.Items, true), nil
}

func imageSummaries(images []imagetypes.Summary, forceDangling bool) []ImageInfo {
	result := make([]ImageInfo, 0, len(images))
	for _, current := range images {
		dangling := forceDangling || len(current.RepoTags) == 0 || (len(current.RepoTags) == 1 && current.RepoTags[0] == "<none>:<none>")
		result = append(result, ImageInfo{
			ID:        current.ID,
			RepoTags:  append([]string(nil), current.RepoTags...),
			Dangling:  dangling,
			Protected: hasRollbackImageTag(current.RepoTags),
			CreatedAt: time.Unix(current.Created, 0),
			Size:      current.Size,
			Labels:    cloneStringMap(current.Labels),
		})
	}
	return result
}

func (d *DockerClient) RemoveImage(ctx context.Context, imageID string) error {
	if _, err := d.cli.ImageRemove(ctx, imageID, client.ImageRemoveOptions{Force: false, PruneChildren: true}); err != nil {
		return fmt.Errorf("remove image %s: %w", imageID, err)
	}
	return nil
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func validatePullResponse(response []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(response))
	for {
		var message *struct {
			jsonstream.Message
			LegacyError string `json:"error"`
		}
		if err := decoder.Decode(&message); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		if message == nil || (message.Stream == "" && message.Status == "" && message.Progress == nil && message.ID == "" && message.Error == nil && message.Aux == nil && message.LegacyError == "") {
			return errors.New("missing pull message")
		}
		if message.Error != nil {
			return message.Error
		}
		if message.LegacyError != "" {
			return errors.New(message.LegacyError)
		}
	}
}
