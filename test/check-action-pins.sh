#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'

status=0
while IFS= read -r reference; do
    if [[ "$reference" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@[0-9a-f]{40}$ ]]; then
        continue
    fi
    if [[ "$reference" =~ ^docker://.+@sha256:[0-9a-f]{64}$ ]]; then
        continue
    fi
    printf 'workflow action is not pinned to an immutable digest: %s\n' "$reference" >&2
    status=1
done < <(awk '$1 == "uses:" { print $2 }' .github/workflows/*.yml)

exit "$status"
