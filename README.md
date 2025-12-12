# HarborBuddy ⚓️

[![CI](https://github.com/MikeO7/HarborBuddy/actions/workflows/ci.yml/badge.svg)](https://github.com/MikeO7/HarborBuddy/actions/workflows/ci.yml)
[![Docker Build](https://github.com/MikeO7/HarborBuddy/actions/workflows/docker-build.yml/badge.svg)](https://github.com/MikeO7/HarborBuddy/actions/workflows/docker-build.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)

**The easiest way to keep your Docker containers updated automatically.**

HarborBuddy acts as your automated DevOps assistant, ensuring your containers are always running the latest versions of their images. It watches your running containers, detects new images on the registry, and updates them seamlessly—cleanup included!

Perfect for **Home Labs**, **Staging Environments**, and **Production** setups where you want "set and forget" maintenance. A modern, lightweight alternative to Watchtower.

---

## 🚀 Why HarborBuddy?

- **Set and Forget**: Install it once, and your containers stay fresh automatically.
- **Safe by Default**: Easily exclude critical services (like databases) using simple labels.
- **Save Disk Space**: Automatically cleans up old, unused images after updates.
- **Peace of Mind**: "Dry Run" mode lets you see exactly what *would* happen without making changes.
- **Zero Config**: Works out of the box with sensible defaults. No complex setup required.

## ✨ Features

- **Automated Updates**: Polls for new Docker images at your chosen interval.
- **Smart Cleanup**: Removes "dangling" and unused images to keep your host clean.
- **Flexible Control**: Update everything by default, or opt-out specific containers.
- **Pattern Filtering**: Allow or Deny updates based on image names (e.g., "never update `postgres:*`").
- **Lightweight**: Written in Go, runs as a tiny container (~10MB) with minimal resource usage.
- **Docker Native**: Uses the official Docker API for reliable operations.

## ⚡️ Quick Start

You can get running in seconds. HarborBuddy connects to your Docker socket to manage updates.

### 1. The "Zero-Config" Setup (Docker Compose)

Add this service to your `docker-compose.yml` to start updating all your containers every 30 minutes.

```yaml
services:
  harborbuddy:
    image: ghcr.io/mikeo7/harborbuddy:latest
    container_name: harborbuddy
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    environment:
      - HARBORBUDDY_INTERVAL=30m
```

### 2. Docker CLI One-Liner

Prefer the command line? Run this:

```bash
docker run -d \
  --name harborbuddy \
  --restart unless-stopped \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e HARBORBUDDY_INTERVAL=1h \
  ghcr.io/mikeo7/harborbuddy:latest
```

That's it! HarborBuddy is now monitoring your containers. check the logs with `docker logs -f harborbuddy` to see it in action.

## 🧠 How it Works

1.  **Check**: Every interval (default: 30m), HarborBuddy scans your running containers.
2.  **Verify**: It checks the remote registry (Docker Hub, GHCR, etc.) for a newer image digest.
3.  **Update**: If a new version exists, it pulls the image, stops the container, and recreates it with the same settings.
4.  **Clean**: Finally, it removes the old image version to free up space.

## 🛡️ Preventing Updates (Opt-Out)

Sometimes you don't want a container to update automatically (e.g., a production database or a specific app version).

**To ignore a container, add this label:**

```yaml
labels:
  com.harborbuddy.autoupdate: "false"
```

**Example:**

```yaml
services:
  # This container will Auto-Update
  my-app:
    image: my-app:latest

  # This container will NEVER update
  postgres:
    image: postgres:15
    labels:
      com.harborbuddy.autoupdate: "false"
```

*Note: You should also add this label to HarborBuddy itself if you want to update it manually!*

## ⚙️ Configuration

HarborBuddy is highly configurable via Environment Variables.

| Variable | Default | Description |
|----------|---------|-------------|
| `HARBORBUDDY_INTERVAL` | `30m` | How often to check for updates (e.g., `15m`, `1h`, `6h`, `24h`). |
| `HARBORBUDDY_DRY_RUN` | `false` | If `true`, logs what *would* happen but makes no changes. Great for testing! |
| `HARBORBUDDY_LOG_LEVEL` | `info` | Control log detail (`debug`, `info`, `warn`, `error`). |
| `HARBORBUDDY_LOG_JSON` | `false` | Output logs in JSON format for tools like Splunk/ELK. |

### Advanced Configuration (File Based)

For complex setups (like "Update `nginx` but never `mysql`"), you can use a `harborbuddy.yml` file.

1. Create a config file `harborbuddy.yml`:
   ```yaml
   updates:
     deny_images:
       - "postgres:*"
       - "mysql:*"
   cleanup:
     min_age_hours: 24  # Only delete images older than 24h
   ```

2. Mount it into the container:
   ```yaml
   volumes:
     - ./harborbuddy.yml:/config/harborbuddy.yml:ro
   ```

## ❓ FAQ

**Q: Will this restart my containers?**
A: Yes. To apply a Docker image update, the container must be recreated. HarborBuddy does this quickly to minimize downtime.

**Q: Does it support private registries?**
A: Yes! As long as the host machine has credentials (e.g., you ran `docker login`), HarborBuddy can pull the images.

**Q: Is it safe for production?**
A: Yes, but for critical production databases, we strictly recommend using specific version tags (e.g., `postgres:14.5` instead of `latest`) and using the **Opt-Out label** described above.

**Q: How do I see what it's doing?**
A: Check the logs! `docker logs -f harborbuddy`. It provides clear, human-readable steps of what it is checking and updating.

## 🤝 Contributing

We welcome contributions from everyone!

- 🐛 [Report a Bug](https://github.com/MikeO7/HarborBuddy/issues)
- 💡 [Request a Feature](https://github.com/MikeO7/HarborBuddy/issues)
- 👩‍💻 [Submit a Pull Request](CONTRIBUTING.md)

## 📄 License

HarborBuddy is open-source software licensed under the [MIT License](LICENSE).
