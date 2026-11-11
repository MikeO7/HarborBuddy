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
    command "$CONTAINER_ENGINE_BIN" "$@"
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
SELFUPDATE_NEW_IMAGE="harborbuddy:self-update-${RUN_ID}"
REGISTRY_NAME="${RUN_ID}-registry"
TARGET_NAME="${RUN_ID}-target"
OPT_OUT_NAME="${RUN_ID}-opt-out"
DAEMON_NAME="${RUN_ID}-daemon"
TMP_DIR="$(mktemp -d)"
readonly ROOT_DIR DOCKER_SOCKET RUN_ID TEST_LABEL HARBORBUDDY_IMAGE SELFUPDATE_NEW_IMAGE
readonly REGISTRY_NAME TARGET_NAME OPT_OUT_NAME DAEMON_NAME TMP_DIR

REGISTRY_PORT=""
REGISTRY_HOST="127.0.0.1"
FIXTURE_REF=""
OPT_OUT_REF=""
OLD_IMAGE_ID=""
NEW_IMAGE_ID=""
UNHEALTHY_IMAGE_ID=""
OPT_OUT_OLD_IMAGE_ID=""
OPT_OUT_NEW_IMAGE_ID=""
SELFUPDATE_REF=""
OLD_SELF_IMAGE_ID=""
NEW_SELF_IMAGE_ID=""
BROKEN_SELF_IMAGE_ID=""

log() {
    printf '[integration] %s\n' "$*"
}

fail() {
    printf '[integration] ERROR: %s\n' "$*" >&2
    exit 1
}

# With the containerd image store, moving a tag deletes the old image record
# even while a container still uses it. Pin the current image under a second
# tag before rebuilding so restore_local_reference can move the tag back.
pin_reference() {
    docker image tag "$1" "$1-pin"
}

restore_local_reference() {
    local image_id="$1"
    local image_ref="$2"
    local restored_id

    docker image tag "$image_id" "$image_ref"
    restored_id="$(docker image inspect "$image_ref" --format '{{.Id}}')"
    [[ "$restored_id" == "$image_id" ]] || fail "could not restore local reference $image_ref to $image_id"
    docker image rm "$image_ref-pin" >/dev/null
}

cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e

    docker rm -f "$DAEMON_NAME" "$OPT_OUT_NAME" "$TARGET_NAME" "$REGISTRY_NAME" >/dev/null 2>&1
    # Self-update helpers are kept after they exit so their logs stay readable.
    local leftover
    while IFS= read -r leftover; do
        [[ -n "$leftover" ]] && docker rm -f "$leftover" >/dev/null 2>&1
    done < <(docker ps -a --format '{{.Names}}' 2>/dev/null | grep "^${DAEMON_NAME}-harborbuddy-")

    if [[ -n "$FIXTURE_REF" ]]; then
        docker image rm -f "$FIXTURE_REF" >/dev/null 2>&1
    fi
    if [[ -n "$OPT_OUT_REF" ]]; then
        docker image rm -f "$OPT_OUT_REF" >/dev/null 2>&1
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
    if [[ -n "$OPT_OUT_OLD_IMAGE_ID" ]]; then
        docker image rm -f "$OPT_OUT_OLD_IMAGE_ID" >/dev/null 2>&1
    fi
    if [[ -n "$OPT_OUT_NEW_IMAGE_ID" ]]; then
        docker image rm -f "$OPT_OUT_NEW_IMAGE_ID" >/dev/null 2>&1
    fi
    if [[ -n "$SELFUPDATE_REF" ]]; then
        docker image rm -f "$SELFUPDATE_REF" >/dev/null 2>&1
    fi
    if [[ -n "$OLD_SELF_IMAGE_ID" ]]; then
        docker image rm -f "$OLD_SELF_IMAGE_ID" >/dev/null 2>&1
    fi
    if [[ -n "$BROKEN_SELF_IMAGE_ID" ]]; then
        docker image rm -f "$BROKEN_SELF_IMAGE_ID" >/dev/null 2>&1
    fi
    if [[ -n "$NEW_SELF_IMAGE_ID" ]]; then
        docker image rm -f "$NEW_SELF_IMAGE_ID" >/dev/null 2>&1
    fi
    docker image rm -f "$SELFUPDATE_NEW_IMAGE" >/dev/null 2>&1
    docker image rm "$FIXTURE_REF-pin" "$OPT_OUT_REF-pin" "$SELFUPDATE_REF-pin" >/dev/null 2>&1
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
CONTAINER_SECURITY_ARGS=()
FIXTURE_BUILD_ARGS=()
if [[ "$ENGINE_KIND" == podman ]]; then
    # Podman's SELinux policy otherwise blocks a container from opening the
    # mounted Docker-compatible API socket. The socket is already the
    # privileged boundary under test, so disable only label separation for
    # these disposable integration containers.
    CONTAINER_SECURITY_ARGS=(--security-opt label=disable)
    # Podman's default OCI output does not expose Dockerfile HEALTHCHECK
    # metadata through its Docker-compatible image API.
    FIXTURE_BUILD_ARGS=(--format docker)
    # Podman's registry policy can mark localhost as insecure without weakening
    # transport checks for other loopback or remote registries.
    REGISTRY_HOST=localhost
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
    docker.io/library/registry:3.1.2 >/dev/null

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
FIXTURE_REF="${REGISTRY_HOST}:${REGISTRY_PORT}/harborbuddy-integration/app:latest"

