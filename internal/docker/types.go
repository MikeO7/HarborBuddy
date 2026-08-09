package docker

import (
	"context"
	"errors"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

type ContainerSummary struct {
	ID        string
	Name      string
	ImageRef  string
	ImageID   string
	Labels    map[string]string
	CreatedAt time.Time
}

type ContainerDetails struct {
	Summary  ContainerSummary
	Config   *containertypes.Config
	Host     *containertypes.HostConfig
	Mounts   []containertypes.MountPoint
	Networks map[string]*network.EndpointSettings
	State    *containertypes.State
}

type ImageInfo struct {
	ID        string
	RepoTags  []string
	Dangling  bool
	CreatedAt time.Time
	Size      int64
	Labels    map[string]string
	Config    *containertypes.Config
}

type CleanupResourceKind string

const (
	CleanupImage      CleanupResourceKind = "image"
	CleanupContainer  CleanupResourceKind = "container"
	CleanupNetwork    CleanupResourceKind = "network"
	CleanupVolume     CleanupResourceKind = "volume"
	CleanupBuildCache CleanupResourceKind = "build_cache"
)

type CleanupResource struct {
	Kind       CleanupResourceKind
	ID         string
	Name       string
	CreatedAt  time.Time
	LastUsedAt time.Time
	Size       int64
	Dangling   bool
	InUse      bool
	Protected  bool
}

type CleanupPruneResult struct {
	Deleted        []string
	ReclaimedBytes int64
}

type ReplaceOptions struct {
	StopTimeout           time.Duration
	StartupTimeout        time.Duration
	StabilizationTime     time.Duration
	PollInterval          time.Duration
	CurrentAlreadyStopped bool
}

type ReplaceResult struct {
	NewContainerID    string
	BackupName        string
	BackupCleanupErr  error
	FailureStage      string
	RollbackAttempted bool
	RollbackErr       error
}

type UnsupportedError struct {
	Reason string
}

func (e *UnsupportedError) Error() string { return e.Reason }

func IsUnsupported(err error) bool {
	var target *UnsupportedError
	return errors.As(err, &target)
}

type Client interface {
	ListContainers(context.Context) ([]ContainerSummary, error)
	InspectContainer(context.Context, string) (ContainerDetails, error)
	PullImage(context.Context, string) (ImageInfo, error)
	CheckReplacement(ContainerDetails, ImageInfo) error
	ReplaceContainer(context.Context, ContainerDetails, ImageInfo, ReplaceOptions) (ReplaceResult, error)
	ListImages(context.Context) ([]ImageInfo, error)
	ListDanglingImages(context.Context) ([]ImageInfo, error)
	RemoveImage(context.Context, string) error
}
