# HarborBuddy Quick Start Guide

## Installation

### Option 1: Docker Compose (Recommended)

1. Create `docker-compose.yml`:

```yaml
services:
  harborbuddy:
    image: ghcr.io/mikeo/harborbuddy:latest
    container_name: harborbuddy
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      HARBORBUDDY_INTERVAL: "30m"
    labels:
      com.harborbuddy.autoupdate: "false"
```

2. Start:

```bash
docker-compose up -d
```

3. View logs:

```bash
docker-compose logs -f harborbuddy
```

### Option 2: Docker Run

```bash
docker run -d \
  --name harborbuddy \
  --restart unless-stopped \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -e HARBORBUDDY_INTERVAL=30m \
  -l com.harborbuddy.autoupdate=false \
  ghcr.io/mikeo/harborbuddy:latest
```

## Common Commands

### Dry-Run (Preview Changes)

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  ghcr.io/mikeo/harborbuddy:latest \
  --dry-run --once --log-level debug
```

### Run Once (Single Update Check)

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  ghcr.io/mikeo/harborbuddy:latest --once
```

### Cleanup Only

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  ghcr.io/mikeo/harborbuddy:latest --cleanup-only
```

### Custom Interval

```bash
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -e HARBORBUDDY_INTERVAL=15m \
  ghcr.io/mikeo/harborbuddy:latest
```

## Container Management

### Prevent Auto-Update (Opt-Out)

Add this label to containers you don't want updated:

```yaml
labels:
  com.harborbuddy.autoupdate: "false"
```

**Always exclude:**
- HarborBuddy itself
- Databases (postgres, mysql, mongodb)
- Stateful services requiring manual upgrades

### Examples

#### Auto-updated container (default):

```yaml
nginx:
  image: nginx:latest
  ports:
    - "80:80"
```

#### Protected container:

```yaml
postgres:
  image: postgres:15
  labels:
    com.harborbuddy.autoupdate: "false"
  volumes:
    - db_data:/var/lib/postgresql/data
```

## Configuration File

Create `harborbuddy.yml`:

```yaml
docker:
  host: "unix:///var/run/docker.sock"

updates:
  enabled: true
  check_interval: "30m"
  dry_run: false
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

Mount it:

```yaml
volumes:
  - ./harborbuddy.yml:/config/harborbuddy.yml:ro
```

## Environment Variables

Quick configuration via environment variables:

```yaml
environment:
  HARBORBUDDY_INTERVAL: "1h"          # Check every hour
  HARBORBUDDY_DRY_RUN: "true"         # Preview mode
  HARBORBUDDY_LOG_LEVEL: "debug"      # Verbose logging
  HARBORBUDDY_LOG_JSON: "false"       # Human-readable logs
```

## Monitoring

### View Logs

```bash
docker logs harborbuddy
docker logs -f harborbuddy  # Follow mode
docker logs --tail 100 harborbuddy  # Last 100 lines
```

### Check Status

```bash
docker ps | grep harborbuddy
docker inspect harborbuddy
```

## Troubleshooting

### Connection Issues

**Problem:** Can't connect to Docker

**Solution:** Verify socket mount:

```bash
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock:ro \
  ghcr.io/mikeo/harborbuddy:latest --version
```

### Container Not Updating

**Check label:**

```bash
docker inspect <container> | grep com.harborbuddy.autoupdate
```

**Check logs:**

```bash
docker logs harborbuddy | grep <container-name>
```

**Enable debug:**

```bash
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -e HARBORBUDDY_LOG_LEVEL=debug \
  ghcr.io/mikeo/harborbuddy:latest
```

## Tips

1. **Start with dry-run**: Test with `--dry-run --once` first
2. **Monitor logs**: Watch logs after deployment for a few cycles
3. **Protect databases**: Always exclude stateful services
4. **Test updates**: Use staging environment before production
5. **Set reasonable intervals**: 30m-6h depending on environment

## Next Steps

- Read full [README.md](README.md) for detailed documentation
- Check [examples/](examples/) for complete configurations
- See [CONTRIBUTING.md](CONTRIBUTING.md) for development guide

## Support

- Issues: https://github.com/mikeo/harborbuddy/issues
- Discussions: https://github.com/mikeo/harborbuddy/discussions

