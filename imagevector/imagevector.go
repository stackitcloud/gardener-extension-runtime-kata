// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package imagevector

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"github.com/gardener/gardener/pkg/utils/imagevector"
	"k8s.io/apimachinery/pkg/util/runtime"

	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
)

var (
	//go:embed images.yaml
	imagesYAML  string
	imageVector imagevector.ImageVector
	caBundle    *imagevector.CABundle

	// validVersionRegexp matches valid Kata version strings extracted from image tags.
	// It requires an alphanumeric leading character followed by alphanumeric characters,
	// dots, hyphens, or underscores, preventing path traversal or script injection in downstream usages.
	validVersionRegexp = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

func init() {
	var err error

	imageVector, caBundle, err = imagevector.Read([]byte(imagesYAML))
	runtime.Must(err)

	_, err = ImageVector().FindImage(kata.RuntimeKataInstallationImageName)
	runtime.Must(err)
}

// ImageVector is the image vector that contains all the needed images,
// with any environment overrides applied.
func ImageVector() imagevector.ImageVector {
	iv, _, err := imagevector.WithEnvOverride(imageVector, caBundle, imagevector.OverrideEnv)
	runtime.Must(err)
	return iv
}

// FindInstallationImage resolves the installation image reference and its Kata version.
func FindInstallationImage() (string, string, error) {
	img, err := ImageVector().FindImage(kata.RuntimeKataInstallationImageName)
	if err != nil {
		return "", "", err
	}
	version, err := VersionFromImage(img)
	if err != nil {
		return "", "", err
	}
	return img.String(), version, nil
}

// VersionFromImage extracts the Kata version/tag from an installation image reference.
func VersionFromImage(img *imagevector.Image) (string, error) {
	if img == nil {
		return "", fmt.Errorf("image is nil")
	}

	var version string

	if img.Tag != nil {
		tag, _, _ := strings.Cut(*img.Tag, "@")
		if tag != "" && !strings.HasPrefix(tag, "sha256:") {
			version = tag
		}
	}

	if version == "" && img.Ref != nil {
		ref, _, _ := strings.Cut(*img.Ref, "@")
		path := ref[strings.LastIndex(ref, "/")+1:]
		if _, tag, ok := strings.Cut(path, ":"); ok && tag != "" && !strings.HasPrefix(tag, "sha256:") {
			version = tag
		}
	}

	if version == "" && img.Version != nil && *img.Version != "" && !strings.HasPrefix(*img.Version, "sha256:") {
		version = *img.Version
	}

	if version == "" {
		return "", fmt.Errorf("could not determine kata version from image %s", img.String())
	}

	if !validVersionRegexp.MatchString(version) {
		return "", fmt.Errorf("invalid kata version %q extracted from image %s", version, img.String())
	}

	return version, nil
}
