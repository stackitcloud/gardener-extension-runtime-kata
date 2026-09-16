package controlplane

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
)

const (
	namespace = "shoot--foo--bar"
	poolName  = "pool-a"
)

func makeShoot(runtimes ...string) *gardencorev1beta1.Shoot {
	worker := gardencorev1beta1.Worker{Name: poolName}
	if len(runtimes) > 0 {
		worker.CRI = &gardencorev1beta1.CRI{Name: gardencorev1beta1.CRINameContainerD}
		for _, t := range runtimes {
			worker.CRI.ContainerRuntimes = append(worker.CRI.ContainerRuntimes, gardencorev1beta1.ContainerRuntime{Type: t})
		}
	}
	return &gardencorev1beta1.Shoot{
		TypeMeta: metav1.TypeMeta{APIVersion: "core.gardener.cloud/v1beta1", Kind: "Shoot"},
		Spec: gardencorev1beta1.ShootSpec{
			Provider: gardencorev1beta1.Provider{Workers: []gardencorev1beta1.Worker{worker}},
		},
	}
}

func makeCluster(shoot *gardencorev1beta1.Shoot) *extensionsv1alpha1.Cluster {
	raw, err := json.Marshal(shoot)
	Expect(err).NotTo(HaveOccurred())
	return &extensionsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
		Spec: extensionsv1alpha1.ClusterSpec{
			Shoot: runtime.RawExtension{Raw: raw},
		},
	}
}

func makeOSC(reconcile bool) *extensionsv1alpha1.OperatingSystemConfig {
	purpose := extensionsv1alpha1.OperatingSystemConfigPurposeProvision
	if reconcile {
		purpose = extensionsv1alpha1.OperatingSystemConfigPurposeReconcile
	}

	osc := &extensionsv1alpha1.OperatingSystemConfig{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "osc",
			Labels:    map[string]string{v1beta1constants.LabelWorkerPool: poolName},
		},
		Spec: extensionsv1alpha1.OperatingSystemConfigSpec{
			Purpose:   purpose,
			CRIConfig: &extensionsv1alpha1.CRIConfig{Name: extensionsv1alpha1.CRINameContainerD},
		},
	}
	if reconcile {
		osc.Spec.CRIConfig.Containerd = &extensionsv1alpha1.ContainerdConfig{}
	}
	return osc
}

func handlerNames(criConfig *extensionsv1alpha1.CRIConfig) []string {
	names := make([]string, 0, len(criConfig.Containerd.Plugins))
	for _, plugin := range criConfig.Containerd.Plugins {
		names = append(names, plugin.Path[len(plugin.Path)-1])
	}
	return names
}

func fileByPath(files []extensionsv1alpha1.File, path string) *extensionsv1alpha1.File {
	for i := range files {
		if files[i].Path == path {
			return &files[i]
		}
	}
	return nil
}

