// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package container_runtime

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	kubernetesclient "github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/utils"
	"github.com/gardener/gardener/test/framework"
	"github.com/onsi/ginkgo/v2"
	g "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-runtime-kata/imagevector"
	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
)

const (
	kataContainerRuntimeName = "kata"
	// kataRuntimeClass is the RuntimeClass exercised by this test (Cloud Hypervisor).
	kataRuntimeClass = "kata-clh"
)

var kataTimeout = 30 * time.Minute

var _ = ginkgo.Describe("kata tests", func() {
	f := framework.NewShootFramework(nil)

	f.Beta().Serial().CIt("should add, remove and re-add a worker pool with kata", func(ctx context.Context) {
		ginkgo.By("test adding new worker pool with containerd and kata")
		shoot := f.Shoot

		if len(shoot.Spec.Provider.Workers) == 0 {
			ginkgo.Skip("at least one worker pool is required in the test shoot.")
		}

		testWorker := shoot.Spec.Provider.Workers[0].DeepCopy()
		machineImage := testWorker.Machine.Image

		cloudProfile, err := f.GetCloudProfile(ctx)
		g.Expect(err).ToNot(g.HaveOccurred())

		if !supportsKata(cloudProfile.Spec.MachineImages, machineImage) {
			ginkgo.Skip(fmt.Sprintf("Skipping test as kata is not supported on OS %q, version: %q, according to cloudprofile %q", machineImage.Name, *machineImage.Version, cloudProfile.GetName()))
		}

		ginkgo.By(fmt.Sprintf("OS %q, version: %q supports the kata container runtime according to cloudprofile %q", machineImage.Name, *machineImage.Version, cloudProfile.GetName()))

		testWorker = configureWorkerForTesting(testWorker, true)

		shoot.Spec.Provider.Workers = append(shoot.Spec.Provider.Workers, *testWorker)

		ginkgo.By("adding kata worker pool")

		defer func(ctx context.Context, workerPoolName string) {
			ginkgo.By("removing kata worker pool after test execution")
			removeWorkerPool(ctx, f, workerPoolName)
		}(ctx, testWorker.Name)

		err = f.UpdateShoot(ctx, func(s *gardencorev1beta1.Shoot) error {
			s.Spec.Provider.Workers = shoot.Spec.Provider.Workers
			return nil
		})
		framework.ExpectNoError(err)

		// get the nodes of the worker pool and check that they carry the expected kata label
		nodeList := getKataNodes(ctx, f, testWorker)

		// deploy root pod
		rootPodExecutor := framework.NewRootPodExecutor(f.Logger, f.ShootClient, &nodeList.Items[0].Name, "kube-system")

		// kata requires containerd, so check that first
		containerdServiceCommand := []string{"systemctl", "is-active", "containerd"}
		executeCommand(ctx, rootPodExecutor, containerdServiceCommand, "active")

		// check that the kata shim binary is available on containerd's PATH
		checkKataShimBinary := []string{"sh", "-c", fmt.Sprintf("[ -e %s/%s ] && echo 'found' || echo 'Not found'", string(extensionsv1alpha1.ContainerDRuntimeContainersBinFolder), "containerd-shim-kata-clh-v2")}
		executeCommand(ctx, rootPodExecutor, checkKataShimBinary, "found")

		// check that the version-stamped kata artifacts are present
		_, kataVersion, err := imagevector.FindInstallationImage()
		g.Expect(err).ToNot(g.HaveOccurred())
		checkKataArtifacts := []string{"sh", "-c", fmt.Sprintf("[ -d %s/%s ] && echo 'found' || echo 'Not found'", kata.InstallationDir, kataVersion)}
		executeCommand(ctx, rootPodExecutor, checkKataArtifacts, "found")

		// check that containerd config.toml is configured for the kata-clh handler
		checkConfigurationCommand := []string{"sh", "-c", "grep -q 'runtimes.kata-clh' /etc/containerd/config.toml && echo 'found' || echo 'Not found'"}
		executeCommand(ctx, rootPodExecutor, checkConfigurationCommand, "found")

		// capture the host kernel release for the microVM comparison below
		hostKernel := strings.TrimSpace(string(execute(ctx, rootPodExecutor, []string{"uname", "-r"})))

		// deploy a pod using the kata RuntimeClass
		kataPod, err := deployKataPod(ctx, f.ShootClient.Client())
		g.Expect(err).ToNot(g.HaveOccurred())

		defer func(ctx context.Context, pod *corev1.Pod) {
			ginkgo.By("removing kata pod after test execution")
			err := f.ShootClient.Client().Delete(ctx, pod)
			g.Expect(err).ToNot(g.HaveOccurred())
		}(ctx, kataPod)

		// wait for it to run - implicitly checks that the pod has been scheduled to a kata node (would not start otherwise)
		err = framework.WaitUntilPodIsRunning(ctx, f.Logger, kataPod.Name, kataPod.Namespace, f.ShootClient)
		g.Expect(err).ToNot(g.HaveOccurred())

		// verify the pod runs inside a microVM: its guest kernel differs from the host node's kernel
		stdout, _, err := kubernetesclient.NewPodExecutor(f.ShootClient.RESTConfig()).Execute(ctx, kataPod.Namespace, kataPod.Name, kataPod.Spec.Containers[0].Name, "uname", "-r")
		g.Expect(err).ToNot(g.HaveOccurred())
		response, err := io.ReadAll(stdout)
		g.Expect(err).ToNot(g.HaveOccurred())
		guestKernel := strings.TrimSpace(string(response))
		g.Expect(guestKernel).ToNot(g.BeEmpty())
		g.Expect(guestKernel).ToNot(g.Equal(hostKernel), "guest (kata) kernel must differ from the host kernel")

		ginkgo.By("test removal of kata from worker pool")
		// remove kata from the worker pool and wait for the Shoot to be successfully reconciled.
		removeKataFromWorker(ctx, f, testWorker.Name)

		ginkgo.By("test re-adding kata to the containerd pool")
		addKataToWorker(ctx, f, testWorker.Name)
	}, kataTimeout)

})

