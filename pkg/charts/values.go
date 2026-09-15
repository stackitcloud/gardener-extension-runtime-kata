// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package charts

import (
	"github.com/gardener/gardener/pkg/chartrenderer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stackitcloud/gardener-extension-runtime-kata/charts"
	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
)

// KataConfigKey is the key under which the rendered chart is stored in the ManagedResource secret.
const KataConfigKey = "config.yaml"

// RenderKataChart renders the Kata chart containing the shoot-wide prerequisites: the RuntimeClasses
// (kata-qemu, kata-clh).
func RenderKataChart(renderer chartrenderer.Interface) ([]byte, error) {
	release, err := renderer.RenderEmbeddedFS(charts.InternalChart, kata.ChartPath, kata.ReleaseName, metav1.NamespaceSystem, map[string]any{})
	if err != nil {
		return nil, err
	}
	return release.Manifest(), nil
}
