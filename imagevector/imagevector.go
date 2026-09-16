// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package imagevector

import (
	_ "embed"
	"strings"

	"github.com/gardener/gardener/pkg/utils/imagevector"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/version"

	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
)

var (
	//go:embed images.yaml
	imagesYAML  string
	imageVector imagevector.ImageVector
	caBundle    *imagevector.CABundle
)

func init() {
	var err error

	imageVector, caBundle, err = imagevector.Read([]byte(imagesYAML))
	runtime.Must(err)
	// image vector for components deployed by the Kata Containers extension
	imageVector, caBundle, err = imagevector.WithEnvOverride(imageVector, caBundle, imagevector.OverrideEnv)
	runtime.Must(err)

	_, err = imageVector.FindImage(kata.RuntimeKataInstallationImageName)
	runtime.Must(err)
}

// ImageVector is the image vector that contains all the needed images.
func ImageVector() imagevector.ImageVector {
	return imageVector
}

// FindImage returns the container runtime Kata Containers installation image.
func FindImage(name string) string {
	image, err := imageVector.FindImage(name)
	runtime.Must(err)

	if image.Ref != nil {
		return image.String()
	}

	var (
		repository = image.String()
		tag        = version.Get().GitVersion
	)
	if image.Tag != nil {
		repository = *image.Repository
		tag = *image.Tag
	} else if strings.Contains(tag, "$Format:") || tag == "" {
		tag = "v0.0.0"
	}
	calculatedImage := imagevector.Image{
		Repository: &repository,
		Tag:        &tag,
	}
	return calculatedImage.String()
}
