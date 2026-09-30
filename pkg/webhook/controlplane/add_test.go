package controlplane

import (
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	gardenerutils "github.com/gardener/gardener/pkg/utils/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
)

var _ = Describe("Add", func() {
	var (
		scheme = runtime.NewScheme()
		mgr    *gardenerutils.FakeManager
	)

	BeforeEach(func() {
		utilruntime.Must(extensionsv1alpha1.AddToScheme(scheme))
		fakeClient := fakeclient.NewClientBuilder().WithScheme(scheme).Build()
		mgr = &gardenerutils.FakeManager{Scheme: scheme, Client: fakeClient}
	})

	Describe("#AddToManager", func() {
		It("should successfully create the mutating webhook with selectors and Fail failure policy", func() {
			webhook, err := AddToManager(mgr)
			Expect(err).NotTo(HaveOccurred())
			Expect(webhook).NotTo(BeNil())

			Expect(webhook.Name).To(Equal(WebhookName))
			Expect(webhook.Path).To(Equal(WebhookPath))
			Expect(webhook.Action).To(Equal(extensionswebhook.ActionMutating))
			Expect(webhook.Target).To(Equal(extensionswebhook.TargetSeed))
			Expect(webhook.FailurePolicy).To(Equal(new(admissionregistrationv1.Fail)))

			Expect(webhook.NamespaceSelector).To(Equal(extensionswebhook.BuildContainerRuntimeTypeNamespaceSelector(kata.Type)))
			Expect(webhook.ObjectSelector).To(Equal(extensionswebhook.BuildContainerRuntimeTypeObjectSelector(kata.Type)))

			expectedSelector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "containerruntime.worker.gardener.cloud/" + kata.Type,
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"true"},
					},
				},
			}
			Expect(webhook.NamespaceSelector).To(Equal(expectedSelector))
			Expect(webhook.ObjectSelector).To(Equal(expectedSelector))

			Expect(webhook.Types).To(ConsistOf(extensionswebhook.Type{
				Obj: &extensionsv1alpha1.OperatingSystemConfig{},
			}))
		})
	})
})
