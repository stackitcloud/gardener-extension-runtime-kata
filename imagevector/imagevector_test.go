// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package imagevector_test

import (
	"os"
	"path/filepath"

	gardenerimagevector "github.com/gardener/gardener/pkg/utils/imagevector"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackitcloud/gardener-extension-runtime-kata/imagevector"
	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
)

var _ = Describe("ImageVector", func() {
	Describe("#FindImage", func() {
		It("resolves the installation image with a valid repository and tag", func() {
			iv := imagevector.ImageVector()
			img, err := iv.FindImage(kata.RuntimeKataInstallationImageName)
			Expect(err).NotTo(HaveOccurred())
			Expect(img.String()).NotTo(BeEmpty())
			Expect(img.String()).To(ContainSubstring(kata.RuntimeKataInstallationImageName))
			Expect(img.String()).NotTo(ContainSubstring("$Format:"))
			Expect(img.String()).NotTo(ContainSubstring("$"))
			Expect(img.String()).NotTo(ContainSubstring("%"))
			Expect(img.String()).To(MatchRegexp(`^.+/[a-zA-Z0-9_.-]+:[a-zA-Z0-9_.-]+$`))
		})
	})

	Describe("#FindInstallationImage", func() {
		It("resolves the installation image and returns the correct version", func() {
			img, version, err := imagevector.FindInstallationImage()
			Expect(err).NotTo(HaveOccurred())
			Expect(img).NotTo(BeEmpty())
			Expect(version).NotTo(BeEmpty())
			Expect(img).To(HaveSuffix(":" + version))
		})

		It("respects IMAGEVECTOR_OVERWRITE when set", func() {
			tempDir := GinkgoT().TempDir()
			overwriteFile := filepath.Join(tempDir, "overwrite.yaml")
			overwriteContent := `images:
  - name: runtime-kata-installation
    repository: my-custom-registry.io/kata-install
    tag: v9.9.9-1
`
			Expect(os.WriteFile(overwriteFile, []byte(overwriteContent), 0600)).To(Succeed())
			GinkgoT().Setenv("IMAGEVECTOR_OVERWRITE", overwriteFile)

			img, version, err := imagevector.FindInstallationImage()
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal("v9.9.9-1"))
			Expect(img).To(Equal("my-custom-registry.io/kata-install:v9.9.9-1"))
		})
	})

	Describe("#VersionFromImage", func() {
		It("extracts version from image with tag", func() {
			img := &gardenerimagevector.Image{
				Repository: new("example.com/repo"),
				Tag:        new("v4.2.0-1"),
			}
			ver, err := imagevector.VersionFromImage(img)
			Expect(err).NotTo(HaveOccurred())
			Expect(ver).To(Equal("v4.2.0-1"))
		})

		It("extracts version from image with tag containing digest", func() {
			img := &gardenerimagevector.Image{
				Repository: new("example.com/repo"),
				Tag:        new("v4.2.0-1@sha256:1234567890abcdef"),
			}
			ver, err := imagevector.VersionFromImage(img)
			Expect(err).NotTo(HaveOccurred())
			Expect(ver).To(Equal("v4.2.0-1"))
		})

		It("extracts version from image with ref having tag", func() {
			img := &gardenerimagevector.Image{
				Ref: new("ghcr.io/stackitcloud/kata-installation:v4.2.0-1"),
			}
			ver, err := imagevector.VersionFromImage(img)
			Expect(err).NotTo(HaveOccurred())
			Expect(ver).To(Equal("v4.2.0-1"))
		})

		It("extracts version from image with ref having registry port and tag", func() {
			img := &gardenerimagevector.Image{
				Ref: new("localhost:5001/stackitcloud/kata-installation:v4.2.0-1"),
			}
			ver, err := imagevector.VersionFromImage(img)
			Expect(err).NotTo(HaveOccurred())
			Expect(ver).To(Equal("v4.2.0-1"))
		})

		It("extracts version from image with ref having tag and digest", func() {
			img := &gardenerimagevector.Image{
				Ref: new("example.com/org/repo:3.2.0-1@sha256:abcdef123456"),
			}
			ver, err := imagevector.VersionFromImage(img)
			Expect(err).NotTo(HaveOccurred())
			Expect(ver).To(Equal("3.2.0-1"))
		})

		It("falls back to Version when tag is a digest", func() {
			img := &gardenerimagevector.Image{
				Repository: new("example.com/repo"),
				Tag:        new("sha256:1234567890abcdef"),
				Version:    new("4.1.0-1"),
			}
			ver, err := imagevector.VersionFromImage(img)
			Expect(err).NotTo(HaveOccurred())
			Expect(ver).To(Equal("4.1.0-1"))
		})

		It("returns error when image is nil", func() {
			_, err := imagevector.VersionFromImage(nil)
			Expect(err).To(MatchError(ContainSubstring("image is nil")))
		})

		It("returns error when no tag or version can be determined", func() {
			img := &gardenerimagevector.Image{
				Repository: new("example.com/repo"),
			}
			_, err := imagevector.VersionFromImage(img)
			Expect(err).To(HaveOccurred())
		})
	})
})
