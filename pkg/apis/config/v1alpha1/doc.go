// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

// +k8s:deepcopy-gen=package
// +k8s:conversion-gen=github.com/stackitcloud/gardener-extension-runtime-kata/pkg/apis/config
// +k8s:openapi-gen=true
// +k8s:defaulter-gen=TypeMeta

//go:generate crd-ref-docs --source-path=. --config=../../../../hack/api-reference/config-config.yaml --renderer=markdown --templates-dir=$GARDENER_HACK_DIR/api-reference/template --log-level=ERROR --output-path=../../../../hack/api-reference/config.md

// Package v1alpha1 contains the Kata Containers runtime configuration API resources.
// +groupName=kata.runtime.extensions.config.gardener.cloud
package v1alpha1