func getKataNodes(ctx context.Context, f *framework.ShootFramework, worker *gardencorev1beta1.Worker) *corev1.NodeList {
	return getNodeListWithLabel(ctx, f, worker, fmt.Sprintf(extensionsv1alpha1.ContainerRuntimeNameWorkerLabel, kataContainerRuntimeName), "true")
}

func getNodeListWithLabel(ctx context.Context, f *framework.ShootFramework, worker *gardencorev1beta1.Worker, nodeLabelKey, nodeLabelValue string) *corev1.NodeList {
	nodeList, err := framework.GetAllNodesInWorkerPool(ctx, f.ShootClient, &worker.Name)
	framework.ExpectNoError(err)
	g.Expect(nodeList.Items).To(g.HaveLen(int(worker.Minimum)))

	for _, node := range nodeList.Items {
		value, found := node.Labels[nodeLabelKey]
		g.Expect(found).To(g.BeTrue())
		g.Expect(value).To(g.Equal(nodeLabelValue))
	}
	return nodeList
}

// configureWorkerForTesting configures the worker pool with test specific configuration such as a unique name and the CRI settings
func configureWorkerForTesting(worker *gardencorev1beta1.Worker, useKata bool) *gardencorev1beta1.Worker {
	allowedCharacters := "0123456789abcdefghijklmnopqrstuvwxyz"
	id, err := utils.GenerateRandomStringFromCharset(3, allowedCharacters)
	framework.ExpectNoError(err)

	worker.Name = fmt.Sprintf("test-%s", id)
	worker.Maximum = 1
	worker.Minimum = 1
	worker.CRI = &gardencorev1beta1.CRI{
		Name: gardencorev1beta1.CRINameContainerD,
	}

	if useKata {
		addKata(worker)
	}
	return worker
}

func addKata(worker *gardencorev1beta1.Worker) {
	worker.CRI.ContainerRuntimes = []gardencorev1beta1.ContainerRuntime{
		{
			Type: kataContainerRuntimeName,
		},
	}
}