mkdir -p "$TMP_DIR/fixture"
printf '%s\n' \
    'FROM docker.io/library/busybox:1.38.0' \
    'ARG REVISION' \
    'LABEL com.harborbuddy.integration=true' \
    "LABEL com.harborbuddy.fixture.revision=\$REVISION" \
    "ENV HB_FIXTURE_REVISION=\$REVISION" \
    'CMD ["sh", "-c", "trap '\''exit 0'\'' TERM INT; while :; do sleep 1; done"]' \
    > "$TMP_DIR/fixture/Dockerfile"

log "publishing initial fixture image"
docker build \
    "${FIXTURE_BUILD_ARGS[@]}" \
    --build-arg REVISION=old \
    --tag "$FIXTURE_REF" \
    "$TMP_DIR/fixture" >/dev/null
docker push "$FIXTURE_REF" >/dev/null
OLD_IMAGE_ID="$(docker image inspect "$FIXTURE_REF" --format '{{.Id}}')"

docker run -d \
    --name "$TARGET_NAME" \
    --label "$TEST_LABEL" \
    --env HB_USER_SETTING=kept \
    "$FIXTURE_REF" >/dev/null

TARGET_CONTAINER_ID="$(docker inspect "$TARGET_NAME" --format '{{.Id}}')"
TARGET_IMAGE_ID="$(docker inspect "$TARGET_NAME" --format '{{.Image}}')"
[[ "$TARGET_IMAGE_ID" == "$OLD_IMAGE_ID" ]] || fail "target did not start from the initial image"

log "publishing replacement fixture image"
pin_reference "$FIXTURE_REF"
docker build --no-cache \
    "${FIXTURE_BUILD_ARGS[@]}" \
    --build-arg REVISION=new \
    --tag "$FIXTURE_REF" \
    "$TMP_DIR/fixture" >/dev/null
