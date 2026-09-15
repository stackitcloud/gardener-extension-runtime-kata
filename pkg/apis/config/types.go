// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package config

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// KataConfiguration defines the provider configuration for the Kata Containers runtime resource.
//
// It is intentionally empty for now: the set of installed RuntimeClasses (kata-qemu, kata-clh)
// fully determines the behaviour on the node. The type exists to anchor the API group and to
// allow adding runtime-specific configuration in a backwards-compatible way in the future.
type KataConfiguration struct {
	metav1.TypeMeta
}
