package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	gcontext "github.com/gardener/gardener/extensions/pkg/webhook/context"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-runtime-kata/imagevector"
	"github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata"
)

const (
	// installUnitName is the systemd unit that installs the Kata binaries on the node.
	installUnitName = "kata-installation.service"
	// installScriptPath is the on-node path of the install script (delivered inline via the OSC).
	installScriptPath = "/opt/kata/bin/install-kata.sh"
	// downloadDir is where the Kata tarball is delivered by gardener-node-agent (via imageRef).
	downloadDir = "/opt/kata/downloads"
	// tarballPathInImage is the path of the kata-static tarball inside the ko-built installation image.
	tarballPathInImage = "/var/run/ko/kata-static.tar.gz"
)

// kataHandler describes a containerd runtime handler that this extension registers.
type kataHandler struct {
	// name must correspond to the RuntimeClass `handler` value.
	name string
	// configFile is the Kata configuration file that selects the hypervisor for this handler.
	configFile string
}

// kataHandlers is the fixed set of runtime handlers registered by this extension
var kataHandlers = []kataHandler{
	{name: "kata-qemu", configFile: "configuration-qemu-runtime-rs.toml"},
	{name: "kata-clh", configFile: "configuration-clh-runtime-rs.toml"},
}

// NewMutator creates a new OperatingSystemConfig mutator for the Kata Containers runtime.
func NewMutator(mgr manager.Manager, logger logr.Logger) extensionswebhook.Mutator {
	return &mutator{
		client: mgr.GetClient(),
		logger: logger.WithName("kata-osc-mutator"),
	}
}

type mutator struct {
	client client.Client
	logger logr.Logger
}

// Mutate installs and configures the Kata Containers runtime on the nodes of a worker pool that
// requested the `kata` container runtime, entirely through the OperatingSystemConfig (OSC):
//   - it registers the kata-qemu / kata-clh containerd runtime handlers in the structured containerd
//     config (rendered by gardener-node-agent), and
//   - it delivers the Kata payload as an OSC file (pulled from the installation image via imageRef by
//     node-agent) plus a oneshot systemd unit that unpacks it to a version-stamped path.
//
// Everything is gated precisely on the worker pool of the OSC, so nodes that do not use Kata are not
// touched.
func (m *mutator) Mutate(ctx context.Context, newObj, _ client.Object) error {
	osc, ok := newObj.(*extensionsv1alpha1.OperatingSystemConfig)
	if !ok {
		return fmt.Errorf("could not cast object of type %T to OperatingSystemConfig", newObj)
	}

	// Do not mutate objects that are being deleted.
	if osc.GetDeletionTimestamp() != nil {
		return nil
	}

	// Only the 'reconcile' OperatingSystemConfig carries the containerd configuration
	if osc.Spec.Purpose != extensionsv1alpha1.OperatingSystemConfigPurposeReconcile {
		return nil
	} else if osc.Spec.CRIConfig == nil || osc.Spec.CRIConfig.Containerd == nil {
		return nil
	}

	// Precise per-pool gating: only act if the worker pool this OSC belongs to requests the kata
	// container runtime. gardenlet labels the OSC with the worker pool name.
	poolName, ok := osc.Labels[v1beta1constants.LabelWorkerPool]
	if !ok {
		return nil
	}
	cluster, err := gcontext.NewGardenContext(m.client, osc).GetCluster(ctx)
	if err != nil {
		return fmt.Errorf("could not get cluster for OperatingSystemConfig mutation: %w", err)
	}
	if cluster.Shoot == nil || !workerPoolUsesKata(cluster.Shoot, poolName) {
		return nil
	}

	m.logger.Info("Installing and configuring Kata Containers", "workerPool", poolName, "shoot", client.ObjectKeyFromObject(cluster.Shoot))

	if err := ensureCRIConfig(osc.Spec.CRIConfig); err != nil {
		return err
	}
	if err := ensureFiles(&osc.Spec.Files); err != nil {
		return fmt.Errorf("could not ensure files for OperatingSystemConfig mutation: %w", err)
	}
	ensureUnits(&osc.Spec.Units)

	return nil
}

// ensureCRIConfig appends the Kata containerd runtime handlers to the structured containerd config
func ensureCRIConfig(criConfig *extensionsv1alpha1.CRIConfig) error {
	for _, handler := range kataHandlers {
		// node-agent translates the path to work with containerd v1 and v2
		path := []string{"io.containerd.grpc.v1.cri", "containerd", "runtimes", handler.name}

		values, err := handler.values()
		if err != nil {
			return fmt.Errorf("could not marshal containerd runtime handler config for %q: %w", handler.name, err)
		}

		upsertPlugin(&criConfig.Containerd.Plugins, extensionsv1alpha1.PluginConfig{
			Path:   path,
			Values: values,
		})
	}

	return nil
}

// ensureFiles adds the OSC files that deliver the Kata payload: the kata-static tarball (pulled from
// the installation image by node-agent) and the inline install script.
func ensureFiles(files *[]extensionsv1alpha1.File) error {
	// FindImage resolves the fully-qualified installation image reference (repository + tag).
	installerImage, err := imagevector.ImageVector().FindImage(kata.RuntimeKataInstallationImageName)
	if err != nil {
		return err
	}

	desired := []extensionsv1alpha1.File{
		{
			// Version-stamped download path: a Kata upgrade delivers a new tarball at a new path, which
			// changes the install unit's FilePaths and re-triggers the installation.
			Path:        tarballPath(),
			Permissions: ptr.To[uint32](0644),
			Content: extensionsv1alpha1.FileContent{
				ImageRef: &extensionsv1alpha1.FileContentImageRef{
					Image:           installerImage.String(),
					FilePathInImage: tarballPathInImage,
				},
			},
		},
		{
			Path:        installScriptPath,
			Permissions: ptr.To[uint32](0755),
			Content: extensionsv1alpha1.FileContent{
				Inline: &extensionsv1alpha1.FileContentInline{
					Encoding: string(extensionsv1alpha1.PlainFileCodecID),
					Data:     installScript(""),
				},
			},
		},
	}

	for _, file := range desired {
		upsertFile(files, file)
	}

	return nil
}

