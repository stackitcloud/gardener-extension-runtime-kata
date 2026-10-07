#!/usr/bin/env bash

set -euo pipefail

GOVULNCHECK="${1:-govulncheck}"
shift || true
PATTERNS="${*:-./...}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IGNORE_FILE="${GOVULNCHECK_IGNORE_FILE:-${REPO_ROOT}/.govulncheck-ignore}"

tmp=$(mktemp)
ignore_tmp=$(mktemp)
trap 'rm -f "$tmp" "$ignore_tmp"' EXIT

exit_code=0
"$GOVULNCHECK" -format json $PATTERNS > "$tmp" || exit_code=$?

if [ "$exit_code" -ne 0 ] && [ "$exit_code" -ne 3 ]; then
  exit "$exit_code"
fi

vulns=$(jq -r 'select(.finding != null and (.finding.trace[] | has("function"))) | .finding.osv' "$tmp" | sort -u)
if [ -z "$vulns" ]; then
  echo "No vulnerabilities affecting the codebase were found."
  exit 0
fi

if [ -f "$IGNORE_FILE" ]; then
  awk '/^[[:space:]]*[^#[:space:]]/ {print $1}' "$IGNORE_FILE" > "$ignore_tmp"
fi

unignored=$(echo "$vulns" | grep -vxFf "$ignore_tmp" || true)

if [ -n "$unignored" ]; then
  echo "Unignored vulnerabilities affecting the codebase:"
  for id in $unignored; do
    echo "  • $id (https://pkg.go.dev/vuln/$id)"
  done
  exit 3
fi

echo "All detected vulnerabilities are ignored in $(basename "$IGNORE_FILE")."
