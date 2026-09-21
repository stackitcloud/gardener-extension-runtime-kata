#!/usr/bin/env sh

# Downloads the Kata Containers `kata-static` release tarball (runtime-rs / Rust runtime + VM
# artifacts) for the requested version, plus the `kata-go-static` release tarball (Go runtime,
# from which we only want the `kata-runtime` CLI — used for `kata-runtime check`), merges the
# latter's kata-runtime binary into the former's tree, recompresses from .tar.zst to .tar.gz, and
# stores the result at ${KATA_ARTIFACTS_DIR}/kata-static.tar.gz.
#
# The tarball is bundled into the data-only installation image by `ko` as kodata (ending up at
# /var/run/ko/kata-static.tar.gz in the image). gardener-node-agent pulls the image and extracts the
# tarball onto the node via an OperatingSystemConfig file imageRef, and a systemd unit unpacks it.
#
# The tarball is recompressed to gzip so that on-node extraction only needs `tar` + `gzip`, which are
# available on both Flatcar and Ubuntu (zstd may not be); zstd is only required here at build time.
# The tarball contains the full Kata payload
# (containerd shim, QEMU + Cloud Hypervisor, guest kernel, rootfs and default configuration).

set -e

KATA_VERSION=$1
KATA_ARTIFACTS_DIR=${KATA_ARTIFACTS_DIR:-cmd/gardener-extension-runtime-kata-installation/kodata}
ARCH=amd64

if [ -z "${KATA_VERSION}" ]; then
    echo "usage: KATA_ARTIFACTS_DIR=<dir> install-binaries.sh <kata-version>" >&2
    exit 1
fi

TARBALL="kata-static-${KATA_VERSION}-${ARCH}.tar.zst"
TARBALL_PATH="/tmp/${TARBALL}"

GO_TARBALL="kata-go-static-${KATA_VERSION}-${ARCH}.tar.zst"
GO_TARBALL_PATH="/tmp/${GO_TARBALL}"

# Check required tools.
for cmd in zstd jq sha256sum curl; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "error: '$cmd' is required but was not found" >&2
        exit 1
    fi
done

if tar --version 2>&1 | grep -q "bsdtar"; then
    echo "error: GNU tar is required" >&2
    exit 1
fi

mkdir -p "${KATA_ARTIFACTS_DIR}"

work="$(mktemp -d)"
work_go="$(mktemp -d)"
# shellcheck disable=SC2064
trap "rm -rf '${work}' '${work_go}'" EXIT

RELEASE_JSON="${work}/release.json"
echo "Fetching release metadata for Kata Containers ${KATA_VERSION}..."
TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
if [ -n "${TOKEN}" ]; then
    if ! curl -fsSL -H "Authorization: Bearer ${TOKEN}" -o "${RELEASE_JSON}" "https://api.github.com/repos/kata-containers/kata-containers/releases/tags/${KATA_VERSION}"; then
        echo "error: failed to fetch release metadata for Kata Containers ${KATA_VERSION}" >&2
        exit 1
    fi
else
    if ! curl -fsSL -o "${RELEASE_JSON}" "https://api.github.com/repos/kata-containers/kata-containers/releases/tags/${KATA_VERSION}"; then
        echo "error: failed to fetch release metadata for Kata Containers ${KATA_VERSION}. If you hit the GitHub API rate limit, set GITHUB_TOKEN or GH_TOKEN." >&2
        exit 1
    fi
fi

# --- Download + verify a release asset against the SHA256 digest published in GitHub release metadata ---
#
fetch_and_verify_asset() {
    # $1=asset filename
    name="$1"
    dest="/tmp/${name}"
    url="https://github.com/kata-containers/kata-containers/releases/download/${KATA_VERSION}/${name}"

    expected_sha=$(jq -r --arg TARBALL "${name}" '.assets[]? | select(.name == $TARBALL) | .digest // empty' "${RELEASE_JSON}" | sed 's/^sha256://')
    if [ -z "${expected_sha}" ]; then
        echo "error: could not find expected SHA256 checksum for ${name} in release metadata" >&2
        exit 1
    fi

    if [ -f "${dest}" ]; then
        echo "Found existing download at ${dest}, verifying checksum..."
        if echo "${expected_sha}  ${dest}" | sha256sum -c >/dev/null 2>&1; then
            echo "Checksum verified for existing ${dest}, skipping download"
            return 0
        fi
        echo "Existing file at ${dest} failed checksum verification, removing..."
        rm -f "${dest}"
    fi

    echo "Downloading ${url}"
    staging="${work}/${name}"
    curl -fSL -o "${staging}" "${url}"
    echo "Verifying SHA256 checksum..."
    echo "${expected_sha}  ${staging}" | sha256sum -c -
    mv "${staging}" "${dest}"
}

