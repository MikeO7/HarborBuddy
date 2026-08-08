#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTAINER_ENGINE_BIN="${CONTAINER_ENGINE:-}"
if [[ -z "$CONTAINER_ENGINE_BIN" ]]; then
    if command -v docker >/dev/null 2>&1; then
        CONTAINER_ENGINE_BIN=docker
    elif command -v podman >/dev/null 2>&1; then
        CONTAINER_ENGINE_BIN=podman
    elif [[ -x /opt/podman/bin/podman ]]; then
        CONTAINER_ENGINE_BIN=/opt/podman/bin/podman
    else
        printf '[integration] ERROR: Docker or Podman is required\n' >&2
        exit 1
    fi
fi
if ! command -v "$CONTAINER_ENGINE_BIN" >/dev/null 2>&1; then
    printf '[integration] ERROR: container engine not found: %s\n' "$CONTAINER_ENGINE_BIN" >&2
    exit 1
fi

docker() {
    "$CONTAINER_ENGINE_BIN" "$@"
}

ENGINE_KIND=docker
if [[ "$CONTAINER_ENGINE_BIN" == *podman* ]]; then
    ENGINE_KIND=podman
fi
if ! docker info >/dev/null 2>&1; then
    printf '[integration] ERROR: %s engine is unavailable; start it before running the suite\n' "$ENGINE_KIND" >&2
    exit 1
fi
DOCKER_SOCKET="${ENGINE_SOCKET:-${DOCKER_SOCKET:-}}"
if [[ -z "$DOCKER_SOCKET" && "$ENGINE_KIND" == podman ]]; then
    DOCKER_SOCKET="$(docker info --format '{{.Host.RemoteSocket.Path}}')"
    DOCKER_SOCKET="${DOCKER_SOCKET#unix://}"
fi
DOCKER_SOCKET="${DOCKER_SOCKET:-/var/run/docker.sock}"
RUN_ID="hb-int-$(date +%s)-$$"
TEST_LABEL="com.harborbuddy.integration=true"
HARBORBUDDY_IMAGE="harborbuddy:integration-${RUN_ID}"
REGISTRY_NAME="${RUN_ID}-registry"
TARGET_NAME="${RUN_ID}-target"
DAEMON_NAME="${RUN_ID}-daemon"
TMP_DIR="$(mktemp -d)"
readonly ROOT_DIR DOCKER_SOCKET RUN_ID TEST_LABEL HARBORBUDDY_IMAGE
readonly REGISTRY_NAME TARGET_NAME DAEMON_NAME TMP_DIR

REGISTRY_PORT=""
FIXTURE_REF=""
OLD_IMAGE_ID=""
NEW_IMAGE_ID=""
UNHEALTHY_IMAGE_ID=""
SELFUPDATE_REF=""
OLD_SELF_IMAGE_ID=""
NEW_SELF_IMAGE_ID=""

log() {
    printf '[integration] %s\n' "$*"
}

fail() {
    printf '[integration] ERROR: %s\n' "$*" >&2
    exit 1
}

cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e

    docker rm -f "$DAEMON_NAME" "$TARGET_NAME" "$REGISTRY_NAME" >/dev/null 2>&1

    if [[ -n "$FIXTURE_REF" ]]; then
        docker image rm -f "$FIXTURE_REF" >/dev/null 2>&1
    fi
    if [[ -n "$OLD_IMAGE_ID" ]]; then
        docker image rm -f "$OLD_IMAGE_ID" >/dev/null 2>&1
    fi
    if [[ -n "$NEW_IMAGE_ID" ]]; then
        docker image rm -f "$NEW_IMAGE_ID" >/dev/null 2>&1
    fi
    if [[ -n "$UNHEALTHY_IMAGE_ID" ]]; then
        docker image rm -f "$UNHEALTHY_IMAGE_ID" >/dev/null 2>&1
    fi
    if [[ -n "$SELFUPDATE_REF" ]]; then
        docker image rm -f "$SELFUPDATE_REF" >/dev/null 2>&1
    fi
    if [[ -n "$OLD_SELF_IMAGE_ID" ]]; then
        docker image rm -f "$OLD_SELF_IMAGE_ID" >/dev/null 2>&1
    fi
    if [[ -n "$NEW_SELF_IMAGE_ID" ]]; then
        docker image rm -f "$NEW_SELF_IMAGE_ID" >/dev/null 2>&1
    fi
    docker image rm -f "$HARBORBUDDY_IMAGE" >/dev/null 2>&1
    rm -rf "$TMP_DIR"

    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ "$ENGINE_KIND" == docker && ! -S "$DOCKER_SOCKET" ]]; then
    fail "Docker socket not found at $DOCKER_SOCKET"
