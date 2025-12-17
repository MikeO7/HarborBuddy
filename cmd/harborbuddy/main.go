package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"context"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/scheduler"
	"github.com/MikeO7/HarborBuddy/internal/selfupdate"
	"github.com/MikeO7/HarborBuddy/pkg/log"
	flag "github.com/spf13/pflag"
)

const version = "0.2.0"

var (
	// commit is injected at build time
	commit = "unknown"
)

func main() {
	// Panic recovery to ensure logs are flushed and errors captured
	defer func() {
		if r := recover(); r != nil {
			log.Error(fmt.Sprintf("PANIC: %v\nStack Trace:\n%s", r, debug.Stack()))
			os.Exit(1)
		}
	}()

	// Define CLI flags
	configPath := flag.String("config", "/config/harborbuddy.yml", "Path to config file")
	interval := flag.Duration("interval", 0, "Override update check interval (e.g., 15m, 1h)")
	scheduleTime := flag.String("schedule-time", "", "Run at specific time daily (e.g., '03:00')")
	timezone := flag.String("timezone", "", "Timezone for schedule (e.g., 'America/Los_Angeles', 'UTC')")
	once := flag.Bool("once", false, "Run a single update cycle and exit")
	dryRun := flag.Bool("dry-run", false, "Enable dry-run mode (no actual updates)")
	logLevel := flag.String("log-level", "", "Logging level (debug, info, warn, error)")
	cleanupOnly := flag.Bool("cleanup-only", false, "Run only cleanup logic and exit")
	showVersion := flag.Bool("version", false, "Show version and exit")

	// Internal flags for self-update mechanism
	updaterMode := flag.Bool("updater-mode", false, "Internal: Run in updater helper mode")
	targetID := flag.String("target-container-id", "", "Internal: ID of the container to update")
	newImage := flag.String("new-image-id", "", "Internal: ID/Name of the new image")

	flag.Parse()

	flag.Parse()

	if *showVersion {
		fmt.Printf("HarborBuddy version %s (commit: %s, %s/%s)\n", version, commit, runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// If running in updater mode, we skip normal configuration loading
	if *updaterMode {
		log.Initialize(log.Config{Level: "info"}) // Basic logging for helper

		if *targetID == "" || *newImage == "" {
			log.Error("Updater mode requires --target-container-id and --new-image-id")
			os.Exit(1)
		}

		// Create Docker client (check env first, default to socket)
		dockerHost := os.Getenv("HARBORBUDDY_DOCKER_HOST")
		if dockerHost == "" {
			dockerHost = "unix:///var/run/docker.sock"
		}

		dockerClient, err := docker.NewClient(dockerHost)
		if err != nil {
			log.ErrorErr("Failed to create Docker client for updater", err)
			os.Exit(1)
		}
		defer dockerClient.Close()

		if err := selfupdate.RunUpdater(context.Background(), dockerClient, *targetID, *newImage); err != nil {
			log.ErrorErr("Updater failed", err)
			os.Exit(1)
		}
		return
	}

	// Load configuration
	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Apply CLI flag overrides
	if *interval > 0 {
		cfg.Updates.CheckInterval = *interval
	}
	if *scheduleTime != "" {
		cfg.Updates.ScheduleTime = *scheduleTime
	}
	if *timezone != "" {
		cfg.Updates.Timezone = *timezone
	}
	if *once {
		cfg.RunOnce = true
	}
	if *dryRun {
		cfg.Updates.DryRun = true
	}
	if *logLevel != "" {
		cfg.Log.Level = *logLevel
	}
	if *cleanupOnly {
		cfg.CleanupOnly = true
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid configuration: %v\n", err)
		os.Exit(1)
	}

	// Auto-detect log volume if not explicitly configured
	if cfg.Log.File == "" {
		if info, err := os.Stat("/logs"); err == nil && info.IsDir() {
			cfg.Log.File = "/logs/harborbuddy.log"
			fmt.Printf("Detected /logs volume, enabling file logging to %s\n", cfg.Log.File)
		} else if info, err := os.Stat("/config"); err == nil && info.IsDir() {
			cfg.Log.File = "/config/harborbuddy.log"
			fmt.Printf("Detected /config volume, enabling file logging to %s\n", cfg.Log.File)
		}
	}

	// Initialize logger
	log.Initialize(log.Config{
		Level:      cfg.Log.Level,
		JSON:       cfg.Log.JSON,
		File:       cfg.Log.File,
		MaxSize:    cfg.Log.MaxSize,
		MaxBackups: cfg.Log.MaxBackups,
	})

	log.Infof("HarborBuddy version %s starting", version)
	log.Infof("Build: commit=%s, os=%s, arch=%s", commit, runtime.GOOS, runtime.GOARCH)
	log.Infof("Docker host: %s", cfg.Docker.Host)

	if cfg.Updates.ScheduleTime != "" {
		log.Infof("Schedule: Daily at %s (%s)", cfg.Updates.ScheduleTime, cfg.Updates.Timezone)
	} else {
		log.Infof("Update interval: %v", cfg.Updates.CheckInterval)
	}

	log.Infof("Dry-run mode: %v", cfg.Updates.DryRun)

	// Create Docker client
	dockerClient, err := docker.NewClient(cfg.Docker.Host)
	if err != nil {
		log.ErrorErr("Failed to create Docker client", err)
		os.Exit(1)
	}
	defer dockerClient.Close()

	log.Info("Successfully connected to Docker daemon")

	// Start scheduler
	if err := scheduler.Run(cfg, dockerClient); err != nil {
		log.ErrorErr("Scheduler error", err)
		os.Exit(1)
	}

	log.Info("HarborBuddy stopped")
}

// loadConfig loads and merges configuration from file and environment
func loadConfig(path string) (config.Config, error) {
	// Check if config env var is set
	if envPath := os.Getenv("HARBORBUDDY_CONFIG"); envPath != "" {
		path = envPath
	}

	// Load from file (or use defaults if file doesn't exist)
	cfg, err := config.LoadFromFile(path)
	if err != nil {
		return config.Config{}, err
	}

	// Apply environment variable overrides
	cfg.ApplyEnvironmentOverrides()

	return cfg, nil
}
