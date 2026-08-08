# Local verification and integration tests

`make verify-local` is the comprehensive developer suite. It combines the Go and repository-policy checks with `test/integration.sh`, the disposable Docker-compatible runtime test.

## Requirements

- A Unix-like shell with Bash
- A running local Docker Engine or Podman machine
- Network access to pull the temporary `registry:2` and `busybox:1.36` test dependencies

The integration script requires read/write engine access. Do not run it against a shared or production engine.

## Run it

From the repository root:

```bash
make verify-local
```

To run only the runtime integration scenarios:

```bash
make test-integration
```

The Makefile auto-detects Docker, a `podman` on `PATH`, or the standard macOS package path `/opt/podman/bin/podman`. Override it explicitly when needed:

```bash
make verify-local CONTAINER_ENGINE=/opt/podman/bin/podman
ENGINE_SOCKET=/run/user/501/podman/podman.sock make test-integration CONTAINER_ENGINE=podman
```

## What it verifies

The script:

1. Builds the HarborBuddy image with deterministic version, commit, and date arguments.
2. Runs `/harborbuddy --version` from the scratch image.
3. Verifies the image carries `com.harborbuddy.role=daemon`.
4. Starts an isolated local registry and publishes an initial and healthy replacement revision under one mutable test tag.
5. Starts a target container from the initial revision.
6. Runs one HarborBuddy dry-run cycle with `HARBORBUDDY_SELF_UPDATE_ENABLED=false` and an allow-list containing only the target image.
7. Confirms HarborBuddy reports the healthy replacement while leaving the target container ID, image ID, running state, and old image unchanged.
8. Runs a real update and confirms the target is replaced under its original name, remains running, and uses the healthy replacement image.
9. Publishes an unhealthy revision with a failing Docker health check.
10. Confirms readiness failure is reported, the healthy container and image are restored, and the failed replacement is removed.
11. Publishes two HarborBuddy revisions under one mutable tag and starts the first with an `unless-stopped` restart policy.
12. Confirms the daemon launches its helper, replaces itself with the second image, remains running under the original name, preserves its restart policy, and removes helper/backup containers.
13. Removes its uniquely named containers and generated fixture/HarborBuddy images. Pulled dependency images may remain in the engine cache.

This is an end-to-end test for the image, Docker-compatible connection, discovery, dry-run non-mutation, successful ordinary replacement, readiness failure, rollback, and automatic self-update. Focused Go tests cover remote TLS, scheduler timing, identity ambiguity, and individual rollback boundaries.

## Other checks

```bash
make fmt-check source-limits
make vet lint vuln
make test-cover test-race test-fuzz build
make lint-nongo
```

CI also builds and runs the scratch image for `linux/amd64`. The scheduled multi-platform workflow builds and runs it under QEMU for `linux/amd64`, `linux/arm64`, and `linux/arm/v7`.

## Troubleshooting

### Engine unavailable

For Docker, start Docker Engine or Docker Desktop and verify:

```bash
docker info
docker version
```

For Podman on macOS, a desktop installation alone is not enough; initialize and start a machine, then verify it:

```bash
/opt/podman/bin/podman machine init  # first use only
/opt/podman/bin/podman machine start
/opt/podman/bin/podman info
```

The script obtains Podman's Docker-compatible socket from `podman info`. Set `ENGINE_SOCKET` when using a nonstandard socket.

### Local registry pull fails

The test publishes to a loopback registry on a random port. Docker and Podman normally permit insecure loopback registries. If your engine policy disables them, run the test in a disposable environment where a loopback registry is allowed.

### Cleanup after interruption

The script installs an exit trap and uses unique names. If the process is killed before the trap runs, list resources with:

```bash
docker ps -a --filter label=com.harborbuddy.integration=true
docker image ls --filter label=com.harborbuddy.integration=true
```

Remove only resources carrying that test label; the script never calls a global prune command.
