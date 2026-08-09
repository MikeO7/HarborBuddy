#!/usr/bin/env bash
set -Eeuo pipefail

readonly pinned_version="0.11.0"
if command -v shellcheck >/dev/null 2>&1; then
    native_version="$(shellcheck --version | awk '/^version:/ {print $2}')"
    if [[ "$native_version" == "$pinned_version" ]]; then
        exec shellcheck "$@"
    fi
fi

engine="${CONTAINER_ENGINE:-docker}"
if ! command -v "$engine" >/dev/null 2>&1; then
    if [[ "$engine" == "podman" && -x /opt/podman/bin/podman ]]; then
        engine=/opt/podman/bin/podman
    else
        printf 'Container engine not found: %s\n' "$engine" >&2
        exit 127
    fi
fi

exec "$engine" run --rm --interactive \
    --volume "$PWD:/work:ro" \
    --workdir /work \
    docker.io/koalaman/shellcheck:v0.11.0@sha256:61862eba1fcf09a484ebcc6feea46f1782532571a34ed51fedf90dd25f925a8d \
    "$@"
