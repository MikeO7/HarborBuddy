package cleanup

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/rs/zerolog"
)

func TestBuildCacheRetainedByDockerIsReported(t *testing.T) {
	now := time.Now()
	client := &fakeClient{
		resources: map[docker.CleanupResourceKind][]docker.CleanupResource{
			docker.CleanupBuildCache: {{Kind: docker.CleanupBuildCache, ID: "old", Name: strings.Repeat("x", 300), CreatedAt: now.Add(-48 * time.Hour)}},
		},
		pruneResult: docker.CleanupPruneResult{},
	}
	cfg := config.Default()
	cfg.Cleanup.BuildCache = true
	var output bytes.Buffer
	logger := zerolog.New(&output).Level(zerolog.DebugLevel)
	report, err := runCleanupAt(context.Background(), cfg, client, logger, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 1 || report.Results[0].Reason != "Docker prune retained the eligible cache record" {
		t.Fatalf("retained cache report = %+v", report)
	}
	text := output.String()
	if !strings.Contains(text, `"resource_name_truncated":true`) || !strings.Contains(text, `"event":"cleanup_resource_summary"`) {
		t.Fatalf("cleanup operational log = %q", text)
	}
}

func TestVolumeLogPreservesFullResourceName(t *testing.T) {
	resource := docker.CleanupResource{Kind: docker.CleanupVolume, ID: "signal-cli-data"}
	if got := displayResourceID(resource); got != resource.ID {
		t.Fatalf("displayResourceID(volume) = %q", got)
	}
}
