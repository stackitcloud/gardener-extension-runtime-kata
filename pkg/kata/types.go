package kata

import (
	"path/filepath"

	"github.com/stackitcloud/gardener-extension-runtime-kata/charts"
)

const (
	// Name is a constant to identify the Kata Containers extension.
	Name = "runtime-kata"

	// InstallationDir is the parent directory into which the version-stamped Kata artifacts are
	// unpacked on the node. `/opt` is writable on both Flatcar and Ubuntu.
	InstallationDir = "/opt/kata"

	// BinDir is the directory in which the kata-runtime and kata-collect-data binaries are located.
	BinDir = "/opt/kata/bin"

	// RuntimeKataInstallationImageName is the image name of the installation image (a data-only image
	// carrying the kata-static tarball) as referenced in imagevector/images.yaml. gardener-node-agent
	// pulls it and extracts the tarball onto the node via an OperatingSystemConfig file imageRef.
	RuntimeKataInstallationImageName = "runtime-kata-installation"

	// ReleaseName is the name of the Kata Helm release (RuntimeClasses).
	ReleaseName = "kata"
)

var (
	// Version is the Kata Containers release that this extension installs and configures.
	// Injected at build time via -ldflags "-X github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata.Version=<version>".
	Version = "0.0.0-dev"

	// PackageRelease is a counter that can be incremented for packaging changes of kata that
	// do not update the kata version at the same time.
	// Injected at build time via -ldflags "-X github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata.PackageRelease=<release>".
	PackageRelease = "0"

	// PackageVersion is the Kata Containers release including a package release counter
	PackageVersion = Version + "-" + PackageRelease

	// ChartPath is the path to the internal Kata chart.
	ChartPath = filepath.Join(charts.InternalChartsPath, "kata")
)
