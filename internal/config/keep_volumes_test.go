package config

import (
	"strings"
	"testing"
)

func TestKeepsVolume(t *testing.T) {
	cfg := CleanupConfig{KeepVolumes: []string{"arcane-backups", "*-data"}}
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"arcane-backups", true}, {"pg-data", true}, {"arcane-rustic-cache", false}, {"", false},
	} {
		if got := cfg.KeepsVolume(tc.name); got != tc.want {
			t.Errorf("KeepsVolume(%q) = %v; want %v", tc.name, got, tc.want)
		}
	}
	if (CleanupConfig{}).KeepsVolume("arcane-backups") {
		t.Error("empty keep list kept a volume")
	}
}

func TestKeepVolumesConfigAndValidation(t *testing.T) {
	env := map[string]string{"HARBORBUDDY_CLEANUP_KEEP_VOLUMES": " arcane-backups, ,*-data "}
	cfg := Default()
	if err := cfg.ApplyEnvironment(func(k string) string { return env[k] }); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.Cleanup.KeepVolumes, ","); got != "arcane-backups,*-data" {
		t.Fatalf("KeepVolumes = %q", got)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	cfg.Cleanup.KeepVolumes = []string{""}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "cleanup.keep_volumes contains an empty pattern") {
		t.Fatalf("Validate() = %v", err)
	}
	cfg.Cleanup.KeepVolumes = []string{"[bad"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), `cleanup.keep_volumes pattern "[bad" is invalid`) {
		t.Fatalf("Validate() = %v", err)
	}
}
