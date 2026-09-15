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

	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/charts"
)

// KataManagedResourceName is the name of the managed resource holding the shoot-wide Kata
// prerequisites (the kata-qemu / kata-clh RuntimeClasses).
const KataManagedResourceName = "extension-runtime-kata"

// Reconcile deploys the shoot-wide RuntimeClasses. Everything node-local (the Kata binaries
// and the containerd runtime handlers) is delivered through the OperatingSystemConfig by the
// controlplane webhook (see pkg/webhook/controlplane), gated precisely on the worker pool.
func (a *actuator) Reconcile(ctx context.Context, log logr.Logger, cr *extensionsv1alpha1.ContainerRuntime, cluster *extensionscontroller.Cluster) error {
	chartRenderer, err := a.chartRendererFactory.NewChartRendererForShoot(cluster.Shoot.Spec.Kubernetes.Version)
	if err != nil {
		return fmt.Errorf("could not create chart renderer for shoot '%s', %w", cr.Namespace, err)
	}

	log.Info("Deploying Kata RuntimeClasses", "shoot", cluster.Shoot.Name, "shootNamespace", cluster.Shoot.Namespace, "workerPoolName", cr.Spec.WorkerPool.Name)
	kataChart, err := charts.RenderKataChart(chartRenderer)
	if err != nil {
		return err
	}

	return managedresources.CreateForShoot(ctx, a.client, cr.Namespace, KataManagedResourceName, "extension-runtime-kata", false, map[string][]byte{charts.KataConfigKey: kataChart})
}
