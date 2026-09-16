// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package imagevector_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackitcloud/gardener-extension-runtime-kata/imagevector"
	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
)

var _ = Describe("ImageVector", func() {
	Describe("#ImageVector", func() {
		It("should return a non-empty ImageVector", func() {
			vector := imagevector.ImageVector()
			Expect(vector).NotTo(BeEmpty())
		})

		It("should successfully find the runtime-kata-installation image", func() {
			image, err := imagevector.ImageVector().FindImage(kata.RuntimeKataInstallationImageName)
			Expect(err).NotTo(HaveOccurred())
			Expect(image).NotTo(BeNil())
			Expect(image.Name).To(Equal("runtime-kata-installation"))
			Expect(image.Repository).NotTo(BeNil())
			Expect(*image.Repository).To(Equal("ghcr.io/stackitcloud/gardener-extension-runtime-kata/gardener-extension-runtime-kata-installation"))
		})

		It("should return an error when the image is not found", func() {
			image, err := imagevector.ImageVector().FindImage("non-existing-image")
			Expect(err).To(HaveOccurred())
			Expect(image).To(BeNil())
		})

		It("should validate that all image entries have valid attributes", func() {
			for _, image := range imagevector.ImageVector() {
				Expect(image.Name).NotTo(BeEmpty(), "image name must not be empty")
				Expect(image.Repository).NotTo(BeNil(), "image %s repository must not be nil", image.Name)
				Expect(*image.Repository).NotTo(BeEmpty(), "image %s repository must not be empty", image.Name)
			}
		})
	})
})
