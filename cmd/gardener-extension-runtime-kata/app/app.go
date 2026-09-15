// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"fmt"
	"os"

	"github.com/gardener/gardener/extensions/pkg/controller"
	controllercmd "github.com/gardener/gardener/extensions/pkg/controller/cmd"
	"github.com/gardener/gardener/extensions/pkg/controller/heartbeat"
	heartbeatcmd "github.com/gardener/gardener/extensions/pkg/controller/heartbeat/cmd"
	webhookcmd "github.com/gardener/gardener/extensions/pkg/webhook/cmd"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/component-base/version/verflag"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	katacontroller "github.com/stackitcloud/gardener-extension-runtime-kata/pkg/controller"
	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/healthcheck"
	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
	controlplanewebhook "github.com/stackitcloud/gardener-extension-runtime-kata/pkg/webhook/controlplane"
)

// NewControllerManagerCommand creates a new command that is used to start the Kata container runtime controller.
func NewControllerManagerCommand(ctx context.Context) *cobra.Command {
	var (
		generalOpts = &controllercmd.GeneralOptions{}
		restOpts    = &controllercmd.RESTOptions{}
		mgrOpts     = &controllercmd.ManagerOptions{
			LeaderElection:          true,
			LeaderElectionID:        controllercmd.LeaderElectionNameID(kata.Name),
			LeaderElectionNamespace: os.Getenv("LEADER_ELECTION_NAMESPACE"),
			WebhookServerPort:       443,
			WebhookCertDir:          "/tmp/gardener-extensions-cert",
		}
		reconcileOpts = &controllercmd.ReconcilerOptions{}

		// options for the runtime-kata controller
		kataCtrlOpts = &controllercmd.ControllerOptions{
			MaxConcurrentReconciles: 5,
		}

		// options for the health care controller
		healthCheckCtrlOpts = &controllercmd.ControllerOptions{
			MaxConcurrentReconciles: 5,
		}

		// options for the heartbeat controller
		heartbeatCtrlOpts = &heartbeatcmd.Options{
			ExtensionName:        kata.Name,
			RenewIntervalSeconds: 30,
			Namespace:            os.Getenv("LEADER_ELECTION_NAMESPACE"),
		}

		// options for the webhook server
		webhookServerOptions = &webhookcmd.ServerOptions{
			Namespace: os.Getenv("WEBHOOK_CONFIG_NAMESPACE"),
		}
		webhookSwitches = webhookcmd.NewSwitchOptions(
			webhookcmd.Switch(controlplanewebhook.WebhookName, controlplanewebhook.AddToManager),
		)
		// This extension only registers Seed-target webhooks, so the shoot webhook managed-resource
		// name and namespace selector are left empty.
		webhookOptions = webhookcmd.NewAddToManagerOptions(
			kata.Name,
			"",
			nil,
			generalOpts,
			webhookServerOptions,
			webhookSwitches,
		)

		aggOption = controllercmd.NewOptionAggregator(
			generalOpts,
			restOpts,
			mgrOpts,
			kataCtrlOpts,
			controllercmd.PrefixOption("healthcheck-", healthCheckCtrlOpts),
			controllercmd.PrefixOption("heartbeat-", heartbeatCtrlOpts),
			reconcileOpts,
			webhookOptions,
		)
	)

	cmd := &cobra.Command{
		Use: fmt.Sprintf("%s-controller-manager", kata.Name),

		RunE: func(_ *cobra.Command, _ []string) error {
			// Act on version flag, if one was specified
			verflag.PrintAndExitIfRequested()

			if err := aggOption.Complete(); err != nil {
				return fmt.Errorf("error completing options: %w", err)
			}

			if err := heartbeatCtrlOpts.Validate(); err != nil {
				return err
			}

			completedMgrOpts := mgrOpts.Completed().Options()
			completedMgrOpts.Client = client.Options{
				Cache: &client.CacheOptions{
					DisableFor: []client.Object{
						&corev1.Secret{}, // applied for ManagedResources
					},
				},
			}

			mgr, err := manager.New(restOpts.Completed().Config, completedMgrOpts)
			if err != nil {
				return fmt.Errorf("could not instantiate manager: %w", err)
			}

			if err := controller.AddToScheme(mgr.GetScheme()); err != nil {
				return fmt.Errorf("could not update manager scheme: %w", err)
			}

			reconcileOpts.Completed().Apply(&katacontroller.DefaultAddOptions.IgnoreOperationAnnotation)
			katacontroller.DefaultAddOptions.ExtensionClasses = generalOpts.Completed().ExtensionClasses
			kataCtrlOpts.Completed().Apply(&katacontroller.DefaultAddOptions.Controller)
			heartbeatCtrlOpts.Completed().Apply(&heartbeat.DefaultAddOptions)

			if _, err := webhookOptions.Completed().AddToManager(ctx, mgr, nil); err != nil {
				return fmt.Errorf("could not add webhooks to manager: %w", err)
			}

			if err := katacontroller.AddToManager(ctx, mgr); err != nil {
				return fmt.Errorf("could not add controllers to manager: %w", err)
			}

			if err := healthcheck.AddToManager(mgr); err != nil {
				return fmt.Errorf("could not add health check controller to manager: %w", err)
			}

			if err := heartbeat.AddToManager(ctx, mgr); err != nil {
				return fmt.Errorf("could not add heartbeat controller to manager: %w", err)
			}

			if err := mgr.AddReadyzCheck("webhook-server", mgr.GetWebhookServer().StartedChecker()); err != nil {
				return fmt.Errorf("could not add readiness check for webhook server to manager: %w", err)
			}

			if err := mgr.Start(ctx); err != nil {
				return fmt.Errorf("error running manager: %w", err)
			}

			return nil
		},
	}

	verflag.AddFlags(cmd.Flags())
	aggOption.AddFlags(cmd.Flags())

	return cmd
}
