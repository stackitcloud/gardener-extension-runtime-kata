package controlplane

import (
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
)

const (
	// WebhookName is the name of the OperatingSystemConfig mutating webhook.
	WebhookName = "runtime-kata"
	// WebhookPath is the HTTP path at which the webhook is served.
	WebhookPath = "runtime-kata"
)

var logger = log.Log.WithName("runtime-kata-webhook")

// AddToManager creates the OperatingSystemConfig mutating webhook for the seed and adds it to the manager.
func AddToManager(mgr manager.Manager) (*extensionswebhook.Webhook, error) {
	logger.Info("Adding webhook to manager")

	types := []extensionswebhook.Type{
		{Obj: &extensionsv1alpha1.OperatingSystemConfig{}},
	}

	handler, err := extensionswebhook.NewBuilder(mgr, logger).WithMutator(NewMutator(mgr, logger), types...).Build()
	if err != nil {
		return nil, err
	}

	return &extensionswebhook.Webhook{
		Name:    WebhookName,
		Action:  extensionswebhook.ActionMutating,
		Path:    WebhookPath,
		Target:  extensionswebhook.TargetSeed,
		Types:   types,
		Webhook: &admission.Webhook{Handler: handler, RecoverPanic: new(true)},
		// FailurePolicy Fail: Gardener labels both the shoot control plane namespace and
		// the worker pool OperatingSystemConfig with containerruntime.worker.gardener.cloud/<type>=true.
		// By matching on this label via NamespaceSelector and ObjectSelector, this webhook is strictly
		// scoped to OSCs of worker pools that requested the kata runtime. The blast radius is bounded,
		// making FailurePolicy Fail safe and guaranteeing that kata pools fail closed if the webhook
		// is unavailable.
		FailurePolicy:     new(admissionregistrationv1.Fail),
		NamespaceSelector: extensionswebhook.BuildContainerRuntimeTypeNamespaceSelector(kata.Type),
		ObjectSelector:    extensionswebhook.BuildContainerRuntimeTypeObjectSelector(kata.Type),
	}, nil
}
