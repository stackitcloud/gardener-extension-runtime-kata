package charts_test

import (
	extensioncontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/util"
	"github.com/gardener/gardener/pkg/chartrenderer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/charts"
)

var _ = Describe("Chart package test", func() {
	Describe("#RenderKataChart", func() {
		var (
			chartRenderer chartrenderer.Interface
		)

		BeforeEach(func() {
			var err error
			chartRenderer, err = extensioncontroller.ChartRendererFactoryFunc(util.NewChartRendererForShoot).NewChartRendererForShoot("1.33")
			Expect(err).ToNot(HaveOccurred())
		})

		It("Render Kata chart correctly", func() {
			chart, err := charts.RenderKataChart(chartRenderer)
			Expect(err).NotTo(HaveOccurred())
			Expect(chart).To(ContainSubstring("handler: kata-qemu"))
			Expect(chart).To(ContainSubstring("handler: kata-clh"))
		})
	})
})