docker push "$FIXTURE_REF" >/dev/null
NEW_IMAGE_ID="$(docker image inspect "$FIXTURE_REF" --format '{{.Id}}')"
[[ "$NEW_IMAGE_ID" != "$OLD_IMAGE_ID" ]] || fail "fixture revisions produced the same image"
# The registry now has the replacement, but a real running container still has
# its original local tag until HarborBuddy pulls. Recreate that state after the
# local build moved the tag to the replacement image.
restore_local_reference "$OLD_IMAGE_ID" "$FIXTURE_REF"

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
    "${CONTAINER_SECURITY_ARGS[@]}" \
    --name "$DAEMON_NAME" \
    --label "$TEST_LABEL" \
    --label com.harborbuddy.role=daemon \
    --env HARBORBUDDY_LOG_JSON=true \
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
    "${CONTAINER_SECURITY_ARGS[@]}" \
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
HEALTHY_ENV="$(docker inspect "$TARGET_NAME" --format '{{range .Config.Env}}{{println .}}{{end}}')"
grep -qx 'HB_FIXTURE_REVISION=new' <<< "$HEALTHY_ENV" || fail "replacement inherited the old image's ENV: $HEALTHY_ENV"
grep -qx 'HB_USER_SETTING=kept' <<< "$HEALTHY_ENV" || fail "replacement lost user-set ENV: $HEALTHY_ENV"
HEALTHY_REVISION_LABEL="$(docker inspect "$TARGET_NAME" --format '{{ index .Config.Labels "com.harborbuddy.fixture.revision" }}')"
[[ "$HEALTHY_REVISION_LABEL" == "new" ]] || fail "replacement inherited the old image's label: $HEALTHY_REVISION_LABEL"

log "verifying an already-current container is left untouched"
if ! output="$(docker run --rm \
    "${CONTAINER_SECURITY_ARGS[@]}" \
    --name "$DAEMON_NAME" \
    --label "$TEST_LABEL" \
    --label com.harborbuddy.role=daemon \
    --env HARBORBUDDY_LOG_JSON=true \
    --env HARBORBUDDY_SELF_UPDATE_ENABLED=false \
    --volume "$DOCKER_SOCKET:/var/run/docker.sock" \
    --volume "$TMP_DIR/harborbuddy.yml:/config/harborbuddy.yml:$BIND_READ_ONLY" \
    "$HARBORBUDDY_IMAGE" \
    --once 2>&1)"; then
    printf '%s\n' "$output" >&2
    fail "current-image cycle failed"
fi
printf '%s\n' "$output"
grep -Eq '"event":"update_complete".*"current":1' <<< "$output" || fail "current-image cycle summary was not reported"
CURRENT_CONTAINER_ID="$(docker inspect "$TARGET_NAME" --format '{{.Id}}')"
CURRENT_IMAGE_ID="$(docker inspect "$TARGET_NAME" --format '{{.Image}}')"
[[ "$CURRENT_CONTAINER_ID" == "$HEALTHY_CONTAINER_ID" ]] || fail "current-image cycle replaced the target"
[[ "$CURRENT_IMAGE_ID" == "$HEALTHY_IMAGE_ID" ]] || fail "current-image cycle changed the target image"

log "verifying the auto-update opt-out label"
OPT_OUT_REF="${REGISTRY_HOST}:${REGISTRY_PORT}/harborbuddy-integration/opt-out:latest"
docker build --no-cache \
    "${FIXTURE_BUILD_ARGS[@]}" \
    --build-arg REVISION=opt-out-old \
    --tag "$OPT_OUT_REF" \
    "$TMP_DIR/fixture" >/dev/null
docker push "$OPT_OUT_REF" >/dev/null
OPT_OUT_OLD_IMAGE_ID="$(docker image inspect "$OPT_OUT_REF" --format '{{.Id}}')"
docker run -d \
    --name "$OPT_OUT_NAME" \
    --label "$TEST_LABEL" \
    --label com.harborbuddy.autoupdate=false \
    "$OPT_OUT_REF" >/dev/null
OPT_OUT_CONTAINER_ID="$(docker inspect "$OPT_OUT_NAME" --format '{{.Id}}')"
pin_reference "$OPT_OUT_REF"

docker build --no-cache \
    "${FIXTURE_BUILD_ARGS[@]}" \
    --build-arg REVISION=opt-out-new \
    --tag "$OPT_OUT_REF" \
    "$TMP_DIR/fixture" >/dev/null
