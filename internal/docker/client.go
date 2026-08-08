package docker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/docker/docker/client"
)

type DockerClient struct {
	cli *client.Client
}

func NewClient(ctx context.Context, host string) (*DockerClient, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := cli.Ping(pingCtx); err != nil {
		return nil, errors.Join(fmt.Errorf("ping Docker daemon: %w", err), cli.Close())
	}
	return &DockerClient{cli: cli}, nil
}

func (d *DockerClient) Close() error {
	if d == nil || d.cli == nil {
		return nil
	}
	return d.cli.Close()
}