func removeKataFromWorker(ctx context.Context, f *framework.ShootFramework, workerPoolName string) {
	err := f.UpdateShoot(ctx, func(s *gardencorev1beta1.Shoot) error {
		workers := make([]gardencorev1beta1.Worker, 0, len(s.Spec.Provider.Workers))
		for _, worker := range s.Spec.Provider.Workers {
			if worker.Name == workerPoolName {
				worker.CRI.ContainerRuntimes = []gardencorev1beta1.ContainerRuntime{}
			}
			workers = append(workers, worker)
		}
		s.Spec.Provider.Workers = workers
		return nil
	})
	framework.ExpectNoError(err)
}

func removeWorkerPool(ctx context.Context, f *framework.ShootFramework, workerPoolName string) {
	err := f.UpdateShoot(ctx, func(s *gardencorev1beta1.Shoot) error {
		workers := make([]gardencorev1beta1.Worker, 0, len(s.Spec.Provider.Workers))
		for _, worker := range s.Spec.Provider.Workers {
			if worker.Name == workerPoolName {
				continue
			}
			workers = append(workers, worker)
		}
		s.Spec.Provider.Workers = workers
		return nil
	})
	framework.ExpectNoError(err)
}

func addKataToWorker(ctx context.Context, f *framework.ShootFramework, workerPoolName string) {
	err := f.UpdateShoot(ctx, func(s *gardencorev1beta1.Shoot) error {
		workers := make([]gardencorev1beta1.Worker, 0, len(s.Spec.Provider.Workers))
		for _, worker := range s.Spec.Provider.Workers {
			if worker.Name == workerPoolName {
				worker.CRI.ContainerRuntimes = []gardencorev1beta1.ContainerRuntime{
					{
						Type: kataContainerRuntimeName,
					},
				}
			}
			workers = append(workers, worker)
		}
		s.Spec.Provider.Workers = workers
		return nil
	})
	framework.ExpectNoError(err)
}

// deployKataPod deploys a pod using the kata RuntimeClass.
func deployKataPod(ctx context.Context, c client.Client) (*corev1.Pod, error) {
	runtimeClass := kataRuntimeClass
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "kata",
			Namespace:    "default",
		},
		Spec: corev1.PodSpec{
			RuntimeClassName: &runtimeClass,
			Containers: []corev1.Container{
				{
					Name:  "kata-container",
					Image: "europe-docker.pkg.dev/gardener-project/releases/3rd/busybox:1.29.3",
					Command: []string{
						"sleep",
						"10000000",
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: new(false),
					},
				},
			},
		},
	}
	if err := c.Create(ctx, &pod); err != nil {
		return nil, err
	}
	return &pod, nil
}

// execute runs a command on the host and returns the raw response.
func execute(ctx context.Context, rootPodExecutor framework.RootPodExecutor, command []string) []byte {
	response, err := rootPodExecutor.Execute(ctx, command...)
	framework.ExpectNoError(err)
	g.Expect(response).ToNot(g.BeNil())
	return response
}

// executeCommand executes a command on the host and checks the returned result
func executeCommand(ctx context.Context, rootPodExecutor framework.RootPodExecutor, command []string, expected string) {
	response := execute(ctx, rootPodExecutor, command)
	g.Expect(string(response)).To(g.Equal(fmt.Sprintf("%s\n", expected)))
}

// supportsKata checks whether the given workerImage supports kata as container runtime
func supportsKata(cloudProfileImages []gardencorev1beta1.MachineImage, workerImage *gardencorev1beta1.ShootMachineImage) bool {
	var (
		cloudProfileImage *gardencorev1beta1.MachineImage
		machineVersion    *gardencorev1beta1.MachineImageVersion
	)

	for _, current := range cloudProfileImages {
		if current.Name == workerImage.Name {
			cloudProfileImage = &current
			break
		}
	}

	if cloudProfileImage == nil {
		return false
	}

	for _, version := range cloudProfileImage.Versions {
		if version.Version == *workerImage.Version {
			machineVersion = &version
			break
		}
	}

	if machineVersion == nil {
		return false
	}

	for _, cri := range machineVersion.CRI {
		if cri.Name != gardencorev1beta1.CRINameContainerD {
			continue
		}

		for _, runtime := range cri.ContainerRuntimes {
			if runtime.Type == kata.Type {
				return true
			}
		}
	}

	return false
}
