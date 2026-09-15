#!/bin/bash
#
# SPDX-FileCopyrightText: Contributors to the Gardener project
#
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o nounset
set -o pipefail

PROJECT_ROOT="$(dirname "$0")"/..

CODE_GEN_DIR=$(go list -mod=mod -m -f '{{.Dir}}' k8s.io/code-generator)
# shellcheck disable=SC1091
source "${CODE_GEN_DIR}/kube_codegen.sh"

kube::codegen::gen_helpers \
  --boilerplate "${GARDENER_HACK_DIR}/LICENSE_BOILERPLATE.txt" \
    --extra-peer-dir k8s.io/apimachinery/pkg/apis/meta/v1 \
    --extra-peer-dir k8s.io/apimachinery/pkg/conversion \
    --extra-peer-dir k8s.io/component-base/config \
    --extra-peer-dir k8s.io/component-base/config/v1alpha1 \
  "${PROJECT_ROOT}/pkg/apis"
