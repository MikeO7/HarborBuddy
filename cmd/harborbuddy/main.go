package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"time"

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
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

type appConfig struct {
	configPath   string
	interval     time.Duration
	scheduleTime string
	timezone     string
	once         bool
	dryRun       bool
	logLevel     string
	cleanupOnly  bool
	showVersion  bool
	updaterMode  bool
	targetID     string
	newImage     string
}

func parseFlags(args []string) (*appConfig, error) {
	fs := flag.NewFlagSet("harborbuddy", flag.ContinueOnError)
	c := &appConfig{}

	fs.StringVar(&c.configPath, "config", "/config/harborbuddy.yml", "Path to config file")
	fs.DurationVar(&c.interval, "interval", 0, "Override update check interval (e.g., 15m, 1h)")
	fs.StringVar(&c.scheduleTime, "schedule-time", "", "Run at specific time daily (e.g., '03:00')")
	fs.StringVar(&c.timezone, "timezone", "", "Timezone for schedule (e.g., 'America/Los_Angeles', 'UTC')")
	fs.BoolVar(&c.once, "once", false, "Run a single update cycle and exit")
	fs.BoolVar(&c.dryRun, "dry-run", false, "Enable dry-run mode (no actual updates)")
	fs.StringVar(&c.logLevel, "log-level", "", "Logging level (debug, info, warn, error)")
	fs.BoolVar(&c.cleanupOnly, "cleanup-only", false, "Run only cleanup logic and exit")
	fs.BoolVar(&c.showVersion, "version", false, "Show version and exit")

	// Internal flags for self-update mechanism
	fs.BoolVar(&c.updaterMode, "updater-mode", false, "Internal: Run in updater helper mode")
	fs.StringVar(&c.targetID, "target-container-id", "", "Internal: ID of the container to update")
	fs.StringVar(&c.newImage, "new-image-id", "", "Internal: ID/Name of the new image")

	// Don't silence usage entirely, but let main print the error
	fs.Usage = func() {
		// Use standard usage printer but to the output set in fs (stderr by default)
		fmt.Fprintf(os.Stderr, "Usage of harborbuddy:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c, nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	// Panic recovery to ensure logs are flushed and errors captured
	defer func() {
		if r := recover(); r != nil {
			log.Error(fmt.Sprintf("PANIC: %v\nStack Trace:\n%s", r, debug.Stack()))
		}
	}()

	opts, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "Error parsing flags: %v\n", err)
		return 1
	}

	if opts.showVersion {
		fmt.Fprintf(stdout, "HarborBuddy version %s (commit: %s, %s/%s)\n", version, commit, runtime.GOOS, runtime.GOARCH)
		return 0
	}

	// If running in updater mode, we skip normal configuration loading
	if opts.updaterMode {
		log.Initialize(log.Config{Level: "info"}) // Basic logging for helper

		if opts.targetID == "" || opts.newImage == "" {
			log.Error("Updater mode requires --target-container-id and --new-image-id")
			return 1
		}

		// Create Docker client (check env first, default to socket)
		dockerHost := os.Getenv("HARBORBUDDY_DOCKER_HOST")
		if dockerHost == "" {
			dockerHost = "unix:///var/run/docker.sock"
		}

		dockerClient, err := docker.NewClient(dockerHost)
		if err != nil {
			log.ErrorErr("Failed to create Docker client for updater", err)
			return 1
		}
		defer dockerClient.Close()

		if err := selfupdate.RunUpdater(ctx, dockerClient, opts.targetID, opts.newImage); err != nil {
			log.ErrorErr("Updater failed", err)
			return 1
		}
		return 0
	}

	// Load configuration
	cfg, err := loadConfig(opts.configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Failed to load configuration: %v\n", err)
		return 1
	}

	// Apply CLI flag overrides
	if opts.interval > 0 {
		cfg.Updates.CheckInterval = opts.interval
	}
	if opts.scheduleTime != "" {
		cfg.Updates.ScheduleTime = opts.scheduleTime
	}
	if opts.timezone != "" {
		cfg.Updates.Timezone = opts.timezone
	}
	if opts.once {
		cfg.RunOnce = true
	}
	if opts.dryRun {
		cfg.Updates.DryRun = true
	}
	if opts.logLevel != "" {
		cfg.Log.Level = opts.logLevel
	}
	if opts.cleanupOnly {
		cfg.CleanupOnly = true
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(stderr, "Invalid configuration: %v\n", err)
		return 1
	}

	// Auto-detect log volume if not explicitly configured
	if cfg.Log.File == "" {
		if info, err := os.Stat("/logs"); err == nil && info.IsDir() {
			cfg.Log.File = "/logs/harborbuddy.log"
			fmt.Fprintf(stdout, "Detected /logs volume, enabling file logging to %s\n", cfg.Log.File)
		} else if info, err := os.Stat("/config"); err == nil && info.IsDir() {
			cfg.Log.File = "/config/harborbuddy.log"
			fmt.Fprintf(stdout, "Detected /config volume, enabling file logging to %s\n", cfg.Log.File)
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
		return 1
	}
	defer dockerClient.Close()

	log.Info("Successfully connected to Docker daemon")

	// Start scheduler
	if err := scheduler.Run(cfg, dockerClient); err != nil {
		log.ErrorErr("Scheduler error", err)
		return 1
	}

	log.Info("HarborBuddy stopped")
	return 0
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