docker push "$OPT_OUT_REF" >/dev/null
OPT_OUT_NEW_IMAGE_ID="$(docker image inspect "$OPT_OUT_REF" --format '{{.Id}}')"
[[ "$OPT_OUT_NEW_IMAGE_ID" != "$OPT_OUT_OLD_IMAGE_ID" ]] || fail "opt-out fixture revisions produced the same image"
restore_local_reference "$OPT_OUT_OLD_IMAGE_ID" "$OPT_OUT_REF"
cat > "$TMP_DIR/opt-out.yml" <<EOF
updates:
  enabled: true
  check_interval: 1h
  dry_run: false
  allow_images:
    - "$OPT_OUT_REF"
  deny_images: []
  stop_timeout: 5s
  startup_timeout: 10s
cleanup:
  enabled: false
log:
  level: info
  json: false
  max_size: 10
  max_backups: 1
EOF
if ! output="$(docker run --rm \
    "${CONTAINER_SECURITY_ARGS[@]}" \
    --name "$DAEMON_NAME" \
    --label "$TEST_LABEL" \
    --label com.harborbuddy.role=daemon \
    --env HARBORBUDDY_LOG_JSON=true \
    --env HARBORBUDDY_SELF_UPDATE_ENABLED=false \
    --volume "$DOCKER_SOCKET:/var/run/docker.sock" \
    --volume "$TMP_DIR/opt-out.yml:/config/harborbuddy.yml:$BIND_READ_ONLY" \
    "$HARBORBUDDY_IMAGE" \
    --once 2>&1)"; then
    printf '%s\n' "$output" >&2
    fail "opt-out cycle failed"
fi
printf '%s\n' "$output"
grep -Eq '"event":"update_complete".*"excluded":[1-9][0-9]*' <<< "$output" || fail "opt-out cycle summary was not reported"
OPT_OUT_CURRENT_ID="$(docker inspect "$OPT_OUT_NAME" --format '{{.Id}}')"
OPT_OUT_CURRENT_IMAGE="$(docker inspect "$OPT_OUT_NAME" --format '{{.Image}}')"
[[ "$OPT_OUT_CURRENT_ID" == "$OPT_OUT_CONTAINER_ID" ]] || fail "opt-out container was replaced"
[[ "$OPT_OUT_CURRENT_IMAGE" == "$OPT_OUT_OLD_IMAGE_ID" ]] || fail "opt-out container changed image"

log "publishing an unhealthy fixture revision"
pin_reference "$FIXTURE_REF"
printf '%s\n' \
    'FROM docker.io/library/busybox:1.38.0' \
    'ARG REVISION' \
    'LABEL com.harborbuddy.integration=true' \
    "LABEL com.harborbuddy.fixture.revision=\$REVISION" \
    'HEALTHCHECK --interval=1s --timeout=1s --retries=1 CMD exit 1' \
    'CMD ["sh", "-c", "trap '\''exit 0'\'' TERM INT; while :; do sleep 1; done"]' \
    > "$TMP_DIR/fixture/Dockerfile"
docker build --no-cache \
    "${FIXTURE_BUILD_ARGS[@]}" \
    --build-arg REVISION=unhealthy \
    --tag "$FIXTURE_REF" \
    "$TMP_DIR/fixture" >/dev/null
docker push "$FIXTURE_REF" >/dev/null
UNHEALTHY_IMAGE_ID="$(docker image inspect "$FIXTURE_REF" --format '{{.Id}}')"
[[ "$UNHEALTHY_IMAGE_ID" != "$HEALTHY_IMAGE_ID" ]] || fail "unhealthy fixture did not produce a new image"
restore_local_reference "$HEALTHY_IMAGE_ID" "$FIXTURE_REF"

