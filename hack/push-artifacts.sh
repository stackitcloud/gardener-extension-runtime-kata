#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Packages and pushes the extension Helm chart as an OCI artifact. The chart's values are patched so
# that:
#   - .image points at the freshly built controller image, and
#   - .imageVectorOverwrite points the controller at the freshly built installation image (used by the
#     OperatingSystemConfig webhook via an imageRef). This decouples the installation image reference
#     from the controller's compiled-in version and makes dev and release builds resolve correctly.

if [ "${PUSH:-false}" != "true" ]; then
  echo "Skip pushing artifacts because PUSH is not set to 'true'"
  exit 0
fi

if [ $# -lt 1 ] || [ ! -f "$1" ]; then
  echo "Usage: $0 <path-to-images.json>" >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Verify required tools upfront
for tool in jq yq helm; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "error: '$tool' is required but was not found in PATH" >&2
    exit 1
  fi
done

images=$(cat "$1")
if ! echo "$images" | jq empty >/dev/null 2>&1; then
  echo "error: '$1' does not contain valid JSON" >&2
  exit 1
fi

chart_name="gardener-extension-runtime-kata"
controller_key="gardener-extension-runtime-kata"
installation_key="gardener-extension-runtime-kata-installation"
# Must match kata.RuntimeKataInstallationImageName / imagevector/images.yaml.
installation_imagevector_name="runtime-kata-installation"
helm_artifacts="${REPO_ROOT}/artifacts/charts"

function image_full() {
  local key="$1"
  local val
  val=$(echo "$images" | jq -r --arg k "$key" '.images[$k] // empty')
  if [ -z "$val" ] || [ "$val" = "null" ]; then
    echo "error: image key '$key' not found in images JSON" >&2
    return 1
  fi
  echo "$val"
}

# In Docker/OCI image references, registry ports with colons (e.g. localhost:5001/org/repo:tag)
# only appear before the first slash, whereas tag colons only appear after the last slash.
function image_repo() {
  local ref="$1"
  local no_digest="${ref%@*}"
  local last_segment="${no_digest##*/}"
  if [[ "$last_segment" == *:* ]]; then
    local tag_part=":${last_segment#*:}"
    echo "${no_digest%"$tag_part"}"
  else
    echo "$no_digest"
  fi
}

# Tag without the digest but keeping any '-dirty' suffix, i.e. the exact tag the image was pushed with.
function image_tag_pushed() {
  local ref="$1"
  local no_digest="${ref%@*}"
  local last_segment="${no_digest##*/}"
  if [[ "$last_segment" == *:* ]]; then
    echo "${last_segment#*:}"
  else
    echo ""
  fi
}

# Tag with digest if present, or digest if tag-less, or fallback to latest.
function image_tag_with_digest() {
  local ref="$1"
  local digest=""
  if [[ "$ref" == *@* ]]; then
    digest="@${ref##*@}"
  fi
  local tag
  tag=$(image_tag_pushed "$ref")
  if [ -n "$tag" ]; then
    echo "${tag}${digest}"
  elif [ -n "$digest" ]; then
    echo "${digest#@}"
  else
    echo "latest"
  fi
}

# A clean tag for Helm chart versioning (digest stripped, trailing '-dirty' stripped).
function image_tag() {
  local tag
  tag=$(image_tag_pushed "$1")
  echo "${tag%-dirty}"
}

# Returns the OCI repository prefix for Helm charts by stripping the last segment.
function oci_repo() {
  local repo
  repo=$(image_repo "$1")
  if [[ "$repo" == */* ]]; then
    echo "${repo%/*}"
  else
    echo "$repo"
  fi
}

controller_image="$(image_full "$controller_key")"
installation_image="$(image_full "$installation_key")"

rm -rf "$helm_artifacts"
mkdir -p "$helm_artifacts"

chart_build_dir="${helm_artifacts}/${chart_name}"
cp -r "${REPO_ROOT}/charts/${chart_name}/." "$chart_build_dir"

controller_repo="$(image_repo "$controller_image")"
installation_repo="$(image_repo "$installation_image")"

CONTROLLER_REPO="$controller_repo"
CONTROLLER_TAG="$(image_tag_with_digest "$controller_image")"
INSTALLATION_NAME="$installation_imagevector_name"
INSTALLATION_REPO="$installation_repo"
INSTALLATION_TAG="$(image_tag_pushed "$installation_image")"
export CONTROLLER_REPO CONTROLLER_TAG INSTALLATION_NAME INSTALLATION_REPO INSTALLATION_TAG

yq -i '
  .image.repository = env(CONTROLLER_REPO) |
  .image.tag = env(CONTROLLER_TAG) |
  .imageVectorOverwrite.images = [{"name": env(INSTALLATION_NAME), "repository": env(INSTALLATION_REPO), "tag": env(INSTALLATION_TAG)}]
' "$chart_build_dir/values.yaml"

unset CONTROLLER_REPO CONTROLLER_TAG INSTALLATION_NAME INSTALLATION_REPO INSTALLATION_TAG

# Helm chart versions must follow SemVer 2 (which does not allow a leading 'v').
# Release/CI tags (v1.2.3, v0.0.0, git-describe) become valid SemVer by stripping any leading 'v'.
# A throwaway tag like "dev-kata-test" or a bare commit sha is not valid SemVer, so fall back to a
# valid 0.0.0 pre-release built from the (sanitized) tag.
raw_version="$(image_tag "$controller_image")"
chart_version="${raw_version#v}"

if ! printf '%s' "$chart_version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([-.+].*)?$'; then
  sanitized="$(printf '%s' "$chart_version" | tr -c 'A-Za-z0-9' '-' | sed -E 's/-+/-/g; s/^-//; s/-$//')"
  if [ -z "$sanitized" ]; then
    sanitized="unknown"
  fi
  chart_version="0.0.0-${sanitized}"
  echo "note: tag '${raw_version}' is not SemVer; using chart version '${chart_version}'"
fi

# Propagate '-dev' suffix to chart version for dev builds (where the image repository ends in -dev).
if [[ "$controller_repo" == *-dev ]]; then
  if [[ "$chart_version" == *+* ]]; then
    base_version="${chart_version%%+*}"
    build_meta="${chart_version#*+}"
    if [[ "$base_version" != *-dev ]]; then
      chart_version="${base_version}-dev+${build_meta}"
    fi
  elif [[ "$chart_version" != *-dev ]]; then
    chart_version="${chart_version}-dev"
  fi
fi

# OCI tags forbid '+' characters (allowed in SemVer 2 build metadata).
# Replace '+' with '_' for OCI registry compatibility.
chart_version="${chart_version//+/_}"

packaged_chart_file="${helm_artifacts}/${chart_name}-${chart_version}.tgz"

if ! helm_package_raw_output=$(helm package "$chart_build_dir" --version "$chart_version" -d "$helm_artifacts" 2>&1); then
  echo "Error: 'helm package' failed:" >&2
  echo "$helm_package_raw_output" >&2
  exit 1
fi

if [ ! -f "$packaged_chart_file" ]; then
  echo "Error: Expected packaged chart file '${packaged_chart_file}' was not found." >&2
  echo "Helm output:" >&2
  echo "$helm_package_raw_output" >&2
  exit 1
fi

helm push "$packaged_chart_file" "oci://$(oci_repo "$controller_image")/charts"
