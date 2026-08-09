# HarborBuddy

HarborBuddy keeps containers on a standalone Docker Engine current by pulling each configured image reference and transactionally replacing containers whose image ID changed.

[![CI](https://github.com/MikeO7/HarborBuddy/actions/workflows/ci.yml/badge.svg)](https://github.com/MikeO7/HarborBuddy/actions/workflows/ci.yml)
[![Container Image](https://github.com/MikeO7/HarborBuddy/actions/workflows/docker-build.yml/badge.svg)](https://github.com/MikeO7/HarborBuddy/actions/workflows/docker-build.yml)
[![License: PolyForm Noncommercial 1.0.0](https://img.shields.io/badge/license-PolyForm%20Noncommercial%201.0.0-blue.svg)](LICENSE)

## Features

- Interval-based or daily scheduled update checks
- Allow and deny filters plus per-container opt-out labels
- Transactional replacement with startup checks and rollback
- Safe automatic self-update, enabled by default, through a short-lived helper container
- Dry-run discovery that pulls images but never recreates containers or deletes images
- Configurable cleanup of old or dangling images
- Structured console or JSON logging with optional rotating files
- Published `linux/amd64`, `linux/arm64`, and `linux/arm/v7` images

HarborBuddy targets standalone Docker Engine hosts. Docker Swarm tasks and containers that cannot be recreated safely are skipped.

## Quick start

Create a Compose file:

```yaml
services:
  harborbuddy:
    image: ghcr.io/mikeo7/harborbuddy:latest
    container_name: harborbuddy
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    environment:
      TZ: UTC
      HARBORBUDDY_SCHEDULE_TIME: "03:00"
      HARBORBUDDY_CONTAINER_NAME: harborbuddy
      # Automatic self-update is enabled by default. To opt out:
      # HARBORBUDDY_SELF_UPDATE_ENABLED: "false"
    labels:
      com.harborbuddy.role: daemon
```

Start it and inspect the logs:

```bash
docker compose up -d
docker compose logs -f harborbuddy
```

Docker access is read/write by design: HarborBuddy must pull images and create, stop, rename, start, and remove containers. Treat access to the Docker daemon as equivalent to host-level administrative access. Run only one active HarborBuddy controller per Docker daemon to avoid competing replacement transactions.

The `latest` tag tracks stable releases. The `edge` tag tracks the current `main` branch.

## Update behavior

For each running container that passes the filters, HarborBuddy:

1. Pulls the container's configured image reference.
2. Compares the pulled image ID with the running container's image ID.
3. Re-inspects the container before changing it to detect concurrent changes.
4. Stops the old container and renames it as a temporary backup.
5. Creates a replacement with the original configuration, host settings, name, and networks.
6. Starts the replacement and waits for its health check, or for a short stabilization period when no health check exists.
7. Removes the backup after the replacement is ready.

If creation, startup, or readiness fails, HarborBuddy removes the failed replacement, restores the old container's name and networks, and restarts it. A backup-cleanup failure is reported as a warning rather than hiding a successful update.

HarborBuddy skips replacements it cannot perform safely, including auto-remove containers, Docker Swarm task containers, and containers using another container's network namespace.

### Dry-run semantics

Dry-run mode still pulls eligible image references so it can compare the actual image IDs. It may therefore use network bandwidth and update the Docker Engine's local image cache. It does not stop, recreate, rename, or remove containers, and cleanup reports candidate images without deleting them.

Enable it with either:

```yaml
environment:
  HARBORBUDDY_DRY_RUN: "true"
```

or:

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  ghcr.io/mikeo7/harborbuddy:latest \
  --once --dry-run
```

## Selecting containers

Containers are managed by default unless excluded by configuration or label.

Exclude a container:

```yaml
labels:
  com.harborbuddy.autoupdate: "false"
```

The HarborBuddy daemon uses a separate role label:

```yaml
labels:
  com.harborbuddy.role: daemon
```

The published image also carries this label so operators and tooling can identify the daemon. HarborBuddy detects its own exact container identity at runtime and routes that container through the helper lifecycle described below.

Use `updates.allow_images` and `updates.deny_images` for image-reference filtering. Exact matches and one wildcard at either edge are supported, for example `nginx:*`, `ghcr.io/example/*`, or `*:stable`. Deny rules take precedence.

## Configuration

HarborBuddy reads `/config/harborbuddy.yml` by default. A missing optional file uses defaults. YAML parsing is strict: unknown fields and multiple YAML documents are rejected.

```yaml
docker:
  # Leave empty to use the Docker SDK's standard DOCKER_* environment variables.
  host: ""

updates:
  enabled: true
  check_interval: 12h
  schedule_time: ""
  timezone: UTC
  dry_run: false
  allow_images:
    - "*"
  deny_images: []
  stop_timeout: 10s
  startup_timeout: 30s

cleanup:
  enabled: true
  min_age_hours: 24
  dangling_only: true

log:
  level: info
  json: false
  file: ""
  max_size: 10
  max_backups: 1
```

When `updates.schedule_time` is set, HarborBuddy runs at that `HH:MM` time in `updates.timezone`; otherwise it uses `updates.check_interval`. The interval must remain positive even when updates are disabled.

Mount a configuration file read-only:

```yaml
volumes:
  - ./harborbuddy.yml:/config/harborbuddy.yml:ro
```

### Configuration migration

The current schema removes these legacy keys:

- `docker.tls`
- `updates.update_all`
- the top-level `logging` block

Use standard Docker TLS environment variables, the default opt-out update model plus allow/deny lists, and the `log` block shown above. The current schema also adds `updates.startup_timeout`.

### Environment variables

Environment variables override YAML values.

| Variable | Default | Purpose |
| --- | --- | --- |
| `HARBORBUDDY_CONFIG` | `/config/harborbuddy.yml` | Configuration file path |
| `HARBORBUDDY_DOCKER_HOST` | empty | Explicit Docker endpoint; overrides `DOCKER_HOST` |
| `HARBORBUDDY_INTERVAL` | `12h` | Update check interval |
| `HARBORBUDDY_SCHEDULE_TIME` | empty | Daily time in `HH:MM` |
| `HARBORBUDDY_TIMEZONE` | `UTC` | IANA timezone; takes precedence over `TZ` |
| `TZ` | empty | Standard timezone fallback |
| `HARBORBUDDY_DRY_RUN` | `false` | Pull and report without recreation or deletion |
| `HARBORBUDDY_UPDATES_ENABLED` | `true` | Enable update checks |
| `HARBORBUDDY_SELF_UPDATE_ENABLED` | `true` | Enable safe helper-based HarborBuddy self-update; set to `false` to opt out |
| `HARBORBUDDY_CONTAINER_NAME` | empty | Stable Docker container name used to identify this daemon for self-update |
| `HARBORBUDDY_CONTAINER_ID` | empty | Optional container ID/prefix identity override; prefer the stable name setting |
| `HARBORBUDDY_STOP_TIMEOUT` | `10s` | Graceful stop timeout |
| `HARBORBUDDY_STARTUP_TIMEOUT` | `30s` | Replacement readiness timeout |
| `HARBORBUDDY_CLEANUP_ENABLED` | `true` | Enable image cleanup |
| `HARBORBUDDY_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `HARBORBUDDY_LOG_JSON` | `false` | Emit JSON logs |
| `HARBORBUDDY_LOG_FILE` | empty | Explicit rotating log path; stdout remains enabled |
| `HARBORBUDDY_LOG_MAX_SIZE` | `10` | Rotation size in MiB |
| `HARBORBUDDY_LOG_MAX_BACKUPS` | `1` | Rotated files to retain |

Invalid booleans, integers, and durations fail startup instead of silently falling back.

### Docker connection and TLS

When `docker.host` and `HARBORBUDDY_DOCKER_HOST` are empty, the Docker SDK honors its standard variables:

- `DOCKER_HOST`
- `DOCKER_TLS_VERIFY`
- `DOCKER_CERT_PATH`
- `DOCKER_API_VERSION`

Example for a TLS-protected remote daemon:

```yaml
environment:
  DOCKER_HOST: tcp://docker.example.net:2376
  DOCKER_TLS_VERIFY: "1"
  DOCKER_CERT_PATH: /certs/client
volumes:
  - ./certs/client:/certs/client:ro
```

Remote daemon access must be secured and reachable both by the normal daemon and by the self-update helper. HarborBuddy does not provide separate registry credential configuration; only use image references the configured Docker environment can pull successfully.

## Automatic self-update

Safe automatic self-update is a core feature and is enabled by default. Because a process cannot replace its own running container directly, HarborBuddy uses a helper container:

1. The daemon pulls the newer HarborBuddy image and creates a uniquely named, short-lived helper from that image.
2. The helper receives the target container identity and the same Docker connection needed to manage the daemon.
3. The daemon shuts down normally while the helper waits for its container to exit, then re-inspects it.
4. The helper preserves the old daemon as a backup, creates the replacement under the original name and settings, and waits for startup readiness.
5. On success, it removes the backup, exits, and is automatically removed by Docker.
6. On failure, it removes the failed replacement, restores the backup's original identity and networks, and restarts the old daemon.

Self-update requires working read/write Docker access inside both the daemon and helper. Set `HARBORBUDDY_CONTAINER_NAME` to the stable Docker container name when using Compose or a customized hostname. HarborBuddy otherwise uses Linux runtime identity and Docker's default hostname. The `com.harborbuddy.role=daemon` label protects unidentified HarborBuddy containers from ordinary replacement, but is never trusted as positive identity on a potentially remote daemon.

To keep HarborBuddy updates under manual change control, disable only self-update while continuing to manage other eligible containers:

```yaml
environment:
  HARBORBUDDY_SELF_UPDATE_ENABLED: "false"
```

If automatic self-update cannot complete, inspect both the daemon and helper logs, correct the Docker access or configuration problem, and use the manual fallback:

```bash
docker compose pull harborbuddy
docker compose up -d --no-deps harborbuddy
```

Pinning a release tag instead of `latest` intentionally prevents discovery of later release tags; update the pinned tag manually when desired.

## Cleanup

Cleanup runs after update checks when enabled.

- `dangling_only: true` considers only untagged images.
- `min_age_hours` prevents removal of newer candidates.
- `dangling_only: false` broadens the candidate list, but Docker still refuses removal of images in use.
- Dry-run reports `would_remove` and performs no deletion.

Use conservative cleanup settings on shared Docker hosts.

## Logging

Logs go to the container's standard output. Set `log.json` or `HARBORBUDDY_LOG_JSON=true` for JSON output.

File logging is opt-in. Set `log.file` or `HARBORBUDDY_LOG_FILE` explicitly (for example, `/logs/harborbuddy.log` when mounting a `/logs` volume). File logging rotates according to `max_size` and `max_backups`.

## CLI

```text
--config PATH          configuration file
--interval DURATION    override the check interval
--schedule-time HH:MM  override the daily schedule
--timezone ZONE        override the schedule timezone
--once                 run one cycle and exit
--dry-run              pull and report without recreating containers or deleting images
--cleanup-only         run cleanup and exit
--log-level LEVEL      override the log level
--version              print build information
```

The updater-mode flags are internal implementation details and should not be invoked manually.

## Development

```bash
go mod download
make verify-local  # comprehensive checks; auto-detects Docker or Podman
```

`make verify-local` runs formatting, source limits, vet, lint, vulnerability scanning, coverage ratchets, the race detector, bounded fuzzing, the binary build, non-Go linters, and the disposable integration suite. Set `CONTAINER_ENGINE=/path/to/podman` or `/path/to/docker` to select an engine explicitly. Podman is supported as a local Docker-compatible test engine; HarborBuddy's production target remains standalone Docker Engine.

CI enforces pinned Go and non-Go linters, a 100% aggregate coverage floor plus per-package ratchets, reachable-vulnerability and secret scans, immutable workflow action references, and fixed HIGH/CRITICAL image vulnerability checks.

See [CONTRIBUTING.md](CONTRIBUTING.md), [docs/repository-policy.md](docs/repository-policy.md), [test/README.md](test/README.md), and [SECURITY.md](SECURITY.md).

## License

HarborBuddy is noncommercial source-available software under the [PolyForm Noncommercial License 1.0.0](LICENSE). Personal and other permitted noncommercial uses are allowed; commercial use is not.