log "verifying readiness failure rolls back the healthy container"
if ! output="$(docker run --rm \
    "${CONTAINER_SECURITY_ARGS[@]}" \
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
# The rolled-back container still runs the image the pull just untagged, so
# cleanup must skip that dangling image as in use rather than fail on it.
! grep -q 'Cleanup resource failed' <<< "$output" || fail "cleanup tried to remove an image that a container still uses"
docker image inspect "$HEALTHY_IMAGE_ID" >/dev/null || fail "cleanup removed the image of the rolled-back container"

ROLLED_BACK_CONTAINER_ID="$(docker inspect "$TARGET_NAME" --format '{{.Id}}')"
ROLLED_BACK_IMAGE_ID="$(docker inspect "$TARGET_NAME" --format '{{.Image}}')"
ROLLED_BACK_RUNNING="$(docker inspect "$TARGET_NAME" --format '{{.State.Running}}')"
[[ "$ROLLED_BACK_CONTAINER_ID" == "$HEALTHY_CONTAINER_ID" ]] || fail "rollback did not restore the healthy container"
[[ "$ROLLED_BACK_IMAGE_ID" == "$HEALTHY_IMAGE_ID" ]] || fail "rollback did not restore the healthy image"
[[ "$ROLLED_BACK_RUNNING" == "true" ]] || fail "rolled-back container is not running"
[[ -z "$(docker ps -aq --filter "ancestor=$UNHEALTHY_IMAGE_ID")" ]] || fail "failed replacement container was not removed"

log "verifying end-to-end automatic self-update"
SELFUPDATE_REF="${REGISTRY_HOST}:${REGISTRY_PORT}/harborbuddy-integration/daemon:latest"
docker tag "$HARBORBUDDY_IMAGE" "$SELFUPDATE_REF"
docker push "$SELFUPDATE_REF" >/dev/null
OLD_SELF_IMAGE_ID="$(docker image inspect "$SELFUPDATE_REF" --format '{{.Id}}')"

# Build the replacement before starting the daemon so the scheduled update
# cannot race a long build and pull the old registry manifest during the push.
DOCKER_BUILDKIT=1 docker build \
    --build-arg VERSION=0.0.1-self-update \
    --build-arg COMMIT="$COMMIT" \
    --build-arg DATE="$BUILD_DATE" \
    --tag "$SELFUPDATE_NEW_IMAGE" \
    "$ROOT_DIR" >/dev/null
NEW_SELF_IMAGE_ID="$(docker image inspect "$SELFUPDATE_NEW_IMAGE" --format '{{.Id}}')"
[[ "$NEW_SELF_IMAGE_ID" != "$OLD_SELF_IMAGE_ID" ]] || fail "self-update fixture did not produce a new image"

cat > "$TMP_DIR/self-update.yml" <<EOF
updates:
  enabled: true
  check_interval: 5s
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
  json: true
  max_size: 10
  max_backups: 1
EOF

docker run -d "${CONTAINER_SECURITY_ARGS[@]}" \
    --name "$DAEMON_NAME" \
    --restart unless-stopped \
    --label "$TEST_LABEL" \
    --env "HARBORBUDDY_CONTAINER_NAME=$DAEMON_NAME" \
    --env HARBORBUDDY_LOG_JSON=true \
    --volume "$DOCKER_SOCKET:/var/run/docker.sock" \
    --volume "$TMP_DIR/self-update.yml:/config/harborbuddy.yml:$BIND_READ_ONLY" \
    "$SELFUPDATE_REF" >/dev/null

initial_cycle=false
for _ in {1..60}; do
    daemon_logs="$(docker logs "$DAEMON_NAME" 2>&1 || true)"
    if grep -Eq '"event":"update_complete".*"current":[1-9][0-9]*' <<< "$daemon_logs"; then
        initial_cycle=true
        break
    fi
    sleep 0.25
done
if [[ "$initial_cycle" != true ]]; then
    docker ps -a >&2 || true
    docker logs "$DAEMON_NAME" >&2 || true
    fail "self-update daemon did not complete its initial current-image cycle"
