#!/usr/bin/env bash
#
# SPDX-FileCopyrightText: Contributors to the Gardener project
#
# SPDX-License-Identifier: Apache-2.0

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "> Check Kata package release"

cd "${REPO_ROOT}"

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "Notice: Not in a git repository. Skipping Kata package release check."
  exit 0
fi

# Determine base commit to compare against
BASE_COMMIT=""
BASE_DESC=""

if [ -n "${PULL_BASE_SHA:-}" ] && git rev-parse --verify "${PULL_BASE_SHA}^{commit}" >/dev/null 2>&1; then
  BASE_COMMIT="${PULL_BASE_SHA}"
  BASE_DESC="PULL_BASE_SHA (${PULL_BASE_SHA:0:8})"
elif [ -n "${PULL_BASE_REF:-}" ]; then
  if git rev-parse --verify "origin/${PULL_BASE_REF}^{commit}" >/dev/null 2>&1; then
    BASE_COMMIT=$(git merge-base "origin/${PULL_BASE_REF}" HEAD 2>/dev/null || true)
    BASE_DESC="origin/${PULL_BASE_REF}"
  elif git rev-parse --verify "${PULL_BASE_REF}^{commit}" >/dev/null 2>&1; then
    BASE_COMMIT=$(git merge-base "${PULL_BASE_REF}" HEAD 2>/dev/null || true)
    BASE_DESC="${PULL_BASE_REF}"
  fi
fi

if [ -z "${BASE_COMMIT}" ]; then
  for candidate in "origin/main" "origin/master" "main" "master"; do
    if git rev-parse --verify "${candidate}^{commit}" >/dev/null 2>&1; then
      BASE_COMMIT=$(git merge-base "${candidate}" HEAD 2>/dev/null || true)
      BASE_DESC="${candidate}"
      if [ -n "${BASE_COMMIT}" ]; then
        break
      fi
    fi
  done
fi

if [ -z "${BASE_COMMIT}" ]; then
  if git rev-parse --verify "HEAD~1^{commit}" >/dev/null 2>&1; then
    BASE_COMMIT=$(git rev-parse "HEAD~1")
    BASE_DESC="HEAD~1"
  fi
fi

if [ -z "${BASE_COMMIT}" ]; then
  echo "Notice: Could not determine base commit to compare against. Skipping Kata package release check."
  exit 0
fi

# Files that directly affect the installation image payload / packaging
PACKAGING_TARGETS=(
  "hack/install-binaries.sh"
  "cmd/gardener-extension-runtime-kata-installation"
)

CHANGED_FILES=$(git diff --name-only "${BASE_COMMIT}" -- "${PACKAGING_TARGETS[@]}" 2>/dev/null || true)

parse_var() {
  local content="$1"
  local var_name="$2"
  echo "$content" | grep -E "^[[:space:]]*${var_name}[[:space:]]*:?=" | head -n1 | sed -E "s/^[[:space:]]*${var_name}[[:space:]]*:?=[[:space:]]*//" | tr -d '[:space:]'
}

if [ ! -f "KATA_VERSION" ]; then
  echo "error: KATA_VERSION file not found in repository root" >&2
  exit 1
fi

CURRENT_CONTENT=$(cat "KATA_VERSION")
CURRENT_KATA_VERSION=$(parse_var "$CURRENT_CONTENT" "KATA_VERSION")
CURRENT_KATA_RELEASE=$(parse_var "$CURRENT_CONTENT" "KATA_PACKAGE_RELEASE")

if [ -z "$CURRENT_KATA_VERSION" ] || [ -z "$CURRENT_KATA_RELEASE" ]; then
  echo "error: could not parse KATA_VERSION or KATA_PACKAGE_RELEASE from KATA_VERSION file" >&2
  exit 1
fi

# Check synchronization between KATA_VERSION and imagevector/images.yaml
if [ -f "imagevector/images.yaml" ]; then
  EXPECTED_TAG="${CURRENT_KATA_VERSION}-${CURRENT_KATA_RELEASE}"
  ACTUAL_TAG=$(grep -E '^[[:space:]]*tag:' "imagevector/images.yaml" | head -n1 | awk '{print $2}' | tr -d '"' | tr -d "'")
  if [ -n "$ACTUAL_TAG" ] && [ "$ACTUAL_TAG" != "$EXPECTED_TAG" ]; then
    echo "error: imagevector/images.yaml tag (${ACTUAL_TAG}) does not match KATA_VERSION (${EXPECTED_TAG}). Please run 'make generate'." >&2
    exit 1
  fi
fi

if [ -z "${CHANGED_FILES}" ]; then
  echo "No changes in Kata packaging files since ${BASE_DESC} (${BASE_COMMIT:0:8})."
  exit 0
fi

# Read base KATA_VERSION and KATA_PACKAGE_RELEASE (fallback to Makefile for older commits)
BASE_CONTENT=$(git show "${BASE_COMMIT}:KATA_VERSION" 2>/dev/null || git show "${BASE_COMMIT}:Makefile" 2>/dev/null || true)
BASE_KATA_VERSION=$(parse_var "$BASE_CONTENT" "KATA_VERSION")
BASE_KATA_RELEASE=$(parse_var "$BASE_CONTENT" "KATA_PACKAGE_RELEASE")

# If upstream KATA_VERSION changed, that inherently produces a new version/image tag
if [ -n "$BASE_KATA_VERSION" ] && [ "$CURRENT_KATA_VERSION" != "$BASE_KATA_VERSION" ]; then
  echo "Kata upstream version changed (${BASE_KATA_VERSION} -> ${CURRENT_KATA_VERSION}). Package release check passed."
  exit 0
fi

# If packaging files changed and upstream version is the same, package release MUST be incremented
if [ -n "$BASE_KATA_RELEASE" ]; then
  if [ "$CURRENT_KATA_RELEASE" -le "$BASE_KATA_RELEASE" ] 2>/dev/null || [ "$CURRENT_KATA_RELEASE" = "$BASE_KATA_RELEASE" ]; then
    echo "error: Changes detected in Kata packaging files since ${BASE_DESC} (${BASE_COMMIT:0:8}), but KATA_PACKAGE_RELEASE was not incremented in KATA_VERSION!" >&2
    echo "Changed packaging files:" >&2
    # shellcheck disable=SC2001
    echo "$CHANGED_FILES" | sed 's/^/  /' >&2
    echo "" >&2
    echo "Current KATA_VERSION:         ${CURRENT_KATA_VERSION}" >&2
    echo "Base KATA_PACKAGE_RELEASE:    ${BASE_KATA_RELEASE}" >&2
    echo "Current KATA_PACKAGE_RELEASE: ${CURRENT_KATA_RELEASE}" >&2
    echo "" >&2
    echo "Please increment KATA_PACKAGE_RELEASE in KATA_VERSION and run 'make generate'." >&2
    exit 1
  fi
fi

echo "Kata packaging changes validated: KATA_PACKAGE_RELEASE incremented (${BASE_KATA_RELEASE} -> ${CURRENT_KATA_RELEASE})."
exit 0
