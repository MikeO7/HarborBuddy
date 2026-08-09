# Changelog

All notable changes to HarborBuddy are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and future versioned releases will use [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Staged container replacement with readiness checks and best-effort rollback.
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
- Opt-in cleanup for stopped containers, unused tagged images, unused networks, unused volumes, and build cache through a master environment switch or independent category switches.
- Operational event logging with stable event names, effective configuration, correlated cycles, transaction and rollback fields, self-update helper outcomes, and per-resource cleanup summaries.

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
- The default branch publishes `latest`, `edge`, and commit-specific `sha-*` tags. Version tags will also trigger GitHub release publication when the first versioned release is created.
- Configuration loading is strict and rejects unknown fields or multiple YAML documents.
- Routine no-op results now log at debug, warnings at warn, and failures at error; daemon-provided descriptions are bounded to keep logs concise.

### Removed

- `docker.tls`; use standard Docker TLS environment variables instead.
- `updates.update_all`; updates remain opt-out by default and can be narrowed with allow/deny lists.
- The legacy top-level `logging` block; use `log`.
- Documentation for unsupported runtime user/group overrides and registry credential handling.
- Duplicate and obsolete integration scripts and Compose fixtures.
