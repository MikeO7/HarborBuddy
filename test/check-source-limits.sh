#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'

readonly PRODUCTION_LIMIT=300
readonly TEST_LIMIT=400
status=0

while IFS= read -r -d '' file; do
    if grep -Eq '^// Code generated .* DO NOT EDIT\.$' "$file"; then
        continue
    fi

    limit=$PRODUCTION_LIMIT
    if [[ "$file" == *_test.go ]]; then
        limit=$TEST_LIMIT
    fi
    lines=$(wc -l < "$file")
    if (( lines > limit )); then
        printf '%s has %d lines; limit is %d\n' "$file" "$lines" "$limit" >&2
        status=1
    fi
done < <(find cmd internal -type f -name '*.go' -print0)

exit "$status"
