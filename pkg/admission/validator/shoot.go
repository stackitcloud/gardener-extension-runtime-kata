// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"context"
	"fmt"
	"slices"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/api/core/v1beta1/helper"
	"github.com/gardener/gardener/pkg/apis/core"
	gardencoreinstall "github.com/gardener/gardener/pkg/apis/core/install"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gardenerutils "github.com/gardener/gardener/pkg/utils/gardener"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
)

var gardenCoreScheme = runtime.NewScheme()

func init() {
	utilruntime.Must(gardencoreinstall.AddToScheme(gardenCoreScheme))
}

// RequiredCapability specifies a machine capability key and required value.
type RequiredCapability struct {
	Name  string
	Value string
}

// NewShootValidator returns a new instance of a Shoot validator.
func NewShootValidator(mgr manager.Manager, requiredCapabilities []RequiredCapability) extensionswebhook.Validator {
	return NewShootValidatorWithClient(mgr.GetClient(), requiredCapabilities)
}

// NewShootValidatorWithClient returns a new instance of a Shoot validator with the given client.
func NewShootValidatorWithClient(c client.Client, requiredCapabilities []RequiredCapability) extensionswebhook.Validator {
	return &shootValidator{
		client:               c,
		requiredCapabilities: requiredCapabilities,
	}
}

type shootValidator struct {
	client               client.Client
	requiredCapabilities []RequiredCapability
}

// Validate validates the given Shoot object.
func (s *shootValidator) Validate(ctx context.Context, newObj, oldObj client.Object) error {
	shoot, err := s.extractShoot(newObj)
	if err != nil {
		return err
	}

	if shoot.DeletionTimestamp != nil {
		return nil
	}

	if gardencorev1beta1helper.IsWorkerless(shoot) {
		return nil
	}

	if oldObj != nil {
		oldShoot, err := s.extractShoot(oldObj)
		if err != nil {
			return err
		}
		if apiequality.Semantic.DeepEqual(oldShoot.Spec, shoot.Spec) {
			return nil
		}
	}

	var kataWorkers []struct {
		index  int
		worker gardencorev1beta1.Worker
	}
	for i, w := range shoot.Spec.Provider.Workers {
		if workerUsesKata(w) {
			kataWorkers = append(kataWorkers, struct {
				index  int
				worker gardencorev1beta1.Worker
			}{index: i, worker: w})
		}
	}

	if len(kataWorkers) == 0 || len(s.requiredCapabilities) == 0 {
		return nil
	}

	cloudProfile, err := gardenerutils.GetCloudProfile(ctx, s.client, shoot)
	if err != nil {
		return fmt.Errorf("failed to get cloud profile for shoot %s/%s: %w", shoot.Namespace, shoot.Name, err)
	}

	var allErrs field.ErrorList
	for _, kw := range kataWorkers {
		workerPath := field.NewPath("spec", "provider", "workers").Index(kw.index)
		machineType := gardencorev1beta1helper.FindMachineTypeByName(cloudProfile.Spec.MachineTypes, kw.worker.Machine.Type)
		if machineType == nil {
			allErrs = append(allErrs, field.NotFound(
				workerPath.Child("machine", "type"),
				kw.worker.Machine.Type,
			))
			continue
		}

		for _, reqCap := range s.requiredCapabilities {
			values, ok := machineType.Capabilities[reqCap.Name]
			if !ok || !slices.Contains(values, reqCap.Value) {
				allErrs = append(allErrs, field.Forbidden(
					workerPath.Child("machine", "type"),
					fmt.Sprintf("machine type %q for worker pool %q does not have required capability %s=%q in CloudProfile %q", kw.worker.Machine.Type, kw.worker.Name, reqCap.Name, reqCap.Value, cloudProfile.Name),
				))
			}
		}
	}

	return allErrs.ToAggregate()
}

func (s *shootValidator) extractShoot(obj client.Object) (*gardencorev1beta1.Shoot, error) {
	switch o := obj.(type) {
	case *gardencorev1beta1.Shoot:
		return o, nil
	case *core.Shoot:
		shoot := &gardencorev1beta1.Shoot{}
		if err := gardenCoreScheme.Convert(o, shoot, nil); err != nil {
			return nil, fmt.Errorf("failed to convert core.Shoot to v1beta1.Shoot: %w", err)
		}
		return shoot, nil
	default:
		return nil, fmt.Errorf("expected Shoot, but got %T", obj)
	}
}

func workerUsesKata(worker gardencorev1beta1.Worker) bool {
	if worker.CRI == nil {
		return false
	}
	for _, cr := range worker.CRI.ContainerRuntimes {
		if cr.Type == kata.Type {
			return true
		}
	}
	return false
}