fi
log "using $ENGINE_KIND via $CONTAINER_ENGINE_BIN (API socket: $DOCKER_SOCKET)"
BIND_READ_ONLY=ro
if [[ "$ENGINE_KIND" == podman ]]; then
    BIND_READ_ONLY=ro,Z
fi

COMMIT="$(git -C "$ROOT_DIR" rev-parse HEAD 2>/dev/null || printf 'unknown')"
BUILD_DATE="$(git -C "$ROOT_DIR" show -s --format=%cI HEAD 2>/dev/null || printf 'unknown')"

log "building HarborBuddy image"
DOCKER_BUILDKIT=1 docker build \
    --build-arg VERSION=0.0.0-integration \
    --build-arg COMMIT="$COMMIT" \
    --build-arg DATE="$BUILD_DATE" \
    --label "$TEST_LABEL" \
    --tag "$HARBORBUDDY_IMAGE" \
    "$ROOT_DIR" >/dev/null

version_output="$(docker run --rm "$HARBORBUDDY_IMAGE" --version)"
printf '%s\n' "$version_output"
grep -q 'HarborBuddy' <<< "$version_output" || fail "version command did not start"

role_label="$(docker image inspect "$HARBORBUDDY_IMAGE" --format '{{ index .Config.Labels "com.harborbuddy.role" }}')"
[[ "$role_label" == "daemon" ]] || fail "daemon role image label is missing"

log "starting temporary local registry"
docker run -d \
    --name "$REGISTRY_NAME" \
    --label "$TEST_LABEL" \
    --publish 127.0.0.1::5000 \
    docker.io/library/registry:2 >/dev/null

registry_ready=false
for _ in {1..50}; do
    registry_logs="$(docker logs "$REGISTRY_NAME" 2>&1 || true)"
    if grep -q 'listening on' <<< "$registry_logs"; then
        registry_ready=true
        break
    fi
    sleep 0.2
done
[[ "$registry_ready" == "true" ]] || fail "temporary registry did not become ready"

REGISTRY_PORT="$(docker port "$REGISTRY_NAME" 5000/tcp | grep '127.0.0.1:' | rev | cut -d: -f1 | rev)"
[[ "$REGISTRY_PORT" =~ ^[0-9]+$ ]] || fail "could not determine registry port"
FIXTURE_REF="127.0.0.1:${REGISTRY_PORT}/harborbuddy-integration/app:latest"

mkdir -p "$TMP_DIR/fixture"
printf '%s\n' \
    'FROM docker.io/library/busybox:1.36' \
    'ARG REVISION' \
    'LABEL com.harborbuddy.integration=true' \
    "LABEL com.harborbuddy.fixture.revision=\$REVISION" \
    'CMD ["sh", "-c", "trap '\''exit 0'\'' TERM INT; while :; do sleep 1; done"]' \
    > "$TMP_DIR/fixture/Dockerfile"

log "publishing initial fixture image"
docker build \
    --build-arg REVISION=old \
    --tag "$FIXTURE_REF" \
    "$TMP_DIR/fixture" >/dev/null
docker push "$FIXTURE_REF" >/dev/null
OLD_IMAGE_ID="$(docker image inspect "$FIXTURE_REF" --format '{{.Id}}')"

docker run -d \
    --name "$TARGET_NAME" \
    --label "$TEST_LABEL" \
    "$FIXTURE_REF" >/dev/null

