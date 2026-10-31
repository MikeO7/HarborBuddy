package cleanup

import (
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
)

func TestClassifyUsesPerResourceMinimumAge(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	twoHours := now.Add(-2 * time.Hour)
	one, zero := 1, 0
	cfg := config.CleanupConfig{MinAgeHours: 168, All: true, DanglingImageMinAgeHours: &one,
		CacheVolumes: []string{"*gocache"}, CacheVolumeMinAgeHours: &zero}
	for _, tc := range []struct {
		name     string
		resource docker.CleanupResource
		eligible bool
	}{
		{"dangling image", docker.CleanupResource{Kind: docker.CleanupImage, Dangling: true, CreatedAt: twoHours}, true},
		{"tagged image", docker.CleanupResource{Kind: docker.CleanupImage, Name: "app:1", CreatedAt: twoHours}, false},
		{"cache volume", docker.CleanupResource{Kind: docker.CleanupVolume, ID: "hb-gocache", Name: "hb-gocache", CreatedAt: now}, true},
		{"other volume", docker.CleanupResource{Kind: docker.CleanupVolume, ID: "pgdata", Name: "pgdata", CreatedAt: twoHours}, false},
		{"cache volume in use", docker.CleanupResource{Kind: docker.CleanupVolume, Name: "hb-gocache", CreatedAt: now, InUse: true}, false},
		{"protected dangling image", docker.CleanupResource{Kind: docker.CleanupImage, Dangling: true, Protected: true, CreatedAt: twoHours}, false},
	} {
		if got := classify(tc.resource, cfg, now); got.Eligible != tc.eligible {
			t.Errorf("%s: eligible = %v (%s); want %v", tc.name, got.Eligible, got.Reason, tc.eligible)
		}
	}
}

func TestClassifyKeepsListedImages(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	old := now.Add(-30 * 24 * time.Hour)
	cfg := config.CleanupConfig{MinAgeHours: 168, All: true, KeepImages: []string{"openthread/border-router:*"}}
	kept := classify(docker.CleanupResource{Kind: docker.CleanupImage, Name: "x:1,openthread/border-router:latest", CreatedAt: old}, cfg, now)
	if kept.Eligible || kept.Reason != "image is in cleanup.keep_images" {
		t.Fatalf("kept image = %+v", kept)
	}
	if other := classify(docker.CleanupResource{Kind: docker.CleanupImage, Name: "app:1", CreatedAt: old}, cfg, now); !other.Eligible {
		t.Fatalf("unlisted image = %+v", other)
	}
	if vol := classify(docker.CleanupResource{Kind: docker.CleanupVolume, Name: "openthread/border-router:latest", CreatedAt: old}, cfg, now); !vol.Eligible {
		t.Fatalf("keep list applied to a volume: %+v", vol)
	}
}

func TestClassifyKeepsListedVolumes(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	old := now.Add(-30 * 24 * time.Hour)
	cfg := config.CleanupConfig{MinAgeHours: 168, All: true, KeepVolumes: []string{"arcane-backups"}}
	kept := classify(docker.CleanupResource{Kind: docker.CleanupVolume, ID: "arcane-backups", Name: "arcane-backups", CreatedAt: old}, cfg, now)
	if kept.Eligible || kept.Reason != "volume is in cleanup.keep_volumes" {
		t.Fatalf("kept volume = %+v", kept)
	}
	if other := classify(docker.CleanupResource{Kind: docker.CleanupVolume, Name: "arcane-rustic-cache", CreatedAt: old}, cfg, now); !other.Eligible {
		t.Fatalf("unlisted volume = %+v", other)
	}
	if net := classify(docker.CleanupResource{Kind: docker.CleanupNetwork, Name: "arcane-backups", CreatedAt: old}, cfg, now); !net.Eligible {
		t.Fatalf("keep list applied to a network: %+v", net)
	}
}

func TestClassifyKeepsVolumesByLabel(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	old := now.Add(-30 * 24 * time.Hour)
	cfg := config.CleanupConfig{MinAgeHours: 168, All: true, KeepVolumeLabels: []string{"com.getarcaneapp.internal.resource=true"}}
	arcane := map[string]string{"com.getarcaneapp.internal.resource": "true"}
	kept := classify(docker.CleanupResource{Kind: docker.CleanupVolume, Name: "arcane-rustic-cache", Labels: arcane, CreatedAt: old}, cfg, now)
	if kept.Eligible || kept.Reason != "volume label is in cleanup.keep_volume_labels" {
		t.Fatalf("labeled volume = %+v", kept)
	}
	if other := classify(docker.CleanupResource{Kind: docker.CleanupVolume, Name: "x", CreatedAt: old}, cfg, now); !other.Eligible {
		t.Fatalf("unlabeled volume = %+v", other)
	}
	if img := classify(docker.CleanupResource{Kind: docker.CleanupImage, Name: "x:1", Labels: arcane, CreatedAt: old}, cfg, now); !img.Eligible {
		t.Fatalf("volume label list applied to an image: %+v", img)
	}
}
