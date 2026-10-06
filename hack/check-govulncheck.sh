#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

GOVULNCHECK="${1:-govulncheck}"
shift || true
PATTERNS="${*:-./...}"

IGNORE_FILE="${GOVULNCHECK_IGNORE_FILE:-${REPO_ROOT}/.govulncheck-ignore.yaml}"

tmp_json=$(mktemp)
tmp_err=$(mktemp)
trap 'rm -f "$tmp_json" "$tmp_err"' EXIT

echo "Scanning dependencies with govulncheck..."

set +e
"$GOVULNCHECK" -format json $PATTERNS > "$tmp_json" 2> "$tmp_err"
exit_code=$?
set -e

if [ $exit_code -ne 0 ] && [ $exit_code -ne 3 ]; then
  cat "$tmp_err" >&2
  exit $exit_code
fi

# Extract vulnerabilities called by our code
vulns_json=$(jq -s '
  reduce (.[] | select(.osv != null)) as $item ({}; .[$item.osv.id] = $item.osv) as $osvs |
  [ .[] | select(.finding != null and (.finding.trace[] | has("function"))) | .finding ] |
  group_by(.osv) |
  map({
    id: .[0].osv,
    fixed_version: (.[0].fixed_version // "N/A"),
    module: (.[0].trace[0].module // "unknown"),
    summary: ($osvs[.[0].osv].summary // "No summary")
  })
' "$tmp_json")

count=$(echo "$vulns_json" | jq 'length')

if [ "$count" -eq 0 ]; then
  echo "✓ No vulnerabilities affecting the codebase were found."
  exit 0
fi

# Parse ignored IDs from ignore file if present
ignored_ids=""
if [ -f "$IGNORE_FILE" ]; then
  ignored_ids=$(awk '/^[[:space:]]*-?[[:space:]]*id:/ {id=$NF; gsub(/["\x27]/, "", id); print id}' "$IGNORE_FILE")
fi

unignored_count=0
ignored_count=0

unignored_output=""
ignored_output=""

for i in $(seq 0 $((count - 1))); do
  vid=$(echo "$vulns_json" | jq -r ".[$i].id")
  vmod=$(echo "$vulns_json" | jq -r ".[$i].module")
  vfix=$(echo "$vulns_json" | jq -r ".[$i].fixed_version")
  vsum=$(echo "$vulns_json" | jq -r ".[$i].summary")

  if [ -n "$ignored_ids" ] && echo "$ignored_ids" | grep -qx "$vid"; then
    ignored_count=$((ignored_count + 1))
    reason=$(awk -v target="$vid" '
      /^[[:space:]]*-?[[:space:]]*id:/ {
        cur_id = $NF
        gsub(/["\x27]/, "", cur_id)
        if (in_target && cur_id != target) exit
        if (cur_id == target) in_target = 1
        next
      }
      in_target && /^[[:space:]]*reason:/ {
        val = $0
        sub(/^[[:space:]]*reason:[[:space:]]*(>-[[:space:]]*)?["\x27]?/, "", val)
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", val)
        if (val != "") { reason = val }
        reading_reason = 1
        next
      }
      in_target && reading_reason && /^[[:space:]]+/ {
        val = $0
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", val)
        if (val != "") {
          if (reason != "") { reason = reason " " val } else { reason = val }
        }
        next
      }
      in_target && reading_reason && !/^[[:space:]]+/ {
        exit
      }
      END {
        print reason
      }
    ' "$IGNORE_FILE")
    [ -z "$reason" ] && reason="No explanation provided"
    ignored_output="${ignored_output}  • ${vid} (${vmod})"$'\n'"    Summary: ${vsum}"$'\n'"    Ignored reason: ${reason}"$'\n'"    More info: https://pkg.go.dev/vuln/${vid}"$'\n\n'
  else
    unignored_count=$((unignored_count + 1))
    unignored_output="${unignored_output}  • ${vid} (${vmod})"$'\n'"    Summary: ${vsum}"$'\n'"    Fixed in: ${vfix}"$'\n'"    More info: https://pkg.go.dev/vuln/${vid}"$'\n\n'
  fi
done

if [ "$unignored_count" -gt 0 ]; then
  echo ""
  echo "✗ Found ${unignored_count} vulnerability(ies) affecting the codebase:"
  echo ""
  printf "%s" "$unignored_output"
  if [ "$ignored_count" -gt 0 ]; then
    echo "Ignored vulnerability(ies) (${ignored_count}) according to $(basename "$IGNORE_FILE"):"
    echo ""
    printf "%s" "$ignored_output"
  fi
  exit 3
fi

echo ""
echo "Notice: ${ignored_count} vulnerability(ies) found, but ignored according to $(basename "$IGNORE_FILE"):"
echo ""
printf "%s" "$ignored_output"
echo "✓ All detected vulnerabilities are in the ignore list."
exit 0
