// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validator_test

import (
	"context"

	gardencoreinstall "github.com/gardener/gardener/pkg/apis/core/install"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/admission/validator"
	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
)

var _ = Describe("Shoot Validator", func() {
	var (
		ctx                 context.Context
		scheme              *runtime.Scheme
		defaultRequiredCaps []validator.RequiredCapability
		cloudProfile        *gardencorev1beta1.CloudProfile
		shoot               *gardencorev1beta1.Shoot
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(gardencoreinstall.AddToScheme(scheme)).To(Succeed())

		defaultRequiredCaps = []validator.RequiredCapability{
			{Name: "containerRuntime", Value: "kata"},
		}

		cloudProfile = &gardencorev1beta1.CloudProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name: "stackit",
			},
			Spec: gardencorev1beta1.CloudProfileSpec{
				MachineCapabilities: []gardencorev1beta1.CapabilityDefinition{
					{Name: "containerRuntime", Values: []string{"kata"}},
					{Name: "architecture", Values: []string{"amd64"}},
				},
				MachineTypes: []gardencorev1beta1.MachineType{
					{
						Name: "b2i.4d",
						Capabilities: gardencorev1beta1.Capabilities{
							"containerRuntime": {"kata"},
							"architecture":     {"amd64"},
						},
					},
					{
						Name: "b1.2",
						Capabilities: gardencorev1beta1.Capabilities{
							"architecture": {"amd64"},
						},
					},
					{
						Name: "other.flavor",
						Capabilities: gardencorev1beta1.Capabilities{
							"containerRuntime": {"other"},
							"architecture":     {"amd64"},
						},
					},
				},
			},
		}

		shoot = &gardencorev1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-shoot",
				Namespace: "garden-test",
			},
			Spec: gardencorev1beta1.ShootSpec{
				CloudProfileName: new("stackit"),
				Provider: gardencorev1beta1.Provider{
					Workers: []gardencorev1beta1.Worker{
						{
							Name: "worker-pool-1",
							Machine: gardencorev1beta1.Machine{
								Type: "b2i.4d",
							},
							CRI: &gardencorev1beta1.CRI{
								Name: "containerd",
								ContainerRuntimes: []gardencorev1beta1.ContainerRuntime{
									{Type: kata.Type},
								},
							},
						},
					},
				},
			},
		}
	})

	It("succeeds when shoot is being deleted", func() {
		now := metav1.Now()
		shoot.DeletionTimestamp = &now
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cloudProfile).Build()
		v := validator.NewShootValidatorWithClient(c, defaultRequiredCaps)

		Expect(v.Validate(ctx, shoot, nil)).To(Succeed())
	})

	It("succeeds for a workerless shoot", func() {
		shoot.Spec.Provider.Workers = nil
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cloudProfile).Build()
		v := validator.NewShootValidatorWithClient(c, defaultRequiredCaps)

		Expect(v.Validate(ctx, shoot, nil)).To(Succeed())
	})

	It("succeeds when no worker pool uses Kata", func() {
		shoot.Spec.Provider.Workers[0].CRI = &gardencorev1beta1.CRI{
			Name: "containerd",
		}
		shoot.Spec.Provider.Workers[0].Machine.Type = "b1.2"
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cloudProfile).Build()
		v := validator.NewShootValidatorWithClient(c, defaultRequiredCaps)

		Expect(v.Validate(ctx, shoot, nil)).To(Succeed())
	})

	It("succeeds when no required capabilities are configured", func() {
		shoot.Spec.Provider.Workers[0].Machine.Type = "b1.2"
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cloudProfile).Build()
		v := validator.NewShootValidatorWithClient(c, nil)

		Expect(v.Validate(ctx, shoot, nil)).To(Succeed())
	})

	It("succeeds when update has no changes in spec", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cloudProfile).Build()
		v := validator.NewShootValidatorWithClient(c, defaultRequiredCaps)

		oldShoot := shoot.DeepCopy()
		Expect(v.Validate(ctx, shoot, oldShoot)).To(Succeed())
	})

	It("succeeds when machine type has the required capability", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cloudProfile).Build()
		v := validator.NewShootValidatorWithClient(c, defaultRequiredCaps)

		Expect(v.Validate(ctx, shoot, nil)).To(Succeed())
	})

	It("fails when machine type lacks the required capability", func() {
		shoot.Spec.Provider.Workers[0].Machine.Type = "b1.2"
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cloudProfile).Build()
		v := validator.NewShootValidatorWithClient(c, defaultRequiredCaps)

		err := v.Validate(ctx, shoot, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`machine type "b1.2" for worker pool "worker-pool-1" does not have required capability containerRuntime="kata" in CloudProfile "stackit"`))
	})

	It("fails when machine type has a different capability value", func() {
		shoot.Spec.Provider.Workers[0].Machine.Type = "other.flavor"
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cloudProfile).Build()
		v := validator.NewShootValidatorWithClient(c, defaultRequiredCaps)

		err := v.Validate(ctx, shoot, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`machine type "other.flavor" for worker pool "worker-pool-1" does not have required capability containerRuntime="kata" in CloudProfile "stackit"`))
	})

	It("fails when machine type is not found in CloudProfile", func() {
		shoot.Spec.Provider.Workers[0].Machine.Type = "nonexistent.flavor"
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cloudProfile).Build()
		v := validator.NewShootValidatorWithClient(c, defaultRequiredCaps)

		err := v.Validate(ctx, shoot, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`spec.provider.workers[0].machine.type: Not found: "nonexistent.flavor"`))
	})

	It("succeeds with NamespacedCloudProfile reference when machine type has required capability", func() {
		shoot.Spec.CloudProfileName = nil
		shoot.Spec.CloudProfile = &gardencorev1beta1.CloudProfileReference{
			Kind: v1beta1constants.CloudProfileReferenceKindNamespacedCloudProfile,
			Name: "my-profile",
		}

		namespacedProfile := &gardencorev1beta1.NamespacedCloudProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-profile",
				Namespace: "garden-test",
			},
			Status: gardencorev1beta1.NamespacedCloudProfileStatus{
				CloudProfileSpec: cloudProfile.Spec,
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespacedProfile).Build()
		v := validator.NewShootValidatorWithClient(c, defaultRequiredCaps)

		Expect(v.Validate(ctx, shoot, nil)).To(Succeed())
	})

	It("fails with NamespacedCloudProfile when machine type lacks required capability", func() {
		shoot.Spec.CloudProfileName = nil
		shoot.Spec.CloudProfile = &gardencorev1beta1.CloudProfileReference{
			Kind: v1beta1constants.CloudProfileReferenceKindNamespacedCloudProfile,
			Name: "my-profile",
		}
		shoot.Spec.Provider.Workers[0].Machine.Type = "b1.2"

		namespacedProfile := &gardencorev1beta1.NamespacedCloudProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-profile",
				Namespace: "garden-test",
			},
			Status: gardencorev1beta1.NamespacedCloudProfileStatus{
				CloudProfileSpec: cloudProfile.Spec,
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespacedProfile).Build()
		v := validator.NewShootValidatorWithClient(c, defaultRequiredCaps)

		err := v.Validate(ctx, shoot, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`machine type "b1.2" for worker pool "worker-pool-1" does not have required capability containerRuntime="kata"`))
	})

	It("validates only Kata pools when multiple pools are present", func() {
		shoot.Spec.Provider.Workers = append(shoot.Spec.Provider.Workers, gardencorev1beta1.Worker{
			Name: "worker-pool-regular",
			Machine: gardencorev1beta1.Machine{
				Type: "b1.2",
			},
			CRI: &gardencorev1beta1.CRI{
				Name: "containerd",
			},
		})

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cloudProfile).Build()
		v := validator.NewShootValidatorWithClient(c, defaultRequiredCaps)

		// pool 1 is b2i.4d (kata supported), pool 2 is b1.2 (non-kata pool) -> should succeed
		Expect(v.Validate(ctx, shoot, nil)).To(Succeed())
	})

	It("validates multiple required capabilities", func() {
		multipleCaps := make([]validator.RequiredCapability, 0, 3)
		multipleCaps = append(multipleCaps,
			validator.RequiredCapability{Name: "containerRuntime", Value: "kata"},
			validator.RequiredCapability{Name: "architecture", Value: "amd64"},
		)

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cloudProfile).Build()
		v := validator.NewShootValidatorWithClient(c, multipleCaps)

		Expect(v.Validate(ctx, shoot, nil)).To(Succeed())

		// Add an unsatisfied capability requirement
		unsatisfiedCaps := append(multipleCaps, validator.RequiredCapability{
			Name:  "gpuArchitecture",
			Value: "NVIDIA_AMPERE",
		})
		v2 := validator.NewShootValidatorWithClient(c, unsatisfiedCaps)
		err := v2.Validate(ctx, shoot, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`does not have required capability gpuArchitecture="NVIDIA_AMPERE"`))
	})
})
