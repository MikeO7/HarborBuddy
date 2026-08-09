# Contributing to HarborBuddy

Thank you for contributing. Open an issue before beginning a large behavioral change so the safety model and compatibility impact can be discussed first.

## Prerequisites

- Go version declared in `go.mod`
- Docker Engine, or Podman for local compatibility testing, for container work and Dockerfile linting
- Make
- Bash and ShellCheck 0.11 for maintained scripts
- `pipx` for the pinned yamllint target

The Make targets download pinned Go-based tools such as golangci-lint, govulncheck, and Actionlint; do not substitute arbitrary locally installed versions.

## Set up the repository

```bash
git clone https://github.com/MikeO7/HarborBuddy.git
cd HarborBuddy
go mod download
make build
```

Do not run `go mod tidy` as an incidental build step. Run `make tidy` only when a dependency change requires an intentional `go.mod` or `go.sum` update.

## Development checks

Run the checks relevant to your change:

```bash
make verify-local
```

The quality policy caps production Go files at 300 physical lines, functions at 100 lines and 60 statements, and cyclomatic complexity at 15. Test files may reach 400 lines, while correctly marked generated files receive only the documented generated-code exemptions. Per-package coverage floors are ratcheted in `test/coverage-baseline.txt` in addition to the repository-wide 100% minimum.

For Docker-facing changes, also run:

```bash
make docker-build
make test-integration
# Select an engine explicitly when needed:
make test-integration CONTAINER_ENGINE=/opt/podman/bin/podman
```

The integration script requires read/write access to a disposable Docker or Podman socket. It creates uniquely named containers and a temporary local registry, verifies image metadata and dry-run non-mutation, performs a healthy replacement, confirms that an unhealthy replacement rolls back, and exercises the full automatic self-update helper lifecycle. Podman is a local Docker-compatibility test engine; standalone Docker Engine remains the production target.

## Build metadata

Local builds default to version `dev`. Release and CI builds pass deterministic values into `internal/buildinfo`:

```bash
make build \
  VERSION=1.2.3 \
  COMMIT="$(git rev-parse HEAD)" \
  DATE="$(git show -s --format=%cI HEAD)"
```

Use the same values for a container build:

```bash
make docker-build VERSION=1.2.3 TAG=1.2.3
```

The build date must come from source control or another reproducible source, not the current wall-clock time.

## Project layout

```text
cmd/harborbuddy/       CLI entry point
internal/buildinfo/    injected version metadata
internal/cleanup/      image cleanup policy
internal/config/       strict configuration loading and validation
internal/docker/       Docker SDK adapter and transactional replacement
internal/scheduler/    interval and daily scheduling
internal/selfupdate/   helper-container self-update lifecycle
internal/updater/      discovery, filtering, pulls, and update decisions
pkg/                   shared packages
examples/              supported Compose and YAML examples
test/integration.sh    Docker integration smoke test
.github/workflows/     CI, image publication, release, and platform smoke tests
```

## Safety expectations

Changes to update behavior must preserve these properties unless an approved design explicitly replaces them:

- Dry-run may pull images but must not recreate, rename, stop, or remove containers and must not delete images.
- A target is re-inspected immediately before replacement to detect external changes.
- The old container remains recoverable until the replacement passes readiness checks.
- Failed replacements roll back the original name, networks, and running state where Docker permits it.
- Auto-remove containers, Swarm tasks, and container-network-namespace dependents are rejected rather than recreated unsafely.
- The HarborBuddy daemon uses `com.harborbuddy.role=daemon`; safe helper-based self-update is enabled by default and can be disabled only with `HARBORBUDDY_SELF_UPDATE_ENABLED=false`.
- Docker socket and remote-daemon credentials must not be logged.

Add focused unit tests for success, cancellation, unsupported configurations, and rollback failures. Avoid tests that depend on mutable public tags unless the integration boundary specifically requires them.

## Configuration changes

When changing configuration:

1. Update defaults, validation, environment parsing, and tests together.
2. Keep YAML decoding strict.
3. Add a clear migration hint for removed keys.
4. Update `README.md`, `examples/harborbuddy.yml`, and `CHANGELOG.md`.
5. Never document a setting before the implementation accepts it.

The removed keys `docker.tls`, `updates.update_all`, and top-level `logging` must not be reintroduced accidentally. Docker TLS uses standard `DOCKER_*` variables.

## Generated code

The repository currently commits no generated Go source. New generated files must use the standard `// Code generated ... DO NOT EDIT.` header, document an exact generator command and version, and be committed with their source inputs. Generation must be reproducible and CI must reject a dirty regeneration diff. Do not hand-edit generated outputs.

See [docs/repository-policy.md](docs/repository-policy.md) for required checks, action pinning, generated-code policy, and secret-scanning expectations.

## Documentation changes

Documentation must distinguish guarantees from best-effort behavior. In particular:

- State that Docker access is privileged and read/write.
- State that dry-run still pulls images.
- Do not claim automatic private-registry credential discovery.
- Do not document runtime user/group override settings that the application does not implement.
- Describe self-update as a helper lifecycle with rollback and a manual Compose fallback.
- Keep the image name lowercase and consistent: `ghcr.io/mikeo7/harborbuddy`.

## Pull requests

Before requesting review:

- [ ] The change is focused and has a clear rationale.
- [ ] Go files pass formatting, source-size, vet, lint, vulnerability, coverage, and race checks.
- [ ] Workflow, shell, Dockerfile, and YAML changes pass the pinned non-Go linters.
- [ ] Docker changes pass the relevant image, Trivy, or integration checks.
- [ ] New behavior has tests, including failure paths where practical, and does not lower package coverage floors.
- [ ] Documentation and examples match the implementation.
- [ ] `CHANGELOG.md` includes a user-visible entry when appropriate.
- [ ] No credentials, Docker certificates, generated logs, or local artifacts are committed.

Use concise imperative commit subjects, for example `Add rollback readiness check` or `Document Docker TLS variables`.

## Reporting bugs

Include:

- `harborbuddy --version` output
- Docker Engine and operating-system versions
- Sanitized configuration and Compose service definition
- Relevant logs from the daemon and, for self-update failures, the helper
- Exact reproduction steps, expected behavior, and actual behavior

Report security issues privately according to [SECURITY.md](SECURITY.md).

## Code of conduct and license

Participation is governed by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Contributions are licensed under the repository's [PolyForm Noncommercial License 1.0.0](LICENSE).
