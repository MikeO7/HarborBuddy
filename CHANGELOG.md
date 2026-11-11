# Changelog

All notable changes to HarborBuddy are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `HARBORBUDDY_ALLOW_IMAGES` and `HARBORBUDDY_DENY_IMAGES` set the update allow and deny lists without a config file.
- A startup warning when `HARBORBUDDY_INTERVAL` or `--interval` is set but ignored because a schedule time takes precedence.
- The effective-configuration log now includes the stop and startup timeouts, allow and deny lists, the cache-volume age, log rotation limits, and the one-shot modes.

### Changed

- `--once` and `--cleanup-only` exit non-zero when a container update or cleanup removal failed. A daemon cycle with such failures is logged as `partial_failure` at warning level instead of `completed`.
- The update interval is no longer validated for `--once` and `--cleanup-only`, which never use it.
- Integer and duration environment variables ignore surrounding whitespace, as booleans already did.
- One container inspection per container is reused when checking for namespace dependents, instead of one per update candidate per container.
- An active self-update handoff is detected before images are pulled, and `--once` no longer pulls HarborBuddy's own image.
- `make verify-local` now also runs the protocol E2E journeys (`make test-e2e`), so it needs Node after `npm ci`.
- `make test-cover` checks per-package coverage floors from its single test run instead of running each package again.

### Removed

- The weekly `Multi-Platform Runtime Verification` workflow; the publish workflow already runs the image on `linux/amd64`, `linux/arm64`, and `linux/arm/v7`.

### Fixed

- A failed image pull no longer blocks unused-network cleanup for the cycle; only a replacement that actually started does.
- A Docker API error while waiting for a self-update helper no longer makes the daemon exit as if a handoff were in progress; the cycle fails and the next one retries.
- A transient inspect error during the readiness wait no longer rolls back an otherwise healthy replacement; inspection is retried until the startup timeout.
- Rollback image tags above a lowered `updates.rollback_image_retention`, including `0`, are removed at the next update of that image reference instead of staying protected forever.
- Dry-run no longer reports build-cache records as removable when a real run would skip the prune.
- Container names are taken from the container's own name everywhere, never from a legacy link alias.

- Containers with a bind mount that uses a mode such as `cached`, `delegated`, or `consistent`, or a mount target written with a trailing slash, are no longer recreated with a duplicate mount that Docker rejects.
- Image pulls with a very long progress stream no longer fail with `pull response exceeds 16777216 bytes`; the stream is validated as it arrives.
- With `updates.rollback_image_retention`, a second container that shares an image reference no longer rotates the same image into another slot, and at retention `1` no longer removes that image before tagging it.
- An empty entry in `updates.allow_images` or `updates.deny_images` is rejected at startup instead of silently matching nothing.
- A shutdown during the update phase no longer runs cleanup and logs a failed cleanup.
- Long log fields are truncated on a character boundary.

## [0.1.0] - 2026-11-06

First versioned release.

### Added

- `cleanup.dangling_image_min_age_hours` and `cleanup.cache_volumes` with `cleanup.cache_volume_min_age_hours` (and `HARBORBUDDY_CLEANUP_*` equivalents) let untagged images and unused cache volumes expire sooner than `min_age_hours`. Both default to `min_age_hours`, so existing setups are unchanged.
- `cleanup.keep_images` (`HARBORBUDDY_CLEANUP_KEEP_IMAGES`): image patterns cleanup never removes, for images only host tools run.
- `cleanup.keep_volumes` (`HARBORBUDDY_CLEANUP_KEEP_VOLUMES`): volume-name globs cleanup never removes.
- `cleanup.keep_volume_labels` (`HARBORBUDDY_CLEANUP_KEEP_VOLUME_LABELS`): volumes carrying a listed label (`key=value` or `key`) are never removed.
- A container health check: the daemon records a Docker ping every 15 seconds, and `harborbuddy --healthcheck` (the image `HEALTHCHECK`) fails when that heartbeat is over 60 seconds old or an update cycle has run for over two hours. `HARBORBUDDY_HEALTH_FILE` sets the status file path.
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