fetch_and_verify_asset "${TARBALL}"
fetch_and_verify_asset "${GO_TARBALL}"

echo "Extracting payload from ${TARBALL_PATH}"
zstd -dc "${TARBALL_PATH}" | tar -C "${work}" -xf -
kroot="${work}/opt/kata"

echo "Extracting payload from ${GO_TARBALL_PATH}"
zstd -dc "${GO_TARBALL_PATH}" | tar -C "${work_go}" -xf -
kroot_go="${work_go}/opt/kata"

# --- Merge kata-runtime from kata-go-static into the kata-static (Rust) tree ---
#
# We hard-fail if the destination path already exists, instead
# of silently overwriting Rust-runtime content with Go-runtime content.
GO_RUNTIME_REL="bin/kata-runtime"
go_runtime_src="${kroot_go}/${GO_RUNTIME_REL}"
go_runtime_dst="${kroot}/${GO_RUNTIME_REL}"

if [ ! -e "${go_runtime_src}" ]; then
    echo "error: expected ${GO_RUNTIME_REL} in kata-go-static payload but it was not found (layout change upstream?)" >&2
    exit 1
fi
if [ -e "${go_runtime_dst}" ]; then
    echo "error: refusing to overwrite ${go_runtime_dst} — ${GO_RUNTIME_REL} already exists in the kata-static (Rust) payload" >&2
    exit 1
fi

mkdir -p "$(dirname "${go_runtime_dst}")"
cp -p "${go_runtime_src}" "${go_runtime_dst}"
echo "Merged ${GO_RUNTIME_REL} from kata-go-static"

# kata-runtime is expected to be a statically linked Go binary (no libc/libseccomp deps to carry
# over from the Go tree). Fail loudly if that assumption ever breaks upstream, since a dynamically
# linked binary silently dropped into the Rust payload could fail at runtime on-node in a way
# that's hard to diagnose from the outside.
if command -v file >/dev/null 2>&1; then
    if ! file "${kroot}/bin/kata-runtime" | grep -qi "statically linked"; then
        echo "error: kata-runtime binary from kata-go-static is not statically linked; this build assumes it is self-contained" >&2
        exit 1
    fi
fi

if [ "${KATA_TRIM:-true}" = "true" ]; then
    echo "Trimming unused Kata artifacts (we ship kata-qemu + kata-clh, amd64, non-CC, non-GPU)"

    # Whole families we never use, matched by name anywhere under the tree (binaries, share dirs,
    # guest kernels, rootfs images and configs):
    #   - confidential computing: confidential, coco, tdx, snp, sev, and the CBL-Mariner CC rootfs
    #   - GPU: nvidia, gpu
    #   - alternate/experimental hypervisors: openvmm, dragonball, stratovirt, firecracker (+ jailer)
    #   - anything tagged '-experimental' (e.g. qemu-system-x86_64-{tdx,snp}-experimental)
    #   - debug kernels (huge, e.g. vmlinux-<ver>-debug ~365 MB)
    for pattern in confidential coco tdx snp sev mariner nvidia gpu dragonball stratovirt firecracker jailer experimental debug openvmm; do
        find "${kroot}" -iname "*${pattern}*" -exec rm -rf {} + 2>/dev/null || true
    done

    # EDK2/OVMF firmware for architectures other than the target one. qemu bundles
    # firmware for every arch; drop the ones we don't need.
    for fw in arm aarch64 riscv loongarch s390 ppc64; do
        find "${kroot}" -type f -iname "edk2-${fw}*" -exec rm -f {} + 2>/dev/null || true
    done

    # Keep only the qemu and clh default configuration files.
    find "${kroot}/share/defaults/kata-containers/runtime-rs" -type f -name 'configuration-*.toml' \
        ! -name 'configuration-qemu-runtime-rs.toml' ! -name 'configuration-clh-runtime-rs.toml' -delete 2>/dev/null || true
fi

for i in qemu clh ; do
    fn="${kroot}/share/defaults/kata-containers/runtime-rs/configuration-$i-runtime-rs.toml"
    if [ ! -f "${fn}" ] ; then
        echo "error: missing configuration file ${fn}" >&2
        exit 1
    fi
    # Cleanly communicate sandbox overhead to Kubernetes to avoid pod restarts
    sed -i "s#sandbox_cgroup_only = false#sandbox_cgroup_only = true#g" "${fn}"
done

