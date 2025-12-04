# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, please report security vulnerabilities by:

1. Opening a [Security Advisory](https://github.com/MikeO7/HarborBuddy/security/advisories/new) on GitHub
2. Or emailing the maintainers directly (see repository)

You should receive a response within 48 hours. If for some reason you do not, please follow up to ensure we received your original message.

## Security Considerations

### HarborBuddy Security Model

HarborBuddy requires access to the Docker socket to manage containers. This is a powerful permission. Please consider:

1. **Docker Socket Access**: HarborBuddy needs read/write access to `/var/run/docker.sock` to manage containers
2. **Container Recreation**: HarborBuddy stops and recreates containers during updates
3. **Image Pulls**: HarborBuddy pulls images from registries (ensure you trust your image sources)
4. **Dry-Run Mode**: Always test in `--dry-run` mode first in production environments

### Best Practices

1. **Exclude Critical Services**: Use `com.harborbuddy.autoupdate="false"` label on databases and critical services
2. **Private Registries**: Use Docker authentication for private registries
3. **Network Isolation**: Run HarborBuddy in an appropriate network context
4. **Monitoring**: Monitor HarborBuddy logs for unexpected behavior
5. **Updates**: Keep HarborBuddy updated to receive security patches

### Minimal Attack Surface

HarborBuddy is built with security in mind:

- Built from `scratch` base image (no OS, no shell, minimal dependencies)
- Static binary with no external dependencies
- Uses official Docker SDK
- All dependencies regularly updated and scanned

## Known Security Considerations

### Docker Socket Permissions

Mounting the Docker socket gives container management capabilities equivalent to root access. This is necessary for HarborBuddy's functionality but should be understood:

- HarborBuddy can manage any container on the host
- Protect access to the Docker socket appropriately
- Consider using Docker's authorization plugins for additional control

### Container Updates

During updates, containers are stopped and recreated:

- Brief service interruption during updates
- Ensure proper health checks in your applications
- Use appropriate restart policies

## Security Updates

Security updates will be released as soon as possible after a vulnerability is confirmed. Check:

- [GitHub Security Advisories](https://github.com/MikeO7/HarborBuddy/security/advisories)
- [Releases Page](https://github.com/MikeO7/HarborBuddy/releases)
- Watch this repository for notifications

