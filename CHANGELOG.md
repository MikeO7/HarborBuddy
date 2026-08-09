# Changelog

All notable changes to HarborBuddy are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Transactional container replacement with readiness checks and automatic rollback.
- `updates.startup_timeout` and `HARBORBUDDY_STARTUP_TIMEOUT` for replacement readiness.
- Dedicated daemon role label, `com.harborbuddy.role=daemon`, for the helper-based self-update path.
- `HARBORBUDDY_SELF_UPDATE_ENABLED=false` as an explicit opt-out from self-update, which remains enabled by default.
- Reproducible build metadata for version, commit, and build date.
- Runtime smoke tests for `linux/amd64`, `linux/arm64`, and `linux/arm/v7`.
- SBOM and provenance attestations for published container images.
- A strict Docker/Podman-compatible integration test covering dry-run non-mutation, healthy replacement, readiness failure, rollback, and end-to-end automatic self-update.
- A comprehensive `make verify-local` target with coverage ratchets, race detection, bounded fuzzing, repository linters, and disposable runtime tests.
- Pinned golangci-lint, govulncheck, Gitleaks, Trivy, Actionlint, ShellCheck, Hadolint, and yamllint quality gates.
- Function-complexity and source-size limits, aggregate coverage enforcement, and per-package coverage ratchets.
- Dependabot configuration, CODEOWNERS, a pull-request template, and documented repository policy.

### Changed

- Automatic self-update is enabled by default, uses a temporary helper container, and preserves the old daemon for rollback until the replacement is ready.
- Podman integration now preserves the daemon's security options for the self-update helper and is exercised end to end in CI.
- Self-update helpers inherit the configured stop and startup timeouts, and propagate their typed shutdown result through the scheduler to the application lifecycle.
- Update checks pull eligible image references concurrently, deduplicate pulls, and re-inspect containers before replacement.
- Dry-run pulls images for accurate comparison but never recreates containers or deletes images.
- Docker connections use the Docker SDK's standard `DOCKER_HOST`, `DOCKER_TLS_VERIFY`, `DOCKER_CERT_PATH`, and `DOCKER_API_VERSION` variables when no HarborBuddy-specific host is configured.
- Container builds use BuildKit target platform arguments, a scratch runtime, and `/harborbuddy` as the executable path.
- Image validation, multi-platform publication, and GitHub release creation are sequenced in the container workflow with least-privilege job permissions.
- Container builds use an allow-listed context, explicit source copies, and digest-pinned Dockerfile and Go builder images.
- Go was updated to 1.26.5 to include current standard-library security fixes.
- Stable releases use semantic-version tags and `latest`; the default branch publishes `edge` and immutable SHA tags.
- Configuration loading is strict and rejects unknown fields or multiple YAML documents.

### Removed

- `docker.tls`; use standard Docker TLS environment variables instead.
- `updates.update_all`; updates remain opt-out by default and can be narrowed with allow/deny lists.
- The legacy top-level `logging` block; use `log`.
- Documentation for unsupported runtime user/group overrides and automatic registry credential handling.
- Duplicate and obsolete integration scripts and Compose fixtures.

## [0.2.0] - 2025-12-15

### Added

- Rotating file logs with automatic `/logs` or `/config` path detection.
- Daily scheduled updates through `HARBORBUDDY_SCHEDULE_TIME` and timezone configuration.
- Environment overrides for update and cleanup enablement.

### Fixed

- Excluded-container reporting.
- Validation for stop timeouts and scheduled-update timezones.

### Changed

- Simplified log rotation defaults to 10 MiB and one backup.

## [0.1.0] - 2024-12-03

### Added

- Automatic image pulls and container recreation for standalone Docker Engine.
- Opt-out label `com.harborbuddy.autoupdate=false`.
- Interval scheduling, dry-run mode, image cleanup, and allow/deny image filters.
- YAML, environment, and CLI configuration layers.
- Structured logging and graceful signal handling.
- Scratch container image and initial multi-architecture delivery workflows.

[0.2.0]: https://github.com/MikeO7/HarborBuddy/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/MikeO7/HarborBuddy/releases/tag/v0.1.0
