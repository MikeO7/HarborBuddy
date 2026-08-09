package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/MikeO7/HarborBuddy/internal/config"
)

func TestFlagOverridesApplyEveryUserFacingOverride(t *testing.T) {
	flags, values, err := newFlagSet(&bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := flags.Parse([]string{
		"--interval=1h", "--schedule-time=03:15", "--timezone=UTC", "--once",
		"--dry-run", "--log-level=debug", "--cleanup-only",
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	applyFlagOverrides(flags, values, &cfg)
	if cfg.Updates.CheckInterval != time.Hour || cfg.Updates.ScheduleTime != "03:15" || cfg.Updates.Timezone != "UTC" ||
		!cfg.RunOnce || !cfg.Updates.DryRun || cfg.Log.Level != "debug" || !cfg.CleanupOnly {
		t.Fatalf("flag overrides were incomplete: %+v", cfg)
	}
}

func TestConfigPathPrecedence(t *testing.T) {
	flags, values, err := newFlagSet(&bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	env := func(string) (string, bool) { return "/env/config.yml", true }
	if path, required := configPath(flags, values, env); path != "/env/config.yml" || !required {
		t.Fatalf("environment config path = %q, required=%v", path, required)
	}
	if err := flags.Parse([]string{"--config", "/cli/config.yml"}); err != nil {
		t.Fatal(err)
	}
	if path, required := configPath(flags, values, env); path != "/cli/config.yml" || !required {
		t.Fatalf("CLI config path = %q, required=%v", path, required)
	}
	flags, values, err = newFlagSet(&bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if path, required := configPath(flags, values, func(string) (string, bool) { return "", false }); path != config.DefaultPath || required {
		t.Fatalf("default config path = %q, required=%v", path, required)
	}
}

func TestHideFlagsReportsUnknownFlag(t *testing.T) {
	flags, _, err := newFlagSet(&bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := hideFlags(flags, "does-not-exist"); err == nil || !strings.Contains(err.Error(), "hide flag does-not-exist") {
		t.Fatalf("hideFlags() error = %v", err)
	}
}

func TestHelperFlagsAreHidden(t *testing.T) {
	flags, _, err := newFlagSet(&bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	flags.SetOutput(&output)
	flags.PrintDefaults()
	for _, name := range []string{"updater-mode", "target-container-id", "new-image-id", "helper-stop-timeout", "helper-startup-timeout", "helper-restart-policy", "helper-restart-max-retries"} {
		if strings.Contains(output.String(), name) {
			t.Fatalf("help output contains hidden flag %q: %s", name, output.String())
		}
	}
}
