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
	Describe("#FindImage", func() {
		It("resolves the installation image with a valid repository and tag", func() {
			img := imagevector.FindImage(kata.RuntimeKataInstallationImageName)
			Expect(img).NotTo(BeEmpty())
			Expect(img).To(ContainSubstring(kata.RuntimeKataInstallationImageName))
			Expect(img).NotTo(ContainSubstring("$Format:"))
			Expect(img).NotTo(ContainSubstring("$"))
			Expect(img).NotTo(ContainSubstring("%"))
			Expect(img).To(MatchRegexp(`^.+/[a-zA-Z0-9_.-]+:[a-zA-Z0-9_.-]+$`))
		})
	})
})
