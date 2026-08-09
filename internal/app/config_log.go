package app

import (
	"strings"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/rs/zerolog"
)

func logEffectiveConfig(logger zerolog.Logger, cfg config.Config) {
	logger.Info().
		Str("event", "effective_configuration").
		Bool("updates_enabled", cfg.Updates.Enabled).
		Bool("self_update_enabled", cfg.Updates.SelfUpdate).
		Bool("dry_run", cfg.Updates.DryRun).
		Str("schedule_time", cfg.Updates.ScheduleTime).
		Str("timezone", cfg.Updates.Timezone).
		Str("check_interval", cfg.Updates.CheckInterval.String()).
		Bool("cleanup_enabled", cfg.Cleanup.Enabled).
		Int("cleanup_min_age_hours", cfg.Cleanup.MinAgeHours).
		Bool("cleanup_dangling_only", cfg.Cleanup.DanglingOnly).
		Bool("cleanup_all", cfg.Cleanup.All).
		Str("cleanup_categories", cleanupCategories(cfg.Cleanup)).
		Str("log_level", cfg.Log.Level).
		Bool("log_json", cfg.Log.JSON).
		Bool("log_file_enabled", cfg.Log.File != "").
		Bool("remote_docker", cfg.Docker.Host != "").
		Msg("Effective configuration loaded")
}

func cleanupCategories(cfg config.CleanupConfig) string {
	categories := []string{"images"}
	if cfg.All || cfg.StoppedContainers {
		categories = append(categories, "containers")
	}
	if cfg.All || cfg.UnusedNetworks {
		categories = append(categories, "networks")
	}
	if cfg.All || cfg.UnusedVolumes {
		categories = append(categories, "volumes")
	}
	if cfg.All || cfg.BuildCache {
		categories = append(categories, "build_cache")
	}
	return strings.Join(categories, ",")
}
