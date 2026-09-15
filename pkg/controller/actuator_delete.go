// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	v1beta1helper "github.com/gardener/gardener/pkg/api/core/v1beta1/helper"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
)

// Delete implements ContainerRuntime.Actuator.
func (a *actuator) Delete(ctx context.Context, log logr.Logger, cr *extensionsv1alpha1.ContainerRuntime, cluster *extensionscontroller.Cluster) error {
	forceDelete := cluster != nil && v1beta1helper.ShootNeedsForceDeletion(cluster.Shoot)

	// The RuntimeClasses are shoot-wide, so only delete the managed resource once no worker pool of the
	// shoot requires kata any more.
	list := &extensionsv1alpha1.ContainerRuntimeList{}
	if err := a.client.List(ctx, list, client.InNamespace(cr.Namespace)); err != nil {
		return err
	}

	if isKataStillRequired(cr.Name, list) {
		log.Info("Kata is still required in the cluster - keeping the RuntimeClasses")
		return nil
	}

	log.Info("Deleting managed resource - no worker pool in the Shoot cluster requires kata any more", "managedResourceName", KataManagedResourceName)
	return a.deleteManagedResource(ctx, cr.Namespace, KataManagedResourceName, forceDelete)
}

func isKataStillRequired(name string, list *extensionsv1alpha1.ContainerRuntimeList) bool {
	for _, cr := range list.Items {
		if cr.Name != name && cr.Spec.Type == kata.Type && cr.DeletionTimestamp == nil {
			return true
		}
	}
	return false
}

// ForceDelete implements ContainerRuntime.Actuator.
func (a *actuator) ForceDelete(ctx context.Context, log logr.Logger, cr *extensionsv1alpha1.ContainerRuntime, cluster *extensionscontroller.Cluster) error {
	return a.Delete(ctx, log, cr, cluster)
}