TARGET_CONTAINER_ID="$(docker inspect "$TARGET_NAME" --format '{{.Id}}')"
TARGET_IMAGE_ID="$(docker inspect "$TARGET_NAME" --format '{{.Image}}')"
[[ "$TARGET_IMAGE_ID" == "$OLD_IMAGE_ID" ]] || fail "target did not start from the initial image"

log "publishing replacement fixture image"
docker build --no-cache \
    --build-arg REVISION=new \
    --tag "$FIXTURE_REF" \
    "$TMP_DIR/fixture" >/dev/null
docker push "$FIXTURE_REF" >/dev/null
NEW_IMAGE_ID="$(docker image inspect "$FIXTURE_REF" --format '{{.Id}}')"
[[ "$NEW_IMAGE_ID" != "$OLD_IMAGE_ID" ]] || fail "fixture revisions produced the same image"

cat > "$TMP_DIR/harborbuddy.yml" <<EOF
updates:
  enabled: true
  check_interval: 1h
  dry_run: true
  allow_images:
    - "$FIXTURE_REF"
  deny_images: []
  stop_timeout: 5s
  startup_timeout: 10s
cleanup:
  enabled: true
  min_age_hours: 0
  dangling_only: true
log:
  level: info
  json: false
  max_size: 10
  max_backups: 1
EOF

log "running one dry-run cycle"
if ! output="$(docker run --rm \
    --name "$DAEMON_NAME" \
    --label "$TEST_LABEL" \
    --label com.harborbuddy.role=daemon \
    --env HARBORBUDDY_SELF_UPDATE_ENABLED=false \
    --volume "$DOCKER_SOCKET:/var/run/docker.sock" \
    --volume "$TMP_DIR/harborbuddy.yml:/config/harborbuddy.yml:$BIND_READ_ONLY" \
    "$HARBORBUDDY_IMAGE" \
    --once --dry-run 2>&1)"; then
    printf '%s\n' "$output" >&2
    fail "HarborBuddy dry-run failed"
fi
printf '%s\n' "$output"
grep -Eq 'would_update|Container would be updated' <<< "$output" || fail "dry-run did not report the available fixture update"

CURRENT_CONTAINER_ID="$(docker inspect "$TARGET_NAME" --format '{{.Id}}')"
CURRENT_IMAGE_ID="$(docker inspect "$TARGET_NAME" --format '{{.Image}}')"
CURRENT_RUNNING="$(docker inspect "$TARGET_NAME" --format '{{.State.Running}}')"

[[ "$CURRENT_CONTAINER_ID" == "$TARGET_CONTAINER_ID" ]] || fail "dry-run replaced the target container"
[[ "$CURRENT_IMAGE_ID" == "$TARGET_IMAGE_ID" ]] || fail "dry-run changed the target image"
[[ "$CURRENT_RUNNING" == "true" ]] || fail "dry-run stopped the target container"
docker image inspect "$OLD_IMAGE_ID" >/dev/null || fail "dry-run cleanup deleted the old image"

log "running a real healthy update"
config_contents="$(< "$TMP_DIR/harborbuddy.yml")"
printf '%s\n' "${config_contents/dry_run: true/dry_run: false}" > "$TMP_DIR/harborbuddy.yml"
if ! output="$(docker run --rm \
    --name "$DAEMON_NAME" \
    --label "$TEST_LABEL" \
    --label com.harborbuddy.role=daemon \
    --env HARBORBUDDY_SELF_UPDATE_ENABLED=false \
    --volume "$DOCKER_SOCKET:/var/run/docker.sock" \
    --volume "$TMP_DIR/harborbuddy.yml:/config/harborbuddy.yml:$BIND_READ_ONLY" \
    "$HARBORBUDDY_IMAGE" \
    --once 2>&1)"; then
    printf '%s\n' "$output" >&2
    fail "HarborBuddy healthy update failed"
fi
printf '%s\n' "$output"
grep -Eq '"result":"updated"|Container updated' <<< "$output" || fail "healthy update was not reported"