var _ = Describe("Mutator", func() {
	var (
		ctx = context.Background()
		m   *mutator
	)

	newMutatorWith := func(cluster *extensionsv1alpha1.Cluster) {
		builder := fake.NewClientBuilder().WithScheme(kubernetes.SeedScheme)
		if cluster != nil {
			builder = builder.WithObjects(cluster)
		}
		m = &mutator{client: builder.Build(), logger: logr.Discard()}
	}

	Describe("#Mutate", func() {
		It("installs and configures kata when the worker pool uses kata", func() {
			newMutatorWith(makeCluster(makeShoot(kata.Type)))
			osc := makeOSC(true)

			Expect(m.Mutate(ctx, osc, nil)).To(Succeed())

			By("adding the containerd runtime handlers")
			Expect(handlerNames(osc.Spec.CRIConfig)).To(ConsistOf("kata-qemu", "kata-clh"))

			By("delivering the tarball via imageRef")
			tarball := fileByPath(osc.Spec.Files, tarballPath())
			Expect(tarball).NotTo(BeNil())
			Expect(tarball.Content.ImageRef).NotTo(BeNil())
			Expect(tarball.Content.ImageRef.FilePathInImage).To(Equal(tarballPathInImage))
			Expect(tarball.Content.ImageRef.Image).NotTo(BeEmpty())
			Expect(tarball.Content.ImageRef.Image).NotTo(ContainSubstring("$Format:"))
			Expect(tarball.Content.ImageRef.Image).NotTo(ContainSubstring("$"))
			Expect(tarball.Content.ImageRef.Image).To(ContainSubstring(kata.RuntimeKataInstallationImageName))

			By("delivering the install script inline")
			script := fileByPath(osc.Spec.Files, installScriptPath)
			Expect(script).NotTo(BeNil())
			Expect(script.Content.Inline).NotTo(BeNil())
			Expect(script.Content.Inline.Data).To(ContainSubstring(kata.PackageVersion))

			By("adding the install unit with change-triggering FilePaths")
			Expect(osc.Spec.Units).To(HaveLen(1))
			Expect(osc.Spec.Units[0].Name).To(Equal(installUnitName))
			Expect(osc.Spec.Units[0].FilePaths).To(ConsistOf(tarballPath(), installScriptPath))
		})

		It("does not mutate when the worker pool does not use kata", func() {
			newMutatorWith(makeCluster(makeShoot()))
			osc := makeOSC(true)

			Expect(m.Mutate(ctx, osc, nil)).To(Succeed())
			Expect(osc.Spec.CRIConfig.Containerd.Plugins).To(BeEmpty())
			Expect(osc.Spec.Files).To(BeEmpty())
			Expect(osc.Spec.Units).To(BeEmpty())
		})

		It("does not mutate the provision OSC (no containerd config)", func() {
			newMutatorWith(makeCluster(makeShoot(kata.Type)))
			osc := makeOSC(false)

			Expect(m.Mutate(ctx, osc, nil)).To(Succeed())
			Expect(osc.Spec.CRIConfig.Containerd).To(BeNil())
			Expect(osc.Spec.Files).To(BeEmpty())
			Expect(osc.Spec.Units).To(BeEmpty())
		})

		It("does not mutate an OSC without a worker pool label", func() {
			newMutatorWith(makeCluster(makeShoot(kata.Type)))
			osc := makeOSC(true)
			osc.Labels = nil

			Expect(m.Mutate(ctx, osc, nil)).To(Succeed())
			Expect(osc.Spec.CRIConfig.Containerd.Plugins).To(BeEmpty())
		})

		It("is idempotent", func() {
			newMutatorWith(makeCluster(makeShoot(kata.Type)))
			osc := makeOSC(true)

			Expect(m.Mutate(ctx, osc, nil)).To(Succeed())
			Expect(m.Mutate(ctx, osc, nil)).To(Succeed())
			Expect(handlerNames(osc.Spec.CRIConfig)).To(ConsistOf("kata-qemu", "kata-clh"))
			Expect(osc.Spec.Files).To(HaveLen(2))
			Expect(osc.Spec.Units).To(HaveLen(1))
		})
	})
})

