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
