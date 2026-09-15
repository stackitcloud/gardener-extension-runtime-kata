package controlplane

import (
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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
		// FailurePolicy Ignore: this webhook matches EVERY OperatingSystemConfig in EVERY shoot
		// namespace on the seed (the OSC carries no label identifying kata pools, so it cannot be
		// scoped - see below). For seed-target webhooks Gardener defaults to Fail, which would block
		// OSC reconciliation for ALL shoots on the seed whenever this optional runtime extension is
		// unavailable. That blast radius is disproportionate, so we fail open: if the webhook is down,
		// affected kata pools come up without Kata (detectable, self-healed on the next OSC reconcile)
		// instead of stalling node provisioning cluster-wide. Availability is covered by the extension
		// heartbeat + webhook readyz check and should be monitored.
		//
		// TODO(runtime-kata): once we can scope this webhook to kata OSCs, switch back to Fail to get a
		// correctness guarantee for kata pools. Blocker: the OSC object carries no runtime marker
		// (neither a label nor a spec field - see gardener NodeLabelsForWorkerPool, which stamps
		// `containerruntime.worker.gardener.cloud/<type>=true` onto NODES but not onto the OSC).
		// Upstream fix to contribute: have gardenlet apply the same existing ContainerRuntimeNameWorkerLabel
		// to the OperatingSystemConfig at creation (a few lines in
		// pkg/component/extensions/operatingsystemconfig, mirroring the node-label logic). Then this
		// webhook can use an ObjectSelector on that label, bounding the blast radius to kata pools and
		// making Fail safe again.
		FailurePolicy: new(admissionregistrationv1.Ignore),
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				// Select all shoots as we currently cannot gate on shoots using the runtime class only
				v1beta1constants.GardenRole: v1beta1constants.GardenRoleShoot,
			},
		},
	}, nil
}
