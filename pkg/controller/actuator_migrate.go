// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/utils/managedresources"
	"github.com/go-logr/logr"
)

// Migrate implements ContainerRuntime.Actuator.
func (a *actuator) Migrate(ctx context.Context, log logr.Logger, cr *extensionsv1alpha1.ContainerRuntime, _ *extensionscontroller.Cluster) error {
	// We can directly set `keepObjects=true` and delete the Kata ManagedResource because all
	// ContainerRuntimes are migrated during control plane migration. If the ManagedResource was already
	// deleted (e.g. by the migration of another kata worker pool) no error is returned.
	log.Info("Setting keepObjects=true as part of the migration operation", "managedResourceName", KataManagedResourceName)
	if err := managedresources.SetKeepObjects(ctx, a.client, cr.Namespace, KataManagedResourceName, true); err != nil {
		return fmt.Errorf("could not keep objects of managed resource %q: %w", KataManagedResourceName, err)
	}

	log.Info("Deleting managed resource as part of the migration operation", "managedResourceName", KataManagedResourceName)
	if err := a.deleteManagedResource(ctx, cr.Namespace, KataManagedResourceName, false); err != nil {
		return fmt.Errorf("could not delete managed resource %q: %w", KataManagedResourceName, err)
	}

	return nil
}
