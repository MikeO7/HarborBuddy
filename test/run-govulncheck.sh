#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'

readonly VERSION="${GOVULNCHECK_VERSION:-v1.6.0}"
readonly ACCEPTED_IDS=(
    GO-2026-4883 # Moby daemon plugin privilege validation; HarborBuddy is only an API client.
    GO-2026-4887 # Moby daemon AuthZ request handling; HarborBuddy does not run the daemon server.
    GO-2026-5668 # docker cp/archive race; HarborBuddy never invokes copy/archive APIs.
)
output=$(mktemp)
trap 'rm -f "$output"' EXIT

set +e
go run "golang.org/x/vuln/cmd/govulncheck@${VERSION}" ./... > "$output" 2>&1
rc=$?
set -e
cat "$output"

if (( rc == 0 )); then
    exit 0
fi

found=$(grep -Eo 'GO-[0-9]{4}-[0-9]+' "$output" | sort -u)
if [[ -z "$found" ]]; then
    printf 'govulncheck failed without reporting a vulnerability ID\n' >&2
    exit "$rc"
fi

while IFS= read -r id; do
    accepted=false
    for allowed in "${ACCEPTED_IDS[@]}"; do
        if [[ "$id" == "$allowed" ]]; then
            accepted=true
            break
        fi
    done
    if [[ "$accepted" != true ]]; then
        printf 'govulncheck reported unaccepted vulnerability %s\n' "$id" >&2
        exit "$rc"
    fi
done <<< "$found"

printf 'govulncheck reported only reviewed Moby daemon/copy advisories that are unreachable through HarborBuddy APIs:\n%s\n' "$found" >&2