# --- Force the CLH block storage driver to a value BOTH Kata components accept ---
#
# Kata validates the CLH block device settings with two different accepted-name lists:
# the Go `kata-runtime check` CLI (katautils/config.go) accepts virtio-scsi, virtio-blk,
# virtio-mmio, nvdimm, virtio-blk-ccw, while the Rust runtime-rs shim (kata-types, validated
# on every sandbox creation) accepts virtio-blk-pci, virtio-blk-ccw, virtio-blk-mmio,
# virtio-pmem, virtio-scsi. The upstream default "virtio-blk-pci" fails the CLI check
# during installation; "virtio-blk" passes the CLI but the shim rejects it at sandbox
# creation ("virtio-blk is unsupported block device type"). virtio-scsi is the only value
# both accept (ccw is s390x-only) and is implemented by the runtime-rs CLH backend.
#
# Only block_device_driver needs rewriting: it is the only setting the Go CLI validates,
# and vm_rootfs_driver has no effect on CLH anyway — the runtime-rs CLH backend hardcodes
# the VM rootfs attach to virtio-blk (ch hypervisor: rootfs_driver = VM_ROOTFS_DRIVER_BLK),
# so the upstream default is kept for it.
clh_fn="${kroot}/share/defaults/kata-containers/runtime-rs/configuration-clh-runtime-rs.toml"
sed -i "s#block_device_driver = \"virtio-blk-pci\"#block_device_driver = \"virtio-scsi\"#g" "${clh_fn}"

# Fix kata-qemu runtime class. kata currently does not offer a clean way to point
# qemu to the versioned firmware path. Thus, use a wrapper script instead.
# The installScript will replace the paths with the versioned one.
cat > "${kroot}/bin/qemu-system-x86_64-wrapper" <<'EOF'
#!/bin/sh

# inject correct firmware path
exec /opt/kata/bin/qemu-system-x86_64 "$@" -L /opt/kata/share/kata-qemu/qemu/
EOF
chmod +x "${kroot}/bin/qemu-system-x86_64-wrapper"

sed -i 's#path = "/opt/kata/bin/qemu-system-x86_64"#path = "/opt/kata/bin/qemu-system-x86_64-wrapper"#g' \
    "${kroot}/share/defaults/kata-containers/runtime-rs/configuration-qemu-runtime-rs.toml"

# Ensure LICENSE is included in the payload.
cp "$(dirname "$0")/../LICENSE" "${work}/opt/kata/LICENSE"

# If the Kata Containers release includes a libseccomp source tarball (e.g. LGPL-2.1 compliance for statically linked Kata releases),
# download and bundle it into opt/kata/src/ so it is shipped inside kata-static.tar.gz.
LIBSECCOMP_TARBALL=$(jq -r '.assets[]?.name | select(test("libseccomp-.*\\.tar\\.gz$"))' "${RELEASE_JSON}" 2>/dev/null | head -n 1 || true)

if [ -z "${LIBSECCOMP_TARBALL}" ]; then
    echo "error: No libseccomp source asset found for Kata Containers ${KATA_VERSION}; this is required for LGPL-2.1 compliance" >&2
    exit 1
fi

LIBSECCOMP_URL=$(jq -r --arg TARBALL "${LIBSECCOMP_TARBALL}" '.assets[]? | select(.name == $TARBALL) | .browser_download_url' "${RELEASE_JSON}")
LIBSECCOMP_EXPECTED_SHA=$(jq -r --arg TARBALL "${LIBSECCOMP_TARBALL}" '.assets[]? | select(.name == $TARBALL) | .digest // empty' "${RELEASE_JSON}" | sed 's/^sha256://')
if [ -z "${LIBSECCOMP_EXPECTED_SHA}" ]; then
    echo "error: could not find expected SHA256 checksum for ${LIBSECCOMP_TARBALL} in release metadata" >&2
    exit 1
fi

echo "Found libseccomp source asset at ${LIBSECCOMP_URL}"
mkdir -p "${work}/opt/kata/src"
LIBSECCOMP_SRC="${work}/opt/kata/src/libseccomp-src.tar.gz"
curl -fSL -o "${LIBSECCOMP_SRC}" "${LIBSECCOMP_URL}"

echo "Verifying SHA256 checksum..."
echo "${LIBSECCOMP_EXPECTED_SHA}  ${LIBSECCOMP_SRC}" | sha256sum -c -

echo "Repacking to gzip -> ${KATA_ARTIFACTS_DIR}/kata-static.tar.gz"
# Ensure files are owned by root and pin mtime to ensure deterministic archives
tar --sort=name --owner=root:0 --group=root:0 --mtime='UTC 2019-01-01' \
    -C "${work}" -czf "${KATA_ARTIFACTS_DIR}/kata-static.tar.gz" opt

MAX_SIZE_BYTES=$((250 * 1024 * 1024))
FILE_SIZE=$(wc -c < "${KATA_ARTIFACTS_DIR}/kata-static.tar.gz" | tr -d ' ')
if [ "$FILE_SIZE" -gt "$MAX_SIZE_BYTES" ]; then
    echo "error: ${KATA_ARTIFACTS_DIR}/kata-static.tar.gz ($FILE_SIZE bytes) is larger than 250MB, check if this is expected and adjust the limit if necessary." >&2
    exit 1
fi
