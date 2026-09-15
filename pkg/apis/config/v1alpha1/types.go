// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// KataConfiguration defines the provider configuration for the Kata Containers runtime extension.
//
// It is set as `providerConfig` on a worker pool's ContainerRuntime of type `kata`. It is
// intentionally empty for now: the installed RuntimeClasses (kata-qemu, kata-clh) fully determine
// the node behaviour. The type is kept so that runtime-specific configuration can be added later
// without breaking the API contract.
type KataConfiguration struct {
	metav1.TypeMeta `json:",inline"`
}
