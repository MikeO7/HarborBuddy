package main

import (
	"fmt"
	"os"

	"github.com/MikeO7/HarborBuddy/internal/config"
	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/MikeO7/HarborBuddy/internal/scheduler"
	"github.com/MikeO7/HarborBuddy/pkg/log"
	flag "github.com/spf13/pflag"
)

const version = "0.1.0"

func main() {
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

	flag.Parse()

	if *showVersion {
		fmt.Printf("HarborBuddy version %s\n", version)
		os.Exit(0)
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

	// Initialize logger
	log.Initialize(cfg.Log.Level, cfg.Log.JSON)

	log.Infof("HarborBuddy version %s starting", version)
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