fi
OLD_DAEMON_ID="$(docker inspect "$DAEMON_NAME" --format '{{.Id}}')"

pin_reference "$SELFUPDATE_REF"
docker image tag "$SELFUPDATE_NEW_IMAGE" "$SELFUPDATE_REF"
docker push "$SELFUPDATE_REF" >/dev/null
restore_local_reference "$OLD_SELF_IMAGE_ID" "$SELFUPDATE_REF"

self_updated=false
for _ in {1..180}; do
    CURRENT_DAEMON_ID="$(docker inspect "$DAEMON_NAME" --format '{{.Id}}' 2>/dev/null || true)"
    CURRENT_DAEMON_IMAGE="$(docker inspect "$DAEMON_NAME" --format '{{.Image}}' 2>/dev/null || true)"
    CURRENT_DAEMON_RUNNING="$(docker inspect "$DAEMON_NAME" --format '{{.State.Running}}' 2>/dev/null || true)"
    # Keep a copy of helper logs for diagnosis; cleanup removes the helpers at exit.
    for helper_id in $(docker ps -aq --filter label=com.harborbuddy.self-update-helper 2>/dev/null || true); do
        docker logs "$helper_id" > "$TMP_DIR/helper-$helper_id.log" 2>&1 || true
    done
    if [[ -n "$CURRENT_DAEMON_ID" && "$CURRENT_DAEMON_ID" != "$OLD_DAEMON_ID" && "$CURRENT_DAEMON_IMAGE" == "$NEW_SELF_IMAGE_ID" && "$CURRENT_DAEMON_RUNNING" == true ]]; then
        self_updated=true
        break
    fi
    sleep 0.5
done
if [[ "$self_updated" != true ]]; then
    docker ps -a >&2 || true
    docker logs "$DAEMON_NAME" >&2 || true
    for helper_log in "$TMP_DIR"/helper-*.log; do
        [[ -f "$helper_log" ]] || continue
        printf '[integration] helper log %s:\n' "${helper_log##*/}" >&2
        cat "$helper_log" >&2
    done
    fail "automatic self-update did not produce a running replacement"
fi

RESTART_POLICY="$(docker inspect "$DAEMON_NAME" --format '{{.HostConfig.RestartPolicy.Name}}')"
[[ "$RESTART_POLICY" == "unless-stopped" ]] || fail "self-update did not preserve restart policy: $RESTART_POLICY"

helper_kept=false
backups=""
helper_state=""
for _ in {1..60}; do
    backups="$(docker ps -a --format '{{.Names}}' | grep "^${DAEMON_NAME}-harborbuddy-backup-" || true)"
    helper_ids="$(docker ps -aq --filter "label=com.harborbuddy.self-update-target=$OLD_DAEMON_ID")"
    helper_state=""
    if [[ -n "$helper_ids" ]]; then
        helper_state="$(docker inspect "$helper_ids" --format '{{.State.Status}} {{.State.ExitCode}}' 2>/dev/null || true)"
    fi
    if [[ -z "$backups" && "$helper_state" == "exited 0" ]]; then
        helper_kept=true
        break
    fi
    sleep 0.25
done
[[ "$helper_kept" == true ]] || fail "self-update left a backup or did not keep its finished helper: backups=$backups helper=$helper_state"

log "verifying a broken self-update image rolls back once and is not retried"
UPDATED_DAEMON_ID="$CURRENT_DAEMON_ID"
mkdir -p "$TMP_DIR/broken-self"
# The helper runs from the new image, so it must work; only the replacement
# daemon's healthcheck fails, which forces a readiness rollback.
printf '%s\n' \
    "FROM $SELFUPDATE_NEW_IMAGE" \
    'LABEL com.harborbuddy.integration=true' \
    'HEALTHCHECK --interval=1s --timeout=1s --retries=1 CMD ["/harborbuddy", "--not-a-healthcheck"]' \
    > "$TMP_DIR/broken-self/Dockerfile"
