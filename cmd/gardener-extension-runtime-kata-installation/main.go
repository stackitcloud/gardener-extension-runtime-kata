// Command gardener-extension-runtime-kata-installation produces a data-only container image.
//
// The image contains the tarball builts using `make install-binaries`. This file is only a stub to
// allow using ko to generate the image. It is used to deliver the katatarball for using in the
// OperatingSystemConfig.
package main

import "fmt"

func main() {
	fmt.Println("gardener-extension-runtime-kata-installation is a data-only image and is not meant to be run")
}
