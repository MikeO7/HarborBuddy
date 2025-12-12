# HarborBuddy

[![CI](https://github.com/MikeO7/HarborBuddy/actions/workflows/ci.yml/badge.svg)](https://github.com/MikeO7/HarborBuddy/actions/workflows/ci.yml)
[![Docker Build](https://github.com/MikeO7/HarborBuddy/actions/workflows/docker-build.yml/badge.svg)](https://github.com/MikeO7/HarborBuddy/actions/workflows/docker-build.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)

**HarborBuddy** is a Docker-native daemon that automatically keeps your containers up to date by checking for newer images, recreating containers with updated images, and cleaning up unused images.

> ⚠️ **Important**: Always test in dry-run mode first and exclude critical services (databases, etc.) using labels.


## Features

- **Auto-update by default**: Updates all containers automatically unless explicitly excluded
- **Opt-out control**: Use labels to exclude specific containers (e.g., databases)
- **Configurable intervals**: Set how often to check for updates
- **Dry-run mode**: Preview what would be updated without making changes
- **Image cleanup**: Automatically removes unused/dangling images
- **Docker-native**: Runs as a container alongside your workload
- **Minimal configuration**: Sensible defaults for zero-config operation

## Quick Start

### Using Docker Compose

1. Create a `docker-compose.yml` file:

```yaml
services:
  harborbuddy:
    image: ghcr.io/mikeo7/harborbuddy:latest
    container_name: harborbuddy
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      HARBORBUDDY_INTERVAL: "30m"
    labels:
      com.harborbuddy.autoupdate: "false"  # Don't update itself
```

2. Start HarborBuddy:

```bash
docker-compose up -d
```

3. View logs:

```bash
docker-compose logs -f harborbuddy
```

### Using Docker CLI

```bash
docker run -d \
  --name harborbuddy \
  --restart unless-stopped \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -e HARBORBUDDY_INTERVAL=30m \
  -l com.harborbuddy.autoupdate=false \
  ghcr.io/mikeo7/harborbuddy:latest
```

## Configuration

HarborBuddy uses a three-tier configuration system with the following priority order:

1. **CLI flags** (highest priority)
2. **Environment variables**
3. **YAML config file** (lowest priority)

### Configuration File

Create a YAML configuration file (default path: `/config/harborbuddy.yml`):

```yaml
docker:
  host: "unix:///var/run/docker.sock"

updates:
  enabled: true
  check_interval: "30m"
  dry_run: false
  allow_images:
    - "*"
  deny_images:
    - "postgres:*"
    - "mysql:*"

cleanup:
  enabled: true
  min_age_hours: 24
  dangling_only: true

log:
  level: "info"
  json: false
```

Mount the config file in your container:

```yaml
volumes:
  - ./harborbuddy.yml:/config/harborbuddy.yml:ro
```

### Environment Variables

Override configuration using environment variables:

- `HARBORBUDDY_CONFIG` - Path to config file
- `HARBORBUDDY_INTERVAL` - Check interval (e.g., `15m`, `1h`, `6h`)
- `HARBORBUDDY_DRY_RUN` - Enable dry-run mode (`true` or `false`)
- `HARBORBUDDY_LOG_LEVEL` - Log level (`debug`, `info`, `warn`, `error`)
- `HARBORBUDDY_LOG_JSON` - Output JSON logs (`true` or `false`)
- `HARBORBUDDY_DOCKER_HOST` - Docker host connection string

### CLI Flags

Run with custom flags:

```bash
docker run ghcr.io/mikeo7/harborbuddy:latest \
  --interval 1h \
  --dry-run \
  --log-level debug
```

Available flags:

- `--config <path>` - Path to config file (default: `/config/harborbuddy.yml`)
- `--interval <duration>` - Override check interval
- `--once` - Run a single update cycle and exit
- `--dry-run` - Preview changes without applying them
- `--log-level <level>` - Set log level
- `--cleanup-only` - Only run cleanup and exit
- `--version` - Show version and exit

## Container Management

### Update All by Default

HarborBuddy updates **all containers by default**. No special labels or opt-in configuration required.

### Opt-Out for Specific Containers

To prevent HarborBuddy from updating a container, add the label:

```yaml
labels:
  com.harborbuddy.autoupdate: "false"
```

**Common use cases for opt-out:**

- Databases (postgres, mysql, mongodb)
- HarborBuddy itself
- Containers with manual version pinning
- Containers requiring manual upgrade procedures

### Example: Mixed Management

```yaml
services:
  # Auto-updated (no label needed)
  nginx:
    image: nginx:latest
    ports:
      - "80:80"

  # Auto-updated (explicit opt-in, same as default)
  redis:
    image: redis:latest
    labels:
      com.harborbuddy.autoupdate: "true"

  # NOT auto-updated (opt-out)
  postgres:
    image: postgres:15
    labels:
      com.harborbuddy.autoupdate: "false"
    volumes:
      - db_data:/var/lib/postgresql/data
```

## Image Filtering

Control which images can be updated using patterns:

### Allow Patterns

Only update images matching these patterns (default: `["*"]`):

```yaml
updates:
  allow_images:
    - "nginx:*"              # Any nginx tag
    - "ghcr.io/myorg/*"      # Any image from myorg
    - "redis:latest"         # Specific image:tag
```

### Deny Patterns

Never update images matching these patterns:

```yaml
updates:
  deny_images:
    - "postgres:*"           # Never update any postgres
    - "mysql:*"              # Never update any mysql
    - "*/database:*"         # Never update anything named database
```

**Pattern syntax:**

- `*` - Match everything
- `repo:tag` - Exact match
- `repo:*` - Any tag for this repo
- `registry.io/org/*` - Any repo under this path
- `*suffix` - Match by suffix

## Dry-Run Mode

Preview what HarborBuddy would do without making any changes:

```bash
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -e HARBORBUDDY_DRY_RUN=true \
  ghcr.io/mikeo7/harborbuddy:latest --once
```

Dry-run mode will:

- Check for updates
- Log what would be changed
- **Not** pull images
- **Not** restart containers
- **Not** remove images

## Image Cleanup

HarborBuddy can automatically clean up unused images to save disk space:

```yaml
cleanup:
  enabled: true
  min_age_hours: 24        # Only remove images older than 24 hours
  dangling_only: true      # Only remove dangling (untagged) images
```

Set `dangling_only: false` to remove all unused images (not just dangling ones).

## Common Use Cases

### 1. Always-Updated Web Services

```yaml
services:
  web:
    image: myapp:latest
    # No label needed - auto-updated by default
```

### 2. Protected Databases

```yaml
services:
  db:
    image: postgres:15
    labels:
      com.harborbuddy.autoupdate: "false"
    volumes:
      - db_data:/var/lib/postgresql/data
```

### 3. Staging Environment (Aggressive Updates)

```yaml
environment:
  HARBORBUDDY_INTERVAL: "5m"     # Check every 5 minutes
  HARBORBUDDY_LOG_LEVEL: "debug"
```

### 4. Production Environment (Conservative)

```yaml
environment:
  HARBORBUDDY_INTERVAL: "6h"     # Check every 6 hours
updates:
  deny_images:
    - "postgres:*"
    - "mysql:*"
    - "redis:*"
```

### 5. One-Time Update Check

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  ghcr.io/mikeo7/harborbuddy:latest --once
```

### 6. Cleanup Only

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  ghcr.io/mikeo7/harborbuddy:latest --cleanup-only
```

## Building from Source

### Prerequisites

- Go 1.23 or later
- Docker (for building container image)

### Build Binary

```bash
go build -o harborbuddy ./cmd/harborbuddy
```

### Build Docker Image

```bash
docker build -t harborbuddy:latest .
```

### Run Tests

```bash
# Run unit tests
go test ./...

# Run integration tests
cd test/
./test-docker-compose.sh

# Run local tests
./test-local.sh
```

See [test/README.md](test/README.md) for comprehensive testing documentation.

## Architecture

HarborBuddy consists of the following components:

- **Scheduler**: Main loop that runs update cycles at configured intervals
- **Updater**: Discovers containers, checks for updates, and applies them
- **Cleanup**: Removes unused images based on policy
- **Docker Client**: Wrapper around Docker SDK for container and image operations
- **Config**: Three-tier configuration system (file, env, flags)
- **Logger**: Structured logging with context

### Update Cycle Flow

1. **Discovery**: List all running containers
2. **Eligibility**: Check labels and image patterns
3. **Check**: Pull latest image and compare IDs
4. **Apply**: Stop, recreate, and start container with new image
5. **Cleanup**: Remove unused images per policy

## Security Considerations

- **Docker socket access**: HarborBuddy requires read-write access to the Docker socket to manage containers
- **Self-update protection**: Always add `com.harborbuddy.autoupdate: "false"` label to HarborBuddy itself
- **Database protection**: Exclude databases from auto-updates to prevent data loss
- **Rollback**: HarborBuddy does not implement automatic rollback - monitor your containers after updates

## Logging

HarborBuddy provides structured logging with context:

```
2024-12-03T10:00:00Z INF HarborBuddy version 0.1.0 starting
2024-12-03T10:00:00Z INF Docker host: unix:///var/run/docker.sock
2024-12-03T10:00:00Z INF Successfully connected to Docker daemon
2024-12-03T10:00:00Z INF ==== Starting new cycle ====
2024-12-03T10:00:01Z INF Found 5 running containers
2024-12-03T10:00:01Z INF Skipping container harborbuddy: label com.harborbuddy.autoupdate=false
2024-12-03T10:00:02Z INF Checking container nginx for updates
2024-12-03T10:00:03Z INF Container nginx is up to date
2024-12-03T10:00:03Z INF Update cycle complete: 0 updated, 1 skipped
```

Enable JSON logging for parsing:

```yaml
log:
  json: true
```

## Troubleshooting

### HarborBuddy can't connect to Docker

**Problem**: `Failed to create Docker client` or `Failed to ping docker daemon`

**Solution**: Ensure the Docker socket is mounted correctly:

```yaml
volumes:
  - /var/run/docker.sock:/var/run/docker.sock:ro
```

### Container not being updated

**Possible causes**:

1. Check if container has opt-out label:
   ```bash
   docker inspect <container> | grep com.harborbuddy.autoupdate
   ```

2. Check if image matches deny pattern in config

3. Check HarborBuddy logs for eligibility decision:
   ```bash
   docker logs harborbuddy | grep <container-name>
   ```

### Updates fail silently

Enable debug logging:

```yaml
environment:
  HARBORBUDDY_LOG_LEVEL: "debug"
```

## License

See [LICENSE](LICENSE) file.

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Support

- 🐛 [Report a bug](https://github.com/MikeO7/HarborBuddy/issues/new?template=bug_report.md)
- 💡 [Request a feature](https://github.com/MikeO7/HarborBuddy/issues/new?template=feature_request.md)
- 💬 [Discussions](https://github.com/MikeO7/HarborBuddy/discussions)

## Version

Current version: **0.1.0**

## Future Enhancements (Not in v0.1)

- Semantic version comparison and policies
- Health checks and automatic rollback
- Prometheus metrics endpoint
- Web UI for monitoring and control
- Multi-host support
- Kubernetes integration