docker build \
    "${FIXTURE_BUILD_ARGS[@]}" \
    --tag "$SELFUPDATE_REF" \
    "$TMP_DIR/broken-self" >/dev/null
docker push "$SELFUPDATE_REF" >/dev/null
BROKEN_SELF_IMAGE_ID="$(docker image inspect "$SELFUPDATE_REF" --format '{{.Id}}')"
[[ "$BROKEN_SELF_IMAGE_ID" != "$NEW_SELF_IMAGE_ID" ]] || fail "broken self-update fixture did not produce a new image"
# The running image stays tagged as $SELFUPDATE_NEW_IMAGE, so the mutable tag
# can move back without pinning; the daemon must pull the broken revision.
docker image tag "$NEW_SELF_IMAGE_ID" "$SELFUPDATE_REF"
[[ "$(docker image inspect "$SELFUPDATE_REF" --format '{{.Id}}')" == "$NEW_SELF_IMAGE_ID" ]] || fail "could not restore $SELFUPDATE_REF before the broken self-update"

self_update_blocked=false
for _ in {1..240}; do
    daemon_logs="$(docker logs "$DAEMON_NAME" 2>&1 || true)"
    if grep -q '"event":"self_update_blocked"' <<< "$daemon_logs"; then
        self_update_blocked=true
        break
    fi
    sleep 0.5
done
if [[ "$self_update_blocked" != true ]]; then
    docker ps -a >&2 || true
    docker logs "$DAEMON_NAME" >&2 || true
    for helper in $(docker ps -aq --filter "label=com.harborbuddy.self-update-target=$UPDATED_DAEMON_ID"); do
        docker logs "$helper" >&2 || true
    done
    fail "daemon did not block a retry of the failed self-update image"
fi

[[ "$(docker inspect "$DAEMON_NAME" --format '{{.Id}}')" == "$UPDATED_DAEMON_ID" ]] || fail "broken self-update replaced the daemon"
[[ "$(docker inspect "$DAEMON_NAME" --format '{{.Image}}')" == "$NEW_SELF_IMAGE_ID" ]] || fail "daemon is not running its previous image after rollback"
[[ "$(docker inspect "$DAEMON_NAME" --format '{{.State.Running}}')" == true ]] || fail "daemon is not running after rollback"
RESTART_POLICY="$(docker inspect "$DAEMON_NAME" --format '{{.HostConfig.RestartPolicy.Name}}')"
[[ "$RESTART_POLICY" == "unless-stopped" ]] || fail "rollback did not restore the daemon restart policy: $RESTART_POLICY"

# Several more 5s cycles must not start another helper for the same image.
sleep 12
failed_helpers="$(docker ps -aq --filter "label=com.harborbuddy.self-update-target=$UPDATED_DAEMON_ID")"
failed_helper_count="$(grep -c . <<< "$failed_helpers" || true)"
[[ "$failed_helper_count" == 1 ]] || fail "daemon retried the failed self-update image: $failed_helper_count helpers"
FAILED_HELPER_EXIT="$(docker inspect "$failed_helpers" --format '{{.State.Status}} {{.State.ExitCode}}')"
[[ "$FAILED_HELPER_EXIT" == "exited 1" ]] || fail "failed helper was not kept with a failure exit code: $FAILED_HELPER_EXIT"
helper_logs="$(docker logs "$failed_helpers" 2>&1 || true)"
grep -q '"recovery_outcome":"succeeded"' <<< "$helper_logs" || fail "failed helper did not report daemon recovery"
[[ "$(docker inspect "$DAEMON_NAME" --format '{{.Id}}')" == "$UPDATED_DAEMON_ID" ]] || fail "daemon changed after blocking the failed image"

log "passed: image smoke, dry-run non-mutation, healthy replacement, readiness rollback, automatic self-update, and failed self-update rollback"