// Use Ordered to ensure that script content is checked first
var _ = Describe("Install Script", Ordered, func() {
	It("should correctly include path prefix", func() {
		script := installScript("/tmp/folder")
		Expect(script).To(Equal(`#!/bin/sh
set -eu

VERSION="` + kata.PackageVersion + `"
INSTALL_ROOT=/tmp/folder"/opt/kata"
INSTALL_DIR="${INSTALL_ROOT}/${VERSION}"
ARTIFACT=/tmp/folder"/opt/kata/downloads/kata-static-` + kata.PackageVersion + `.tar.gz"

if [ ! -f "${INSTALL_DIR}/.installed" ]; then
  echo "Installing Kata ${VERSION} into ${INSTALL_DIR} ..."
  staging="${INSTALL_DIR}.staging"
  rm -rf "${staging}" "${INSTALL_DIR}"
  mkdir -p "${staging}"
  # kata-static packs everything under ./opt/kata/{bin,share,...}; drop prefix
  tar -xzf "${ARTIFACT}" -C "${staging}" --strip-components=2
  # Point configs to versioned dir
  for cfg in "${staging}"/share/defaults/kata-containers/runtime-rs/configuration-*.toml "${staging}"/bin/qemu-system-x86_64-wrapper; do
    sed -i "s#/opt/kata/#${INSTALL_DIR}/#g" "${cfg}"
  done
  touch "${staging}/.installed"
  # atomic publish
  mv "${staging}" "${INSTALL_DIR}"
fi

# Garbage-collect old installs: prune everything inside version directories except runtime-rs.
# Removing the shim binary would result in a pod sandbox restart
for d in $(ls -1dt "${INSTALL_ROOT}"/*/ 2>/dev/null); do
  name=$(basename "${d}")
  [ "${name}" = "${VERSION}" ] && continue
  [ -f "${d%/}/.installed" ] || continue
  echo "Pruning old Kata version ${name}"

  for item in "${d}"*; do
    [ -e "${item}" ] || continue
    item_name=$(basename "${item}")
    [ "${item_name}" = "runtime-rs" ] && continue
    rm -rf "${item}"
  done
done

# Verify that host fulfills requirements to run kata workloads for QEMU and cloud-hypervisor
"${INSTALL_DIR}"/bin/kata-runtime --config "${INSTALL_DIR}"/share/defaults/kata-containers/runtime-rs/configuration-qemu-runtime-rs.toml check
"${INSTALL_DIR}"/bin/kata-runtime --config "${INSTALL_DIR}"/share/defaults/kata-containers/runtime-rs/configuration-clh-runtime-rs.toml check

echo "Kata ${VERSION} installed."
`))
	})

	It("creates archive, runs installer script, and verifies installation", func() {
		tempDir := GinkgoT().TempDir()
		downloadsDir := filepath.Join(tempDir, "opt", "kata", "downloads")
		Expect(os.MkdirAll(downloadsDir, 0750)).To(Succeed())
		tarPath := filepath.Join(downloadsDir, "kata-static-"+kata.PackageVersion+".tar.gz")
		createMinimalTarGz(tarPath)

		// Set up older versions to verify garbage collection prunes all older versions
		oldVersion1 := filepath.Join(tempDir, "opt", "kata", "1.0.0", ".installed")
		oldVersion2 := filepath.Join(tempDir, "opt", "kata", "2.0.0", ".installed")
		Expect(os.MkdirAll(filepath.Dir(oldVersion1), 0750)).To(Succeed())
		Expect(os.MkdirAll(filepath.Dir(oldVersion2), 0750)).To(Succeed())
		Expect(os.WriteFile(oldVersion1, []byte(""), 0600)).To(Succeed())
		Expect(os.WriteFile(oldVersion2, []byte(""), 0600)).To(Succeed())

		// Add fake shim
		stubShim := filepath.Join(tempDir, "opt", "kata", "2.0.0", "runtime-rs", "bin", "some-shim")
		Expect(os.MkdirAll(filepath.Dir(stubShim), 0750)).To(Succeed())
		// #nosec G306 file needs to be executable
		Expect(os.WriteFile(stubShim, []byte(""), 0700)).To(Succeed())

		scriptPath := filepath.Join(tempDir, "install.sh")
		script := installScript(tempDir)
		// #nosec G306 file needs to be executable
		Expect(os.WriteFile(scriptPath, []byte(script), 0750)).To(Succeed())
		// #nosec G204 script path is fully controlled by test case
		cmd := exec.Command("bash", scriptPath)
		output, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "Script failed with output: %s", string(output))
		installedFile := filepath.Join(tempDir, "opt", "kata", kata.PackageVersion, ".installed")
		Expect(installedFile).To(BeAnExistingFile())

		// Verify that all older versions were pruned. share contains the majority of the data
		Expect(oldVersion1).To(BeAnExistingFile())
		Expect(oldVersion2).To(BeAnExistingFile())
		Expect(filepath.Join(filepath.Dir(oldVersion1), "share")).ToNot(BeAnExistingFile())
		Expect(filepath.Join(filepath.Dir(oldVersion2), "share")).ToNot(BeAnExistingFile())
		// shim must still exist
		Expect(stubShim).To(BeARegularFile())

		configFile := filepath.Join(tempDir, "opt", "kata", kata.PackageVersion, "share", "defaults", "kata-containers", "runtime-rs", "configuration-example.toml")
		// #nosec G304 path is fully controlled by test case
		content, err := os.ReadFile(configFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("[hypervisor.qemu]\npath = \"" + tempDir + "/opt/kata/" + kata.PackageVersion + "/bin/qemu-system-x86_64\"\n"))

		wrapperFile := filepath.Join(tempDir, "opt", "kata", kata.PackageVersion, "bin", "qemu-system-x86_64-wrapper")
		// #nosec G304 path is fully controlled by test case
		content, err = os.ReadFile(wrapperFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal(`#!/bin/sh

# inject correct firmware path
exec ` + tempDir + "/opt/kata/" + kata.PackageVersion + `/bin/qemu-system-x86_64 "$@" -L ` + tempDir + "/opt/kata/" + kata.PackageVersion + `/share/kata-qemu/qemu/
`))

		By("rewriting the paths in the qemu and clh runtime-rs configs")
		hypervisors := []struct {
			name   string
			binary string
		}{
			{name: "qemu", binary: "qemu-system-x86_64"},
			{name: "clh", binary: "cloud-hypervisor"},
		}

		configFiles := make(map[string]string, len(hypervisors))
		for _, hv := range hypervisors {
			hvConfigFile := filepath.Join(tempDir, "opt", "kata", kata.PackageVersion, "share", "defaults", "kata-containers", "runtime-rs", "configuration-"+hv.name+"-runtime-rs.toml")
			configFiles[hv.name] = hvConfigFile
			// #nosec G304 path is fully controlled by test case
			hvContent, err := os.ReadFile(hvConfigFile)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(hvContent)).To(Equal("[hypervisor." + hv.name + "]\npath = \"" + tempDir + "/opt/kata/" + kata.PackageVersion + "/bin/" + hv.binary + "\"\n"))
		}

		By("invoking kata-runtime check against both hypervisor configs")
		invocationLog := filepath.Join(tempDir, "opt", "kata", kata.PackageVersion, "kata-runtime-invocations.log")
		// #nosec G304 path is fully controlled by test case
		logContent, err := os.ReadFile(invocationLog)
		Expect(err).ToNot(HaveOccurred(), "expected kata-runtime to have been invoked")
		for _, hv := range hypervisors {
			Expect(string(logContent)).To(ContainSubstring("--config " + configFiles[hv.name] + " check"))
		}
	})
})

