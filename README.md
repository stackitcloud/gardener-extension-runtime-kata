# Gardener Extension for the Kata Containers Runtime

This extension implements [Gardener](https://gardener.cloud)'s extension contract for the `kata`
`ContainerRuntime`, installing and configuring [Kata Containers](https://katacontainers.io/) so
that pods can run inside lightweight microVMs with kernel-level isolation.

## How it works

Kata Containers runs each pod (or container) inside a lightweight virtual machine, giving it its own
guest kernel. This provides a stronger isolation boundary than containers alone, which makes it a good
fit for untrusted or privileged workloads (including Docker-in-Docker).

Enabling Kata on a worker pool is opt-in and driven entirely by the standard Gardener
`ContainerRuntime` mechanism:

```yaml
spec:
  provider:
    workers:
      - name: worker-kata
        # A nested-virtualization-capable machine type is required.
        # A user-defined taint on this pool is strongly recommended.
        cri:
          name: containerd
          containerRuntimes:
            - type: kata
```

When a worker pool declares `cri.containerRuntimes[].type: kata`, Gardener:

1. creates a `ContainerRuntime` resource of type `kata` for the pool on the seed, driving this
   extension's actuator, and
2. labels every node of the pool `containerruntime.worker.gardener.cloud/kata=true`.

That label is the whole targeting mechanism. The node-local integration is delivered **entirely
through the `OperatingSystemConfig` (OSC)** by a seed-side mutating webhook instead of a DaemonSet.
The webhook reads the OSC's worker-pool label and, only for pools that requested the `kata` runtime,
adds three things to the reconcile OSC (gardener-node-agent then applies them):

- **containerd runtime handlers.** The `kata-qemu` and `kata-clh` handlers are added to the
  *structured* containerd configuration (`CRIConfig.Containerd.Plugins`). node-agent renders these
  into `/etc/containerd/config.toml`. The configuration points to the versioned kata installation
  directory to ensure an atomic switch on kata upgrades.

- **The Kata payload, via an image `imageRef` file.** The `kata-static` tarball (containerd shim,
  QEMU + Cloud Hypervisor, guest kernel, rootfs, default configuration) is delivered as an OSC file
  whose content is an `imageRef` into a small, data-only installation image. node-agent pulls the
  image via the node's containerd and streams the tarball to disk.

- **A oneshot systemd install unit.** `kata-installation.service` unpacks the tarball to a
  version-stamped path `/opt/kata/<version>/` and garbage-collects old verisons. The unit's `filePaths`
  reference the tarball and the script, so node-agent re-runs the (idempotent) install whenever the
  Kata version changes.

Being fully OSC-driven means the integration is re-applied automatically on every fresh node.

The extension also creates the `kata-qemu` and `kata-clh` `RuntimeClass`es in the shoot via a
`ManagedResource`. Pods opt into a hypervisor via `runtimeClassName`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: kata-example
spec:
  runtimeClassName: kata-clh   # or kata-qemu
  containers:
    - name: main
      image: alpine:latest
      command: ["sleep", "infinity"]
```

The `RuntimeClass` `nodeSelector` pins such pods to nodes of kata-enabled pools. Because
nested-virtualization pools usually carry a user-defined taint, pods must tolerate that taint
as well. The he taint key is not known to the extension, so no tolerations are injected into the
`RuntimeClass`.

### Upgrades

Upgrades follow two distinct, decoupled release lifecycles:

- **Extension upgrades (`vX.Y.Z`)**: Controller bugfixes, feature additions, or chart improvements.
  When the Kata version (`KATA_VERSION` and `KATA_PACKAGE_RELEASE`) is unchanged, the installation image
  reference remains identical. Deployed Shoot worker nodes require no new downloads or reconciliations.
  Releases can skip rebuilding the installation image with `SKIP_INSTALLATION_IMAGE_BUILD=true`.
- **Kata upgrades (`<kata-version>-<package-release>`)**: Upstream Kata releases or packaging changes
  update `KATA_VERSION` and/or `KATA_PACKAGE_RELEASE` in the `KATA_VERSION` file. Running `make installation-image PUSH=true`
  builds and publishes the new installation image.

Kata is installed to a version-stamped path and its runtime handlers reference a version-stamped
`ConfigPath`, so a Kata version bump installs into a fresh `/opt/kata/<newversion>/` directory and
never overwrites the binaries or configuration of a version still in use by running workloads. The
controller resolves the Kata version dynamically from the configured installation image (via imagevector
and `imageVectorOverwrite`), decoupling it from controller build-time flags.

Upon deployment of a new kata version, the binaries of the old version get garbage-collected.
A kata upgrade requires a containerd restart to load the configuration with the updated runtime
paths. During the restart, containerd reconnects to running shims and checks that the
`shim-binary-path` that was used to create a pod still exists. If it is missing, the container
is cleaned up, resulting in a pod restart. To avoid this, the shim binary itself is excluded from
garbage-collection.

## Current features

- RuntimeClasses: `kata-qemu` and `kata-clh`. No Firecracker, no GPU, no Confidential Computing.
- Tested node OS: Flatcar and Ubuntu.
- Architecture: initially amd64 only.

## How to develop or use this extension locally

See the Gardener extension documentation:

- [Extensions overview](https://github.com/gardener/gardener/blob/master/docs/extensions/overview.md)
- [OperatingSystemConfig resource / CRI support](https://github.com/gardener/gardener/blob/master/docs/extensions/resources/operatingsystemconfig.md)
- [ContainerRuntime resource](https://github.com/gardener/gardener/blob/master/docs/extensions/resources/containerruntime.md)

Common workflows:

```sh
make generate          # regenerate code (DeepCopy, conversion, controller-registration, docs)
make test              # run unit tests
make verify-extended   # check-generate + check + check-format + test (what CI runs)
make install-binaries  # download the kata-static tarball into the installation image's kodata dir
make installation-image PUSH=true  # build (ko) + push only the installation image (Kata upgrade flow)
make artifacts PUSH=true  # build (ko) + push controller image, installation image, and Helm chart (OCI)
make artifacts SKIP_INSTALLATION_IMAGE_BUILD=true PUSH=true  # release extension reusing existing installation image
```

### Skaffold setup

The extension can be developed using Skaffold. Target a cluster running `gardener-operator`
and use it as follows:

```bash
# install extension if necessary
kubectl apply -f example/extension.yaml
make extension-up SKAFFOLD_DEFAULT_REPO=<your custom registry>
# [...]
make extension-down
```

### Build process

Images are built with **ko**, to trivially build a multi-arch container for the controller. The
installation image, which is built using `make install-binaries` is currently amd64 only.
`make install-binaries` downloads the tarball into `cmd/gardener-extension-runtime-kata-installation/kodata/`,
and ko bundles it at `/var/run/ko/`.

The versioning of the images is decoupled:
- The **controller image** and the **Helm chart** are tagged with the extension release version
  (`git describe`, e.g. `v0.4.0`).
- The **installation image** is tagged with the packaged Kata version and package release
  (`$(KATA_VERSION)-$(KATA_PACKAGE_RELEASE)`, e.g. `4.1.0-1`), as it only contains the Kata payload
  and evolves independently of controller changes.
  When packaging assets (`hack/install-binaries.sh` or `cmd/gardener-extension-runtime-kata-installation/`)
  are modified without bumping `KATA_VERSION`, `KATA_PACKAGE_RELEASE` in `KATA_VERSION` must be incremented
  and synced with `make generate`. This is verified by `make check-package-release` (executed during `make check`
  and `make check-generate`).

The Helm chart is pushed as an OCI artifact by `hack/push-artifacts.sh`, which also injects the
installation-image reference into the chart's `imageVectorOverwrite` so the deployed
controller resolves it (dev and release alike) via `IMAGEVECTOR_OVERWRITE`. When running locally
without an overwrite, the controller resolves the installation image and its version from `imagevector/images.yaml`.

## License & Licensing Compliance

This project is licensed under the [Apache License 2.0](LICENSE).

### Kata Containers & `libseccomp` Licensing

Kata Containers is released under the [Apache License 2.0](https://github.com/kata-containers/kata-containers/blob/main/LICENSE). Some Kata releases statically link against the GNU LGPL-2.1 licensed `libseccomp` library.

To ensure licensing compliance across all distributed releases:
- The Kata Containers `LICENSE` file is bundled inside `kata-static.tar.gz` and extracted to `/opt/kata/<version>/LICENSE` on worker nodes.
- When an upstream Kata release includes a `libseccomp` source tarball (required for LGPL-2.1 static linking compliance), `make install-binaries` automatically downloads and bundles it into `/opt/kata/<version>/src/libseccomp-src.tar.gz` inside `kata-static.tar.gz`. This guarantees that the source code is shipped alongside the binary payload both in the installation container image and on worker nodes.
