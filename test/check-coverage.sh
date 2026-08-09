#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASELINE_FILE="${1:-$ROOT_DIR/test/coverage-baseline.txt}"
REPORT_FILE="${COVERAGE_REPORT:-$ROOT_DIR/coverage-packages.txt}"

[[ -f "$BASELINE_FILE" ]] || { printf 'coverage baseline not found: %s\n' "$BASELINE_FILE" >&2; exit 1; }

packages=$(go list ./...)
status=0
: > "$REPORT_FILE"
printf '%-70s %10s %10s\n' 'Package' 'Coverage' 'Minimum' >> "$REPORT_FILE"

while IFS= read -r package; do
    [[ -n "$package" ]] || continue
    minimum=$(awk -v package="$package" '$1 == package { print $2 }' "$BASELINE_FILE")
    if [[ -z "$minimum" ]]; then
        printf 'production package missing from coverage baseline: %s\n' "$package" >&2
        status=1
        continue
    fi

    output=$(go test -cover "$package")
    coverage=$(awk '{ for (i = 1; i <= NF; i++) if ($i == "coverage:" && i < NF) { value=$(i + 1); gsub(/%/, "", value); print value; exit } }' <<< "$output")
    coverage=${coverage:-0.0}
    printf '%-70s %9s%% %9s%%\n' "$package" "$coverage" "$minimum" >> "$REPORT_FILE"
    if ! awk -v coverage="$coverage" -v minimum="$minimum" 'BEGIN { exit !(coverage + 0 >= minimum + 0) }'; then
        printf '%s coverage %s%% is below baseline %s%%\n' "$package" "$coverage" "$minimum" >&2
        status=1
    fi
done <<< "$packages"

while IFS=' ' read -r package _; do
    [[ -n "$package" && "${package:0:1}" != "#" ]] || continue
    if ! grep -Fxq "$package" <<< "$packages"; then
        printf 'coverage baseline contains missing package: %s\n' "$package" >&2
        status=1
    fi
done < "$BASELINE_FILE"

cat "$REPORT_FILE"
exit "$status"
