// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"strings"

	webhookcmd "github.com/gardener/gardener/extensions/pkg/webhook/cmd"
	"github.com/spf13/pflag"

	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/admission/validator"
)

// GardenWebhookSwitchOptions are the webhookcmd.SwitchOptions for the admission webhooks.
func GardenWebhookSwitchOptions() *webhookcmd.SwitchOptions {
	return webhookcmd.NewSwitchOptions(
		webhookcmd.Switch(validator.Name, validator.New),
	)
}

// ConfigOptions are command line options that can be set for the admission webhook.
type ConfigOptions struct {
	RawRequiredCapabilities []string
	config                  *Config
}

// Config is a completed admission configuration.
type Config struct {
	RequiredCapabilities []validator.RequiredCapability
}

// AddFlags implements Flagger.AddFlags.
func (c *ConfigOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(
		&c.RawRequiredCapabilities,
		"required-capability",
		nil,
		"Configures a required machine capability key-value pair for Kata (format: <name>=<value>). Can be specified multiple times.",
	)
}

// Complete parses and validates the options.
func (c *ConfigOptions) Complete() error {
	var caps []validator.RequiredCapability
	for _, raw := range c.RawRequiredCapabilities {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("invalid required capability %q, expected format <name>=<value>", raw)
		}
		caps = append(caps, validator.RequiredCapability{
			Name:  parts[0],
			Value: parts[1],
		})
	}

	c.config = &Config{
		RequiredCapabilities: caps,
	}
	return nil
}

// Completed returns the completed Config.
func (c *ConfigOptions) Completed() *Config {
	return c.config
}

// ApplyRequiredCapabilities applies the required capabilities to the validator AddOptions.
func (c *Config) ApplyRequiredCapabilities(target *[]validator.RequiredCapability) {
	if c != nil && len(c.RequiredCapabilities) > 0 {
		*target = c.RequiredCapabilities
	}
}