// Helper function to create a valid minimal .tar.gz file
func createMinimalTarGz(targetPath string) {
	// #nosec G304 path is fully controlled by test case
	file, err := os.Create(targetPath)
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		Expect(file.Close()).To(Succeed())
	}()

	gw := gzip.NewWriter(file)
	defer func() {
		Expect(gw.Close()).To(Succeed())
	}()

	tw := tar.NewWriter(gw)
	defer func() {
		Expect(tw.Close()).To(Succeed())
	}()

	addFile := func(name string, mode int64, content []byte) {
		hdr := &tar.Header{
			Name: name,
			Mode: mode,
			Size: int64(len(content)),
		}
		Expect(tw.WriteHeader(hdr)).To(Succeed())
		_, err := tw.Write(content)
		Expect(err).NotTo(HaveOccurred())
	}

	addFile(
		"opt/kata/share/defaults/kata-containers/runtime-rs/configuration-example.toml",
		0644,
		[]byte("[hypervisor.qemu]\npath = \"/opt/kata/bin/qemu-system-x86_64\"\n"),
	)

	// Config files referenced by the install script's final
	// `kata-runtime --config ... check` invocations.
	addFile(
		"opt/kata/share/defaults/kata-containers/runtime-rs/configuration-qemu-runtime-rs.toml",
		0644,
		[]byte("[hypervisor.qemu]\npath = \"/opt/kata/bin/qemu-system-x86_64\"\n"),
	)
	addFile(
		"opt/kata/share/defaults/kata-containers/runtime-rs/configuration-clh-runtime-rs.toml",
		0644,
		[]byte("[hypervisor.clh]\npath = \"/opt/kata/bin/cloud-hypervisor\"\n"),
	)

	addFile(
		"opt/kata/bin/qemu-system-x86_64-wrapper",
		0644,
		[]byte(`#!/bin/sh

# inject correct firmware path
exec /opt/kata/bin/qemu-system-x86_64 "$@" -L /opt/kata/share/kata-qemu/qemu/
`),
	)

	// Stub kata-runtime binary: records each invocation to a log file next to
	// the install dir so the test can verify that the install script's
	// post-install "check" calls actually ran, and ran against the expected
	// config files.
	addFile(
		"opt/kata/bin/kata-runtime",
		0755,
		[]byte(`#!/bin/sh
dir=$(dirname "$0")
parent=$(dirname "$dir")
echo "$@" >> "${parent}/kata-runtime-invocations.log"
`),
	)
}