HEALTHY_CONTAINER_ID="$(docker inspect "$TARGET_NAME" --format '{{.Id}}')"
HEALTHY_IMAGE_ID="$(docker inspect "$TARGET_NAME" --format '{{.Image}}')"
HEALTHY_RUNNING="$(docker inspect "$TARGET_NAME" --format '{{.State.Running}}')"
[[ "$HEALTHY_CONTAINER_ID" != "$TARGET_CONTAINER_ID" ]] || fail "real update did not replace the target container"
[[ "$HEALTHY_IMAGE_ID" == "$NEW_IMAGE_ID" ]] || fail "real update did not use the replacement image"
[[ "$HEALTHY_RUNNING" == "true" ]] || fail "replacement container is not running"

log "publishing an unhealthy fixture revision"
printf '%s\n' \
    'FROM docker.io/library/busybox:1.36' \
    'ARG REVISION' \
    'LABEL com.harborbuddy.integration=true' \
    "LABEL com.harborbuddy.fixture.revision=\$REVISION" \
    'HEALTHCHECK --interval=1s --timeout=1s --retries=1 CMD exit 1' \
    'CMD ["sh", "-c", "trap '\''exit 0'\'' TERM INT; while :; do sleep 1; done"]' \
    > "$TMP_DIR/fixture/Dockerfile"
docker build --no-cache \
    --build-arg REVISION=unhealthy \
    --tag "$FIXTURE_REF" \
    "$TMP_DIR/fixture" >/dev/null
docker push "$FIXTURE_REF" >/dev/null
UNHEALTHY_IMAGE_ID="$(docker image inspect "$FIXTURE_REF" --format '{{.Id}}')"
[[ "$UNHEALTHY_IMAGE_ID" != "$HEALTHY_IMAGE_ID" ]] || fail "unhealthy fixture did not produce a new image"

log "verifying readiness failure rolls back the healthy container"
if ! output="$(docker run --rm \
    --name "$DAEMON_NAME" \
    --label "$TEST_LABEL" \
    --label com.harborbuddy.role=daemon \
    --env HARBORBUDDY_SELF_UPDATE_ENABLED=false \
    --volume "$DOCKER_SOCKET:/var/run/docker.sock" \
    --volume "$TMP_DIR/harborbuddy.yml:/config/harborbuddy.yml:$BIND_READ_ONLY" \
    "$HARBORBUDDY_IMAGE" \
    --once 2>&1)"; then
    printf '%s\n' "$output" >&2
    fail "HarborBuddy rollback cycle exited unexpectedly"
fi
printf '%s\n' "$output"
grep -Eq '"result":"failed"|Container update failed|did not become ready' <<< "$output" || fail "readiness failure was not reported"

ROLLED_BACK_CONTAINER_ID="$(docker inspect "$TARGET_NAME" --format '{{.Id}}')"
ROLLED_BACK_IMAGE_ID="$(docker inspect "$TARGET_NAME" --format '{{.Image}}')"
ROLLED_BACK_RUNNING="$(docker inspect "$TARGET_NAME" --format '{{.State.Running}}')"
[[ "$ROLLED_BACK_CONTAINER_ID" == "$HEALTHY_CONTAINER_ID" ]] || fail "rollback did not restore the healthy container"
[[ "$ROLLED_BACK_IMAGE_ID" == "$HEALTHY_IMAGE_ID" ]] || fail "rollback did not restore the healthy image"
[[ "$ROLLED_BACK_RUNNING" == "true" ]] || fail "rolled-back container is not running"
[[ -z "$(docker ps -aq --filter "ancestor=$UNHEALTHY_IMAGE_ID")" ]] || fail "failed replacement container was not removed"

log "verifying end-to-end automatic self-update"
SELFUPDATE_REF="127.0.0.1:${REGISTRY_PORT}/harborbuddy-integration/daemon:latest"
docker tag "$HARBORBUDDY_IMAGE" "$SELFUPDATE_REF"
docker push "$SELFUPDATE_REF" >/dev/null
OLD_SELF_IMAGE_ID="$(docker image inspect "$SELFUPDATE_REF" --format '{{.Id}}')"

