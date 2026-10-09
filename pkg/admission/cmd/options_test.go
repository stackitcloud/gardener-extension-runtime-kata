// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package cmd_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/pflag"

	admissioncmd "github.com/stackitcloud/gardener-extension-runtime-kata/pkg/admission/cmd"
	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/admission/validator"
)

var _ = Describe("Admission Cmd Options", func() {
	It("correctly parses valid required-capability flags", func() {
		opts := &admissioncmd.ConfigOptions{}
		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		opts.AddFlags(fs)

		err := fs.Parse([]string{
			"--required-capability=containerRuntime=kata",
			"--required-capability=architecture=amd64",
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(opts.Complete()).To(Succeed())
		cfg := opts.Completed()
		Expect(cfg.RequiredCapabilities).To(Equal([]validator.RequiredCapability{
			{Name: "containerRuntime", Value: "kata"},
			{Name: "architecture", Value: "amd64"},
		}))
	})

	It("fails when required-capability flag format is invalid", func() {
		opts := &admissioncmd.ConfigOptions{}
		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		opts.AddFlags(fs)

		err := fs.Parse([]string{"--required-capability=invalidFormat"})
		Expect(err).NotTo(HaveOccurred())

		err = opts.Complete()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid required capability"))
	})

	It("applies required capabilities to target slice", func() {
		cfg := &admissioncmd.Config{
			RequiredCapabilities: []validator.RequiredCapability{
				{Name: "containerRuntime", Value: "kata"},
			},
		}

		var target []validator.RequiredCapability
		cfg.ApplyRequiredCapabilities(&target)
		Expect(target).To(Equal([]validator.RequiredCapability{
			{Name: "containerRuntime", Value: "kata"},
		}))
	})
})
