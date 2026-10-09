// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
)

const (
	// Name is a name for a validation webhook.
	Name = "validator"
)

var (
	// DefaultAddOptions are the default AddOptions for configuring the validator.
	DefaultAddOptions = AddOptions{}
	logger            = log.Log.WithName("runtime-kata-validator-webhook")
)

// AddOptions are options to apply when adding the admission webhook to the manager.
type AddOptions struct {
	RequiredCapabilities []RequiredCapability
}

// New creates a new webhook that validates Shoot resources for Kata container runtime.
func New(mgr manager.Manager) (*extensionswebhook.Webhook, error) {
	logger.Info("Setting up webhook", "name", Name)

	return extensionswebhook.New(mgr, extensionswebhook.Args{
		Name: Name,
		Path: "/webhooks/validate",
		Validators: map[extensionswebhook.Validator][]extensionswebhook.Type{
			NewShootValidator(mgr, DefaultAddOptions.RequiredCapabilities): {{Obj: &gardencorev1beta1.Shoot{}}},
		},
		Target: extensionswebhook.TargetSeed,
		ObjectSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				v1beta1constants.LabelExtensionContainerRuntimeTypePrefix + kata.Type: "true",
			},
		},
	})
}