// ensureUnits adds the oneshot systemd unit that unpacks the Kata payload. Its FilePaths reference the
// tarball and the install script, so node-agent restarts the unit whenever either changes (e.g. a
// Kata version bump), which re-runs the (idempotent) installation.
func ensureUnits(units *[]extensionsv1alpha1.Unit) {
	unit := extensionsv1alpha1.Unit{
		Name:      installUnitName,
		Enable:    new(true),
		Command:   ptr.To(extensionsv1alpha1.CommandStart),
		Content:   new(installUnitContent),
		FilePaths: []string{tarballPath(), installScriptPath},
	}
	upsertUnit(units, unit)
}

const installUnitContent = `[Unit]
Description=Install the Kata Containers runtime
Before=containerd.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=` + installScriptPath + `

[Install]
WantedBy=multi-user.target
`

// tarballPath is the on-node path of the (version-stamped) Kata tarball delivered via imageRef.
func tarballPath() string {
	return fmt.Sprintf("%s/kata-static-%s.tar.gz", downloadDir, kata.PackageVersion)
}

// installScript is the on-node installation script. It unpacks the tarball to the version-stamped
// directory, rewrites Kata's own config paths, and garbage collects old installations. It is idempotent.
func installScript(pathPrefix string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu

VERSION=%[1]q
INSTALL_ROOT=%[4]s%[2]q
INSTALL_DIR="${INSTALL_ROOT}/${VERSION}"
ARTIFACT=%[4]s%[3]q

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
  [ -f "${d%%/}/.installed" ] || continue
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
`, kata.PackageVersion, kata.InstallationDir, tarballPath(), pathPrefix)
}

// workerPoolUsesKata reports whether the named worker pool requests the kata container runtime.
func workerPoolUsesKata(shoot *gardencorev1beta1.Shoot, poolName string) bool {
	for _, worker := range shoot.Spec.Provider.Workers {
		if worker.Name != poolName || worker.CRI == nil {
			continue
		}
		for _, cr := range worker.CRI.ContainerRuntimes {
			if cr.Type == kata.Type {
				return true
			}
		}
	}
	return false
}

// values builds the containerd runtime handler options for this Kata handler.
func (h kataHandler) values() (*apiextensionsv1.JSON, error) {
	config := runtimeHandlerConfig{
		// Both handlers share the canonical single-shim runtime type; they differ only by ConfigPath
		// (the hypervisor selector), so no per-hypervisor shim binaries or symlinks are needed on the node.
		RuntimeType:                  "io.containerd.kata.v2",
		PrivilegedWithoutHostDevices: true,
		// Version-stamped shim path: containerd invokes the shim directly from the installation
		// directory. This allows for atomic switches of the kata version.
		RuntimePath: fmt.Sprintf("%s/%s/runtime-rs/bin/containerd-shim-kata-v2", kata.InstallationDir, kata.PackageVersion),
		// Kata reads sandbox sizing / configuration from these annotations.
		PodAnnotations:       []string{"io.katacontainers.*"},
		ContainerAnnotations: []string{"io.katacontainers.*"},
		Options: runtimeHandlerOptions{
			// Version-stamped path: a Kata upgrade installs to a new /opt/kata/<version>/ directory and
			// gets a fresh ConfigPath, so a running version's configuration is never overwritten in place.
			ConfigPath: fmt.Sprintf("%s/%s/share/defaults/kata-containers/runtime-rs/%s", kata.InstallationDir, kata.PackageVersion, h.configFile),
		},
	}

	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return &apiextensionsv1.JSON{Raw: raw}, nil
}

// runtimeHandlerConfig mirrors the containerd CRI runtime handler schema (TOML keys) that is expected
// under plugins."io.containerd.grpc.v1.cri".containerd.runtimes.<handler>.
type runtimeHandlerConfig struct {
	RuntimeType                  string                `json:"runtime_type"`
	PrivilegedWithoutHostDevices bool                  `json:"privileged_without_host_devices"`
	RuntimePath                  string                `json:"runtime_path"`
	PodAnnotations               []string              `json:"pod_annotations"`
	ContainerAnnotations         []string              `json:"container_annotations"`
	Options                      runtimeHandlerOptions `json:"options"`
}

type runtimeHandlerOptions struct {
	ConfigPath string `json:"ConfigPath"`
}

// upsertFile appends or replaces the file with the matching path.
func upsertFile(files *[]extensionsv1alpha1.File, file extensionsv1alpha1.File) {
	for i, existing := range *files {
		if existing.Path == file.Path {
			(*files)[i] = file
			return
		}
	}
	*files = append(*files, file)
}

// upsertUnit appends or replaces the unit with the matching name.
func upsertUnit(units *[]extensionsv1alpha1.Unit, unit extensionsv1alpha1.Unit) {
	for i, existing := range *units {
		if existing.Name == unit.Name {
			(*units)[i] = unit
			return
		}
	}
	*units = append(*units, unit)
}

// upsertPlugin appends or replaces the plugin with the matching path.
func upsertPlugin(plugins *[]extensionsv1alpha1.PluginConfig, plugin extensionsv1alpha1.PluginConfig) {
	for i, existing := range *plugins {
		if slices.Equal(existing.Path, plugin.Path) {
			(*plugins)[i] = plugin
			return
		}
	}
	*plugins = append(*plugins, plugin)
}
