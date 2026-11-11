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
		Str("stop_timeout", cfg.Updates.StopTimeout.String()).
		Str("startup_timeout", cfg.Updates.StartupTimeout.String()).
		Str("allow_images", strings.Join(cfg.Updates.AllowImages, ",")).
		Str("deny_images", strings.Join(cfg.Updates.DenyImages, ",")).
		Bool("run_once", cfg.RunOnce).
		Bool("cleanup_only", cfg.CleanupOnly).
		Int("rollback_image_retention", cfg.Updates.RollbackImageRetention).
		Bool("cleanup_enabled", cfg.Cleanup.Enabled).
		Int("cleanup_min_age_hours", cfg.Cleanup.MinAgeHours).
		Int("cleanup_dangling_image_min_age_hours", cfg.Cleanup.MinAgeFor("image", "", true)).
		Str("cleanup_cache_volumes", strings.Join(cfg.Cleanup.CacheVolumes, ",")).
		Int("cleanup_cache_volume_min_age_hours", cacheVolumeMinAge(cfg.Cleanup)).
		Str("cleanup_keep_images", strings.Join(cfg.Cleanup.KeepImages, ",")).
		Str("cleanup_keep_volumes", strings.Join(cfg.Cleanup.KeepVolumes, ",")).
		Str("cleanup_keep_volume_labels", strings.Join(cfg.Cleanup.KeepVolumeLabels, ",")).
		Bool("cleanup_dangling_only", cfg.Cleanup.DanglingOnly).
		Bool("cleanup_all", cfg.Cleanup.All).
		Str("cleanup_categories", cleanupCategories(cfg.Cleanup)).
		Str("log_level", cfg.Log.Level).
		Bool("log_json", cfg.Log.JSON).
		Bool("log_file_enabled", cfg.Log.File != "").
		Int("log_max_size_mb", cfg.Log.MaxSize).
		Int("log_max_backups", cfg.Log.MaxBackups).
		Bool("remote_docker", cfg.Docker.Host != "").
		Msg("Effective configuration loaded")
}

// cacheVolumeMinAge is the age applied to cache volumes: the override when
// set, otherwise the general minimum age.
func cacheVolumeMinAge(cfg config.CleanupConfig) int {
	if cfg.CacheVolumeMinAgeHours != nil {
		return *cfg.CacheVolumeMinAgeHours
	}
	return cfg.MinAgeHours
}

// logConfigWarnings reports valid but suspicious settings, such as unrecognized
// HARBORBUDDY_* variables. They warn instead of failing so existing deployments keep running.
func logConfigWarnings(logger zerolog.Logger, cfg config.Config, environ func() []string) {
	var env []string
	if environ != nil {
		env = environ()
	}
	for _, warning := range config.StartupWarnings(cfg, env) {
		logger.Warn().Str("event", "configuration_warning").Str("setting", warning.Setting).Msg(warning.Message)
	}
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
