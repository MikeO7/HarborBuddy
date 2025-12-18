# HarborBuddy ⚓️

**Automatic Docker Container Updates Made Simple**

[![CI](https://github.com/MikeO7/HarborBuddy/actions/workflows/ci.yml/badge.svg)](https://github.com/MikeO7/HarborBuddy/actions/workflows/ci.yml)
[![Docker Build](https://github.com/MikeO7/HarborBuddy/actions/workflows/docker-build.yml/badge.svg)](https://github.com/MikeO7/HarborBuddy/actions/workflows/docker-build.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)

HarborBuddy automatically keeps your Docker containers up-to-date. It monitors running containers, detects new image versions, and seamlessly updates them—including cleanup of old images.

**Perfect for:**
- 🏠 **Home Labs** — Keep Plex, Home Assistant, Pi-hole, and more updated automatically
- 🧪 **Staging/Dev Environments** — Always run the latest versions
- 🚀 **Production** — Scheduled updates during maintenance windows

---

## Table of Contents

1. [Quick Start (30 seconds)](#-quick-start-30-seconds)
2. [Docker Compose Examples](#-docker-compose-examples)
   - [Basic Setup](#1-basic-setup-update-every-30-minutes)
   - [Daily Scheduled Updates](#2-scheduled-updates-daily-at-3am)
   - [Home Lab Setup](#3-home-lab-setup)
   - [Production Setup with Logging](#4-production-setup-with-file-logging)
3. [How It Works](#-how-it-works)
4. [Environment Variables Reference](#%EF%B8%8F-environment-variables-reference)
5. [Container Labels](#-container-labels)
6. [Configuration File (Advanced)](#-configuration-file-advanced)
7. [Logging & Persistence](#-logging--persistence)
8. [Private Registries](#-private-registries)
9. [Self-Update Feature](#-self-update-feature)
10. [FAQ](#-frequently-asked-questions)
11. [Contributing](#-contributing)

---

## 🚀 Quick Start (30 seconds)

Add HarborBuddy to your `docker-compose.yml`:

```yaml
services:
  harborbuddy:
    image: ghcr.io/mikeo7/harborbuddy:latest
    container_name: harborbuddy
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
```

Run it:

```bash
docker compose up -d
```

**That's it!** HarborBuddy now checks for updates every 12 hours and updates all your containers automatically.

---

## 📦 Docker Compose Examples

### 1. Basic Setup (Update Every 12 Hours)

The simplest setup—checks for updates every 12 hours (default):

```yaml
services:
  harborbuddy:
    image: ghcr.io/mikeo7/harborbuddy:latest
    container_name: harborbuddy
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    environment:
      - HARBORBUDDY_INTERVAL=12h
```

---

### 2. Scheduled Updates (Daily at 3AM)

**Recommended for most users.** Updates run once per day at a specific time:

```yaml
services:
  harborbuddy:
    image: ghcr.io/mikeo7/harborbuddy:latest
    container_name: harborbuddy
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    environment:
      # Set your timezone
      - TZ=America/New_York
      # Run updates daily at 3:00 AM
      - HARBORBUDDY_SCHEDULE_TIME=03:00
```

> **How Timezone Works:** Use standard IANA timezone names like `America/New_York`, `Europe/London`, `Asia/Tokyo`, or `UTC`. [Find your timezone →](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones)

---

### 3. Home Lab Setup

Common home lab configuration with protected databases:

```yaml
services:
  # HarborBuddy - manages all container updates
  harborbuddy:
    image: ghcr.io/mikeo7/harborbuddy:latest
    container_name: harborbuddy
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    environment:
      - TZ=America/Los_Angeles
      - HARBORBUDDY_SCHEDULE_TIME=03:00
      - HARBORBUDDY_LOG_LEVEL=info


  # Plex - WILL be auto-updated
  plex:
    image: linuxserver/plex:latest
    container_name: plex
    restart: unless-stopped
    # ... your plex config

  # Home Assistant - WILL be auto-updated
  homeassistant:
    image: ghcr.io/home-assistant/home-assistant:stable
    container_name: homeassistant
    restart: unless-stopped
    # ... your home assistant config

  # PostgreSQL - Will NOT be updated (protected)
  postgres:
    image: postgres:15
    container_name: postgres
    restart: unless-stopped
    labels:
      com.harborbuddy.autoupdate: "false"
    # ... your postgres config
```

---

### 4. Production Setup with File Logging

Enterprise-ready configuration with persistent logs:

```yaml
services:
  harborbuddy:
    image: ghcr.io/mikeo7/harborbuddy:latest
    container_name: harborbuddy
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      # Persist logs to host
      - ./logs:/logs
      # Optional: Use a config file for advanced settings
      - ./harborbuddy.yml:/config/harborbuddy.yml:ro
    environment:
      - TZ=UTC
      - HARBORBUDDY_SCHEDULE_TIME=03:00
      - HARBORBUDDY_LOG_LEVEL=info
      - HARBORBUDDY_LOG_JSON=true

```

---

### 5. Test Mode (Dry Run)

Preview what would be updated without making any changes:

```yaml
services:
  harborbuddy:
    image: ghcr.io/mikeo7/harborbuddy:latest
    container_name: harborbuddy
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    environment:
      - HARBORBUDDY_INTERVAL=5m
      # Enable dry run - logs changes but doesn't apply them
      - HARBORBUDDY_DRY_RUN=true
      - HARBORBUDDY_LOG_LEVEL=debug
```

Check the logs to see what would happen:

```bash
docker logs -f harborbuddy
```

---

## 🧠 How It Works

```
┌─────────────────────────────────────────────────────────────┐
│                    HarborBuddy Update Cycle                 │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  1. SCAN                                                    │
│     Lists all running containers on the Docker host         │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  2. CHECK                                                   │
│     Compares local image digests with remote registry       │
│     (Docker Hub, GHCR, private registries, etc.)            │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  3. UPDATE                                                  │
│     If newer version exists:                                │
│     • Pull new image                                        │
│     • Stop container gracefully                             │
│     • Recreate with same settings                           │
│     • Start new container                                   │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  4. CLEANUP                                                 │
│     Removes old/dangling images to save disk space          │
└─────────────────────────────────────────────────────────────┘
```

---

## ⚙️ Environment Variables Reference

All configuration can be done via environment variables. These override any config file settings.

### Scheduling

| Variable | Default | Description |
|----------|---------|-------------|
| `HARBORBUDDY_INTERVAL` | `12h` | How often to check for updates. **Examples:** `1h`, `6h`, `12h`, `24h` |
| `HARBORBUDDY_SCHEDULE_TIME` | *(empty)* | Run updates at a specific time daily (24-hour format). **Examples:** `03:00`, `14:30` |
| `HARBORBUDDY_TIMEZONE` | `UTC` | Timezone for scheduled updates. Also supports standard `TZ` variable. **Examples:** `America/New_York`, `Europe/London`, `Asia/Tokyo` |
| `TZ` | *(system)* | Standard Docker timezone variable. `HARBORBUDDY_TIMEZONE` takes priority if both are set. |

> **Note:** If `HARBORBUDDY_SCHEDULE_TIME` is set, it overrides `HARBORBUDDY_INTERVAL`. The update will run once per day at the specified time.

### Behavior

| Variable | Default | Possible Values | Description |
|----------|---------|-----------------|-------------|
| `HARBORBUDDY_DRY_RUN` | `false` | `true`, `false` | Preview mode. Logs what would be updated without making changes. Great for testing! |
| `HARBORBUDDY_UPDATES_ENABLED` | `true` | `true`, `false` | Enable/disable container updates. Set to `false` to only run cleanup. |
| `HARBORBUDDY_CLEANUP_ENABLED` | `true` | `true`, `false` | Enable/disable automatic cleanup of old images. |
| `HARBORBUDDY_STOP_TIMEOUT` | `10s` | Duration (e.g., `30s`, `1m`) | How long to wait for containers to stop gracefully before force-killing. |

### Logging

| Variable | Default | Possible Values | Description |
|----------|---------|-----------------|-------------|
| `HARBORBUDDY_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` | Verbosity of logs. Use `debug` for troubleshooting. |
| `HARBORBUDDY_LOG_JSON` | `false` | `true`, `false` | Output logs in JSON format for log aggregators (ELK, Splunk, Loki). |
| `HARBORBUDDY_LOG_FILE` | *(auto)* | Absolute path | Custom log file path. Default: `/logs/harborbuddy.log` if `/logs` is mounted. |
| `HARBORBUDDY_LOG_MAX_SIZE` | `10` | Integer (MB) | Maximum log file size before rotation. |
| `HARBORBUDDY_LOG_MAX_BACKUPS` | `1` | Integer | Number of rotated log files to keep. |

### Docker Connection

| Variable | Default | Description |
|----------|---------|-------------|
| `HARBORBUDDY_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker socket path. For remote Docker: `tcp://hostname:2376` |

---

## 🏷️ Container Labels

Use Docker labels to control which containers get updated.

### Exclude a Container from Updates

Add this label to any container you don't want HarborBuddy to update:

```yaml
labels:
  com.harborbuddy.autoupdate: "false"
```

### Full Example

```yaml
services:
  # This container WILL be auto-updated (default behavior)
  nginx:
    image: nginx:latest
    container_name: nginx

  # This container will NOT be auto-updated
  mysql:
    image: mysql:8
    container_name: mysql
    labels:
      com.harborbuddy.autoupdate: "false"

  # HarborBuddy updates itself by default!
  harborbuddy:
    image: ghcr.io/mikeo7/harborbuddy:latest
    container_name: harborbuddy
```

### What to Protect

We recommend adding the opt-out label to:

| Container Type | Reason |
|----------------|--------|
| **Databases** (PostgreSQL, MySQL, MongoDB) | Major version updates may require data migration |
| **Stateful applications** | May have upgrade procedures |
| **Pinned versions** | Containers using specific version tags (e.g., `app:1.2.3`) |

---

## 📁 Configuration File (Advanced)

For complex setups, you can use a YAML configuration file instead of (or in addition to) environment variables.

### Create `harborbuddy.yml`:

```yaml
# Image filtering - control what gets updated
updates:
  # Never update these images (wildcards supported)
  deny_images:
    - "postgres:*"
    - "mysql:*"
    - "redis:*"
  
  # Only update these images (default: all)
  # allow_images:
  #   - "nginx:*"
  #   - "my-app:*"

# Cleanup settings
cleanup:
  enabled: true
  min_age_hours: 24      # Only delete images older than 24 hours
  dangling_only: true    # Only remove untagged images

# Logging
log:
  level: info
  json: false
```

### Mount the config file:

```yaml
services:
  harborbuddy:
    image: ghcr.io/mikeo7/harborbuddy:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./harborbuddy.yml:/config/harborbuddy.yml:ro
```

> **Priority:** Environment variables always override config file settings.

---

## 📝 Logging & Persistence

### Enable Persistent Logs

Mount a volume to save logs to your host system:

```yaml
volumes:
  - ./logs:/logs
```

HarborBuddy will automatically:
- Write logs to `/logs/harborbuddy.log`
- Rotate logs when they reach 10MB
- Keep 1 backup file

### View Logs

```bash
# Stream live logs
docker logs -f harborbuddy

# View the log file (if volume mounted)
tail -f ./logs/harborbuddy.log
```

### JSON Logs for Log Aggregators

For Elasticsearch, Loki, Splunk, or other log aggregators:

```yaml
environment:
  - HARBORBUDDY_LOG_JSON=true
```

Output format:

```json
{"level":"info","time":"2024-01-15T03:00:00Z","msg":"Update check starting","containers":12}
{"level":"info","time":"2024-01-15T03:00:05Z","msg":"Updated container","container":"nginx","old_image":"abc123","new_image":"def456"}
```

---

## 🔐 Private Registries

HarborBuddy uses your Docker host's credentials to access private registries.

### Setup

1. Log in to your registry on the Docker host:

```bash
docker login ghcr.io
docker login registry.example.com
```

2. HarborBuddy will automatically use these credentials when pulling images.

### For Docker Compose

If running HarborBuddy in Docker Compose, mount your Docker config:

```yaml
volumes:
  - /var/run/docker.sock:/var/run/docker.sock
  # Optional: Mount Docker config for private registry auth
  - ~/.docker/config.json:/root/.docker/config.json:ro
```

---

## 🔄 Self-Update Feature

HarborBuddy includes a robust **Self-Update** feature. When a new version of HarborBuddy is released, it detects the update and:

1.  Spawns a temporary "updater" container.
2.  Gracefully stops the running HarborBuddy instance.
3.  Recreates HarborBuddy with the new image version.
4.  Cleans up the temporary updater.

This ensures you're always running the latest version with new features and bug fixes without manual intervention 🚀.

**If you prefer to update manually**, you can opt-out:

```yaml
labels:
  com.harborbuddy.autoupdate: "false"
```

---

## ❓ Frequently Asked Questions

<details>
<summary><b>Will this restart my containers?</b></summary>

Yes. To apply a Docker image update, the container must be recreated. HarborBuddy does this quickly to minimize downtime. The container is stopped gracefully (respecting your `stop_grace_period`), then recreated with identical settings.

</details>

<details>
<summary><b>Does it work with Docker Swarm or Kubernetes?</b></summary>

HarborBuddy is designed for standalone Docker hosts. For Kubernetes, consider tools like [Renovate](https://github.com/renovatebot/renovate) or [Keel](https://keel.sh/). Docker Swarm support may come in future versions.

</details>

<details>
<summary><b>Is it safe for production databases?</b></summary>

We recommend **excluding databases** from auto-updates using the `com.harborbuddy.autoupdate: "false"` label. Database major version upgrades often require migration steps that HarborBuddy cannot handle.

</details>

<details>
<summary><b>How do I check what HarborBuddy is doing?</b></summary>

```bash
docker logs -f harborbuddy
```

The logs use clear visual indicators:
- 🔍 Checking for updates
- 🚀 Updating a container
- ✅ Update complete
- 🗑️ Cleaning up old images

</details>

<details>
<summary><b>What happens if an update fails?</b></summary>

If HarborBuddy can't pull an image or start a container, it logs the error and continues with other containers. Your existing container remains running.

</details>

<details>
<summary><b>Can I update containers on a remote Docker host?</b></summary>

Yes! Use the `HARBORBUDDY_DOCKER_HOST` environment variable:

```yaml
environment:
  - HARBORBUDDY_DOCKER_HOST=tcp://192.168.1.100:2376
```

</details>

<details>
<summary><b>How is this different from Watchtower?</b></summary>

HarborBuddy is a modern, lightweight alternative to Watchtower with:
- Smaller image size (~10MB)
- Scheduled updates (not just intervals)
- Built-in image cleanup
- Self-update capability
- Written in Go with extensive tests

</details>

---

## 🤝 Contributing

We welcome contributions!

- 🐛 [Report a Bug](https://github.com/MikeO7/HarborBuddy/issues)
- 💡 [Request a Feature](https://github.com/MikeO7/HarborBuddy/issues)
- 👩‍💻 [Submit a Pull Request](CONTRIBUTING.md)

### Development

```bash
# Run tests
make test
# OR
go test ./...

# Build locally
make build
```

---

## 📄 License

HarborBuddy is open-source software licensed under the [MIT License](LICENSE).

---

<p align="center">
  <b>Keep your containers fresh with HarborBuddy ⚓️</b>
  <br>
  <a href="https://github.com/MikeO7/HarborBuddy">GitHub</a> •
  <a href="https://github.com/MikeO7/HarborBuddy/issues">Issues</a> •
  <a href="CHANGELOG.md">Changelog</a>
</p>
