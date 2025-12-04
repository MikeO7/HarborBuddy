# Changelog

All notable changes to HarborBuddy will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2024-12-03

### Added

#### Core Features
- Automatic container updates by checking for newer images
- Update all containers by default (opt-out model)
- Label-based exclusion with `com.harborbuddy.autoupdate="false"`
- Configurable update check intervals
- Dry-run mode for previewing changes
- Image cleanup with configurable policies
- Pattern-based image filtering (allow/deny lists)

#### Configuration
- Three-tier configuration system (CLI flags > env vars > YAML file)
- YAML configuration file support
- Environment variable overrides
- CLI flag overrides
- Default configuration values

#### Docker Integration
- Docker socket support (`unix:///var/run/docker.sock`)
- Remote Docker host support (tcp://)
- Container inspection and recreation
- Image pulling and management
- Graceful container stop/start/replace

#### CLI
- `--config` - Specify config file path
- `--interval` - Override check interval
- `--once` - Single run mode
- `--dry-run` - Preview mode
- `--log-level` - Set logging level
- `--cleanup-only` - Cleanup-only mode
- `--version` - Show version

#### Logging
- Structured logging with zerolog
- Multiple log levels (debug, info, warn, error)
- JSON output support
- Context-aware logging for containers and images

#### Scheduler
- Configurable interval-based updates
- Graceful shutdown on SIGTERM/SIGINT
- Single-run mode support
- Cleanup-only mode support

#### Cleanup
- Dangling image removal
- Unused image removal
- Age-based filtering
- Configurable minimum age threshold

#### Development
- Comprehensive test suite
- GitHub Actions CI/CD workflows
- Multi-stage Dockerfile
- Example configurations
- Development documentation

### Documentation
- Complete README with usage examples
- Quick start guide
- Contributing guidelines
- Example docker-compose.yml
- Example configuration file
- Troubleshooting guide

### Technical Details
- Written in Go 1.23
- Uses official Docker SDK
- Runs as lightweight container (FROM scratch)
- Multi-architecture support (amd64, arm64)

## [Unreleased]

### Planned for Future Releases
- Semantic version comparison and policies
- Health checks and automatic rollback
- Prometheus metrics endpoint
- Web UI for monitoring and control
- Multi-host support
- Kubernetes integration
- Per-container update schedules (cron-like)
- Webhook notifications

---

[0.1.0]: https://github.com/MikeO7/HarborBuddy/releases/tag/v0.1.0

