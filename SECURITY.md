# Security Policy

## Supported versions

Security fixes are provided for the latest stable release. Users of the `edge` image should move to the next stable release when it becomes available.

| Version | Supported |
| --- | --- |
| Latest stable release | Yes |
| Older releases | No |
| `edge` | Best effort |

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability.

Use a [private GitHub security advisory](https://github.com/MikeO7/HarborBuddy/security/advisories/new). Include affected versions, reproduction steps, impact, and any proposed mitigation. The maintainers will acknowledge a complete report as soon as practical and coordinate disclosure after a fix is available.

## Security model

HarborBuddy controls Docker through the Docker API. Access to `/var/run/docker.sock` or an equivalently privileged remote daemon is effectively host-administrator access. A compromised HarborBuddy container, image, configuration, or Docker endpoint can affect every container and potentially the host.

Use HarborBuddy only with a Docker daemon and image sources you trust.

### Docker access

- Mount the local Docker socket read/write; a read-only mount cannot support updates or self-update.
- Restrict access to the Compose file, configuration, Docker socket, and remote-daemon client certificates.
- For remote TLS, use the Docker SDK's standard `DOCKER_HOST`, `DOCKER_TLS_VERIFY`, and `DOCKER_CERT_PATH` variables.
- Verify the remote daemon's identity and protect its network path.
- Do not expose an unauthenticated Docker TCP endpoint.

HarborBuddy does not add an authorization boundary in front of Docker and does not provide separate registry credential management.

### Container replacement and rollback

Ordinary updates preserve the old container as a temporary backup until the replacement is ready. If replacement startup or readiness fails, HarborBuddy attempts to remove the failed replacement, restore the old name and networks, and restart the old container.

Rollback is best effort because Docker or the host may fail during recovery. Monitor update results and keep independent backups for stateful services. HarborBuddy intentionally skips auto-remove containers, Swarm task containers, and container network namespace dependencies that it cannot recreate safely.

### Self-update

Automatic self-update is enabled by default and uses a short-lived helper container because the daemon cannot replace itself in place. The helper requires the same privileged Docker access, preserves the old daemon as a backup during the transaction, and restores it if the replacement does not become ready. Set `HARBORBUDDY_SELF_UPDATE_ENABLED=false` when HarborBuddy itself must remain under manual change control; this does not disable updates for other eligible containers.

Keep `com.harborbuddy.role=daemon` on the HarborBuddy service. Review daemon and helper logs after a failed self-update. If needed, recover manually with:

```bash
docker compose pull harborbuddy
docker compose up -d --no-deps harborbuddy
```

### Dry-run

Dry-run pulls eligible image references to compare image IDs. Pulling changes the Docker Engine's local image cache and consumes registry/network resources. Dry-run does not recreate or delete containers and does not delete images.

### Image and release verification

Published images are built from a scratch runtime and contain the static HarborBuddy binary, CA certificates, and timezone data. Release publication includes SBOM and provenance attestations in the container registry. The build inputs and Go builder are digest-pinned, and Trivy blocks fixed HIGH and CRITICAL image vulnerabilities before publication. Use immutable semantic-version or digest references where change control requires them; `latest` moves with stable releases and `edge` moves with `main`.

CI also runs pinned golangci-lint, govulncheck, Gitleaks, Actionlint, ShellCheck, Hadolint, and yamllint checks. Scanner success is not proof that no vulnerability or secret exists. The reviewed govulncheck exceptions are limited to unfixed Moby daemon and `docker cp` advisories whose affected APIs HarborBuddy does not provide or invoke; any new advisory remains blocking.

Never place Docker authorization files, registry credentials, client certificates, private keys, tokens, or complete environment dumps in issues, pull requests, logs, or CI artifacts. If a credential is disclosed, rotate or revoke it immediately before attempting history cleanup, then report the disclosure privately. Deleting an unrotated secret from Git history does not make it safe.

GitHub code-scanning presentation and native secret push protection depend on repository plan and settings. CI retains scanner artifacts and blocks findings independently where possible.

### Operational recommendations

- Exclude databases and services with manual migration requirements using `com.harborbuddy.autoupdate=false`.
- Use container health checks so readiness and rollback decisions have an application signal.
- Start with dry-run and conservative allow/deny lists.
- Schedule updates during a monitored maintenance window.
- Keep host-level backups and tested recovery procedures.
- Prefer least-exposed Docker networking and narrowly controlled client certificates.
- Monitor structured logs for failed updates, rollback errors, and leftover backups.

## Security updates

Security advisories and fixed releases are published through:

- [GitHub Security Advisories](https://github.com/MikeO7/HarborBuddy/security/advisories)
- [GitHub Releases](https://github.com/MikeO7/HarborBuddy/releases)
- `ghcr.io/mikeo7/harborbuddy` release tags
