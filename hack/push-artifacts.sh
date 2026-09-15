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

images=$(cat "$1")
chart_name="gardener-extension-runtime-kata"
controller_key="gardener-extension-runtime-kata"
installation_key="gardener-extension-runtime-kata-installation"
# Must match kata.RuntimeKataInstallationImageName / imagevector/images.yaml.
installation_imagevector_name="runtime-kata-installation"
helm_artifacts=artifacts/charts

function image_full() { echo "$images" | jq -r ".images.\"$1\""; }
function oci_repo() { image_full "$1" | rev | cut -d'/' -f2- | rev; }
function image_repo() { image_full "$1" | cut -d':' -f1; }
function image_tag_with_digest() { image_full "$1" | cut -d':' -f2-; }
# tag without the digest but keeping any '-dirty' suffix, i.e. the exact tag the image was pushed with.
function image_tag_pushed() { image_tag_with_digest "$1" | cut -d'@' -f1; }
# a clean, semver-friendly tag used for the Helm chart version (digest and '-dirty' stripped).
function image_tag() { image_tag_pushed "$1" | sed 's/-dirty//'; }

if [ "$PUSH" != "true" ]; then
  echo "Skip pushing artifacts because PUSH is not set to 'true'"
  exit 0
fi

rm -rf "$helm_artifacts"
mkdir -p "$helm_artifacts"

chart_build_dir="${helm_artifacts}/${chart_name}"
cp -r "charts/${chart_name}/." "$chart_build_dir"

yq -i "\
  ( .image.repository = \"$(image_repo ${controller_key})\" ) | \
  ( .image.tag = \"$(image_tag_with_digest ${controller_key})\" ) | \
  ( .imageVectorOverwrite.images = [{\"name\": \"${installation_imagevector_name}\", \"repository\": \"$(image_repo ${installation_key})\", \"tag\": \"$(image_tag_pushed ${installation_key})\"}] )\
" "$chart_build_dir/values.yaml"

# Helm requires the chart version to be a valid SemVer. Release/CI tags (v1.2.3, v0.0.0, git-describe)
# already are; a throwaway tag like "dev-kata-test" or a bare commit sha is not, so fall back to a
# valid 0.0.0 pre-release built from the (sanitized) tag.
chart_version="$(image_tag ${controller_key})"
if ! printf '%s' "$chart_version" | grep -Eq '^v?[0-9]+\.[0-9]+\.[0-9]+([-.+].*)?$'; then
  sanitized="$(printf '%s' "$chart_version" | tr -c 'A-Za-z0-9' '-' | sed -E 's/-+/-/g; s/^-//; s/-$//')"
  chart_version="0.0.0-${sanitized}"
  echo "note: tag '$(image_tag ${controller_key})' is not SemVer; using chart version '${chart_version}'"
fi

# Helm strips leading 'v' from version tags in the generated filename
packaged_chart_file="${helm_artifacts}/${chart_name}-${chart_version}.tgz"

helm_package_raw_output=$(helm package "$chart_build_dir" --version "$chart_version" -d "$helm_artifacts" 2>&1)

if [ ! -f "$packaged_chart_file" ]; then
  echo "Error: Expected packaged chart file '${packaged_chart_file}' was not found."
  echo "Helm output:"
  echo "$helm_package_raw_output"
  exit 1
fi

helm push "$packaged_chart_file" "oci://$(oci_repo ${controller_key})/charts"