- Stranded-backup recovery: each cycle restores a `<name>-harborbuddy-backup-*` container from the last 48 hours whose original name is free, using the restart policy recorded in the backup name.
- `previously_failed` result: after a replacement fails readiness and rolls back, that container is not retried with the same image until a different image is pulled.
- `self_update_blocked` result: after a failed self-update, the same image is not retried for 24 hours. Self-update helpers are kept for 7 days so their logs stay readable.
- Startup warnings for unrecognised `HARBORBUDDY_*` variables, with suggestions for likely typos, and for intervals under one minute.
- `NO_COLOR` support for text logs.
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
- Go was updated to 1.27.3 to include current standard-library security fixes.
- The default branch publishes `latest`, `edge`, and commit-specific `sha-*` tags. Version tags also trigger GitHub release publication.
- Configuration loading is strict and rejects unknown fields or multiple YAML documents.
- Routine no-op results now log at debug, warnings at warn, and failures at error; daemon-provided descriptions are bounded to keep logs concise.

- Replacements no longer inherit the old image's settings. `ENV` entries, labels, working directory, user, stop signal, exposed ports, volumes, command, and health check that came from the old image are dropped so the new image's defaults apply; values you set are kept.
- A replacement finishes or rolls back even when HarborBuddy is asked to stop mid-update, and the original is renamed to its backup name before it is stopped, so an interrupted update can always be recovered.
- `allow_images`, `deny_images`, and `keep_images` match the short and fully qualified forms of a reference, so `*:latest` matches `nginx` and `docker.io/library/nginx:*` matches `nginx:1.27`. Pulls are deduplicated on the normalised reference.
- `TZ` is now a fallback: `updates.timezone` and `HARBORBUDDY_TIMEZONE` take precedence over it. If you relied on `TZ` overriding a timezone in your config file, remove the config value.
- Environment booleans accept `true/false`, `yes/no`, `on/off`, and `1/0` in any case, matching YAML; log levels are case-insensitive.
- Text logs carry full RFC 3339 timestamps and use colour only on a terminal.
- A daily `schedule_time` that falls in a spring-forward gap runs at the first instant after the gap.
- The self-update helper runs without networks when Docker is reached over a Unix socket, and copies the daemon's privileged mode, capabilities, user namespace, runtime, and read-only root setting.
- Stopped-container cleanup keeps containers with a restart policy (`always`, `unless-stopped`, `on-failure`) and HarborBuddy's self-update records.
- Intervals under one second are rejected. `log.max_backups: 0` is rejected when `log.file` is set, because the rotation library would keep every file.
### Removed

- `docker.tls`; use standard Docker TLS environment variables instead.
- `updates.update_all`; updates remain opt-out by default and can be narrowed with allow/deny lists.
- The legacy top-level `logging` block; use `log`.
- Documentation for unsupported runtime user/group overrides and registry credential handling.
- Duplicate and obsolete integration scripts and Compose fixtures.

### Fixed

- Image cleanup now removes unused images that have more than one tag. Docker refuses to delete such an image by ID, so the cleanup failed on it every run; HarborBuddy now removes the extra tags of an image no container uses, skipping any tag that has moved to another image, and then deletes it.

- Self-update looped forever on rootless Podman 5: the helper could not disconnect the `pasta` network, rolled back, and the daemon tried again every cycle. `host`, `none`, `pasta`, and `slirp4netns` are no longer disconnected.
- Self-update failed every cycle for a daemon with a static IP or MAC address, because the helper copied it and collided.
- A broken self-update image no longer causes a restart loop in interval mode, including on Podman, which briefly lists a just-exited helper as still active. Every helper failure restores the old daemon's restart policy and starts it again.
- `--once` runs never self-update.
- A container that another container shares a network, PID, IPC, or UTS namespace with, or links to, is skipped instead of being recreated out from under its dependents.
- Containers created from an image ID are reported as unsupported instead of failing a pull every cycle.
- Cleanup no longer tries to remove images used by stopped containers or by other images, which failed with a conflict every cycle, and no longer treats networks used only by stopped containers as unused.
- Cleanup never removes HarborBuddy's own backups or the container whose update failed in that cycle.
- `keep_images` also matches digest references, so images pulled by digest can be kept.
- Build-cache cleanup is skipped for a cycle when Docker's age-only prune would remove a record HarborBuddy keeps.
- `--help` exits 0, and a trailing `---` in the config file is accepted.
### Security

- Build with Go 1.27.3, fixing standard-library advisories GO-2026-6607 (CVE-2026-97031, `crypto/tls`), GO-2026-6609 (CVE-2026-78667, `net/http`), GO-2026-6611 (CVE-2026-78669, `net/http`), and GO-2026-6809 (CVE-2026-111943, `crypto/x509`).

[Unreleased]: https://github.com/MikeO7/HarborBuddy/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/MikeO7/HarborBuddy/releases/tag/v0.1.0
