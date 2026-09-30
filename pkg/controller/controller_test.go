// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package controller_test

import (
	"context"

	extensioncontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/containerruntime"
	"github.com/gardener/gardener/extensions/pkg/util"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/controller"
)

const (
	shootVersion = "1.33.0"
)

var _ = Describe("Controller tests", func() {
	Describe("#Actuator", func() {
		var (
			ctx context.Context
			c   client.Client

			managedResourceName   string
			managedResource       *resourcesv1alpha1.ManagedResource
			managedResourceSecret *corev1.Secret

			cr  *extensionsv1alpha1.ContainerRuntime
			cr2 *extensionsv1alpha1.ContainerRuntime

			a containerruntime.Actuator

			log = logf.Log.WithName("test")

			namespaceName = "namespace"
			workerGroup   = "worker-kata"

			cluster = &extensioncontroller.Cluster{
				Shoot: &gardencorev1beta1.Shoot{
					Spec: gardencorev1beta1.ShootSpec{
						Kubernetes: gardencorev1beta1.Kubernetes{
							Version: shootVersion,
						},
					},
				},
			}
		)

		BeforeEach(func() {
			ctx = context.TODO()
			c = fake.NewClientBuilder().WithScheme(kubernetes.SeedScheme).Build()
			a = controller.NewActuator(c, extensioncontroller.ChartRendererFactoryFunc(util.NewChartRendererForShoot))

			managedResourceName = "extension-runtime-kata"
			managedResource = &resourcesv1alpha1.ManagedResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      managedResourceName,
					Namespace: namespaceName,
				},
			}
			managedResourceSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "managedresource-" + managedResource.Name,
					Namespace: namespaceName,
				},
			}

			cr = &extensionsv1alpha1.ContainerRuntime{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespaceName, Name: "test-cr"},
				Spec: extensionsv1alpha1.ContainerRuntimeSpec{
					BinaryPath: "/path/test",
					WorkerPool: extensionsv1alpha1.ContainerRuntimeWorkerPool{
						Name: workerGroup,
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{"worker.gardener.cloud/pool": "kata-pool"},
						},
					},
					DefaultSpec: extensionsv1alpha1.DefaultSpec{Type: "kata"},
				},
			}

			cr2 = &extensionsv1alpha1.ContainerRuntime{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespaceName, Name: "test-cr-2"},
				Spec: extensionsv1alpha1.ContainerRuntimeSpec{
					BinaryPath: "/path/test",
					WorkerPool: extensionsv1alpha1.ContainerRuntimeWorkerPool{
						Name: workerGroup + "-2",
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{"worker.gardener.cloud/pool": "kata-pool-2"},
						},
					},
					DefaultSpec: extensionsv1alpha1.DefaultSpec{Type: "kata"},
				},
			}
		})

		expectManagedResourceExists := func() {
			Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResource), managedResource)).To(Succeed())
			managedResourceSecret.Name = managedResource.Spec.SecretRefs[0].Name
			Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResourceSecret), managedResourceSecret)).To(Succeed())
			Expect(managedResourceSecret.Immutable).To(Equal(new(true)))
			Expect(managedResourceSecret.Data).To(HaveLen(1))
		}

		It("Should successfully deploy the shoot-wide RuntimeClasses managed resource", func() {
			Expect(c.Create(ctx, cr)).To(Succeed())
			Expect(a.Reconcile(ctx, log, cr, cluster)).NotTo(HaveOccurred())
			expectManagedResourceExists()
		})

		It("Should keep a single managed resource across multiple kata worker pools", func() {
			Expect(c.Create(ctx, cr)).To(Succeed())
			Expect(a.Reconcile(ctx, log, cr, cluster)).NotTo(HaveOccurred())
			Expect(c.Create(ctx, cr2)).To(Succeed())
			Expect(a.Reconcile(ctx, log, cr2, cluster)).NotTo(HaveOccurred())
			expectManagedResourceExists()
		})

		It("Should delete the managed resource once the last kata worker pool is removed", func() {
			Expect(c.Create(ctx, cr)).To(Succeed())
			Expect(a.Reconcile(ctx, log, cr, cluster)).NotTo(HaveOccurred())
			expectManagedResourceExists()

			Expect(a.Delete(ctx, log, cr, cluster)).NotTo(HaveOccurred())
			Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResource), managedResource)).To(BeNotFoundError())
			Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResourceSecret), managedResourceSecret)).To(BeNotFoundError())
		})

		It("Should keep the managed resource while another kata worker pool still needs it", func() {
			Expect(c.Create(ctx, cr)).To(Succeed())
			Expect(a.Reconcile(ctx, log, cr, cluster)).NotTo(HaveOccurred())
			Expect(c.Create(ctx, cr2)).To(Succeed())
			Expect(a.Reconcile(ctx, log, cr2, cluster)).NotTo(HaveOccurred())

			Expect(a.Delete(ctx, log, cr, cluster)).NotTo(HaveOccurred())
			expectManagedResourceExists()
		})
	})
})