cat > "$TMP_DIR/self-update.yml" <<EOF
updates:
  enabled: true
  check_interval: 2s
  self_update: true
  allow_images:
    - "$SELFUPDATE_REF"
  deny_images: []
  stop_timeout: 7s
  startup_timeout: 20s
cleanup:
  enabled: false
log:
  level: info
  json: false
  max_size: 10
  max_backups: 1
EOF

docker run -d \
    --name "$DAEMON_NAME" \
    --restart unless-stopped \
    --label "$TEST_LABEL" \
    --env "HARBORBUDDY_CONTAINER_NAME=$DAEMON_NAME" \
    --volume "$DOCKER_SOCKET:/var/run/docker.sock" \
    --volume "$TMP_DIR/self-update.yml:/config/harborbuddy.yml:$BIND_READ_ONLY" \
    "$SELFUPDATE_REF" >/dev/null

initial_cycle=false
for _ in {1..60}; do
    if docker logs "$DAEMON_NAME" 2>&1 | grep -Eq 'image is current|"result":"current"'; then
        initial_cycle=true
        break
    fi
    sleep 0.25
done
[[ "$initial_cycle" == true ]] || fail "self-update daemon did not complete its initial current-image cycle"
OLD_DAEMON_ID="$(docker inspect "$DAEMON_NAME" --format '{{.Id}}')"

DOCKER_BUILDKIT=1 docker build \
    --build-arg VERSION=0.0.1-self-update \
    --build-arg COMMIT="$COMMIT" \
    --build-arg DATE="$BUILD_DATE" \
    --tag "$SELFUPDATE_REF" \
    "$ROOT_DIR" >/dev/null
docker push "$SELFUPDATE_REF" >/dev/null
NEW_SELF_IMAGE_ID="$(docker image inspect "$SELFUPDATE_REF" --format '{{.Id}}')"
[[ "$NEW_SELF_IMAGE_ID" != "$OLD_SELF_IMAGE_ID" ]] || fail "self-update fixture did not produce a new image"

self_updated=false
for _ in {1..180}; do
    CURRENT_DAEMON_ID="$(docker inspect "$DAEMON_NAME" --format '{{.Id}}' 2>/dev/null || true)"
    CURRENT_DAEMON_IMAGE="$(docker inspect "$DAEMON_NAME" --format '{{.Image}}' 2>/dev/null || true)"
    CURRENT_DAEMON_RUNNING="$(docker inspect "$DAEMON_NAME" --format '{{.State.Running}}' 2>/dev/null || true)"
    if [[ -n "$CURRENT_DAEMON_ID" && "$CURRENT_DAEMON_ID" != "$OLD_DAEMON_ID" && "$CURRENT_DAEMON_IMAGE" == "$NEW_SELF_IMAGE_ID" && "$CURRENT_DAEMON_RUNNING" == true ]]; then
        self_updated=true
        break
    fi
    sleep 0.5
done
if [[ "$self_updated" != true ]]; then
    docker ps -a >&2 || true
    docker logs "$DAEMON_NAME" >&2 || true
    fail "automatic self-update did not produce a running replacement"
fi

RESTART_POLICY="$(docker inspect "$DAEMON_NAME" --format '{{.HostConfig.RestartPolicy.Name}}')"
[[ "$RESTART_POLICY" == "unless-stopped" ]] || fail "self-update did not preserve restart policy: $RESTART_POLICY"

helper_cleaned=false
for _ in {1..30}; do
    leftovers="$(docker ps -a --format '{{.Names}}' | grep "^${DAEMON_NAME}-harborbuddy-" || true)"
    if [[ -z "$leftovers" ]]; then
        helper_cleaned=true
        break
    fi
    sleep 0.25
done
[[ "$helper_cleaned" == true ]] || fail "self-update left helper or backup containers behind: $leftovers"

log "passed: image smoke, dry-run non-mutation, healthy replacement, readiness rollback, and automatic self-update"
