#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASELINE_FILE="${1:-$ROOT_DIR/test/coverage-baseline.txt}"
REPORT_FILE="${COVERAGE_REPORT:-$ROOT_DIR/coverage-packages.txt}"
# Output of the single `go test -coverprofile` run that `make test-cover` saves,
# so the per-package floors are checked without running the tests again.
TEST_OUTPUT_FILE="${COVERAGE_TEST_OUTPUT:-$ROOT_DIR/coverage-tests.out}"

[[ -f "$BASELINE_FILE" ]] || { printf 'coverage baseline not found: %s\n' "$BASELINE_FILE" >&2; exit 1; }
[[ -f "$TEST_OUTPUT_FILE" ]] || { printf 'go test output not found: %s (run make test-cover)\n' "$TEST_OUTPUT_FILE" >&2; exit 1; }

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

    coverage=$(awk -v package="$package" '$1 == "ok" && $2 == package { for (i = 3; i < NF; i++) if ($i == "coverage:") { value=$(i + 1); gsub(/%/, "", value); print value; exit } }' "$TEST_OUTPUT_FILE")
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
