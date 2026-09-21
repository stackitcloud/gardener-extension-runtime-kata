ENSURE_GARDENER_MOD         := $(shell go get github.com/gardener/gardener@$$(go list -m -f "{{.Version}}" github.com/gardener/gardener) github.com/gardener/gardener/hack/tools@$$(go list -m -f "{{.Version}}" github.com/gardener/gardener/hack/tools))
GARDENER_DIR                := $(shell go list -mod=mod -m -f "{{.Dir}}" github.com/gardener/gardener)
GARDENER_HACK_DIR           := $(GARDENER_DIR)/hack

EXTENSION_PREFIX            := gardener-extension
NAME                        := runtime-kata
NAME_INSTALLATION           := runtime-kata-installation
REGISTRY                    ?= ghcr.io
REPOSITORY                  := $(REGISTRY)/stackitcloud/gardener-extension-runtime-kata
IS_DEV                      ?= true
ifeq ($(IS_DEV),true)
REPO_POSTFIX                := -dev
endif
REPO_ROOT                   := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
HACK_DIR                    := $(REPO_ROOT)/hack
VERSION                     := $(shell git describe --tag --always --dirty)
TAG                         := $(VERSION)
GIT_COMMIT                  := $(shell git rev-parse --verify HEAD 2>/dev/null || true)
BUILD_DATE                  := $(shell date '+%Y-%m-%dT%H:%M:%SZ')
LEADER_ELECTION             := false

# The Kata Containers release that is installed on the nodes. This is the single source of truth;
# injected into the controller binary via ldflags (-X .../pkg/kata.Version=...).
# renovate: datasource=github-releases depName=kata-containers/kata-containers
KATA_VERSION                := 4.1.0
# Release counter that can be incremented if it becomes necessary to update the kata configuration
# without also changing the kata version at the same time
KATA_PACKAGE_RELEASE        := 2

LD_FLAGS                    := -w \
	-X github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata.Version=$(KATA_VERSION) \
	-X github.com/stackitcloud/gardener-extension-runtime-kata/pkg/kata.PackageRelease=$(KATA_PACKAGE_RELEASE) \
	-X k8s.io/component-base/version.gitVersion=$(VERSION) \
	-X k8s.io/component-base/version.gitCommit=$(GIT_COMMIT) \
	-X k8s.io/component-base/version.buildDate=$(BUILD_DATE)

# Directory into which the kata-static tarball is downloaded so that `ko` bundles it into the
# installation image as kodata (available at /var/run/ko/ in the image).
INSTALLATION_KODATA_DIR     := $(REPO_ROOT)/cmd/$(EXTENSION_PREFIX)-$(NAME_INSTALLATION)/kodata

SHELL=/usr/bin/env bash -o pipefail

#########################################
# Tools                                 #
#########################################

TOOLS_DIR := $(HACK_DIR)/tools
include $(GARDENER_HACK_DIR)/tools.mk
include $(HACK_DIR)/tools.mk

.PHONY: run
run: ## Starts the application locally
	@LEADER_ELECTION_NAMESPACE=garden GO111MODULE=on go run \
		-ldflags "$(LD_FLAGS)" \
		./cmd/$(EXTENSION_PREFIX)-$(NAME) \
		--kubeconfig=${KUBECONFIG} \
		--leader-election=$(LEADER_ELECTION)

#################################################################
# Rules related to binary build, image build and release        #
#################################################################

PUSH ?= false

# Helper files to properly trigger rebuilds by make. Kept outside kodata to not include them in the container image
KATA_SENTINEL := $(INSTALLATION_KODATA_DIR)/../.kata_installed
VERSION_FILE  := $(INSTALLATION_KODATA_DIR)/../.kata_version

# Force VERSION_FILE to update if the KATA_VERSION variable changes
$(shell mkdir -p $(INSTALLATION_KODATA_DIR)/..; \
	if [ "$$(cat $(VERSION_FILE) 2>/dev/null)" != "$(KATA_VERSION)" ]; then \
		echo "$(KATA_VERSION)" > $(VERSION_FILE); \
	fi)

# Target depends on the script, the version file, and the contents of INSTALLATION_KODATA_DIR
$(KATA_SENTINEL): $(HACK_DIR)/install-binaries.sh $(VERSION_FILE) $(wildcard $(INSTALLATION_KODATA_DIR)/*)
	@mkdir -p $(INSTALLATION_KODATA_DIR)
	@KATA_ARTIFACTS_DIR=$(INSTALLATION_KODATA_DIR) $(HACK_DIR)/install-binaries.sh $(KATA_VERSION)
	@touch $@

.PHONY: install-binaries
install-binaries: $(KATA_SENTINEL) ## Downloads the kata-static tarball into the installation image's kodata directory

images: export KO_DOCKER_REPO = $(REPOSITORY)

.PHONY: images
images: controller-image installation-image ## Builds all images. Use PUSH=true to also push it to a registry

.PHONY: controller-image
controller-image: $(KO) ## Builds the controller image using ko. Use PUSH=true to also push it to a registry
	KO_DOCKER_REPO=$(REPOSITORY)/$(EXTENSION_PREFIX)-$(NAME)$(REPO_POSTFIX) \
	$(KO) build --image-label org.opencontainers.image.source="https://github.com/stackitcloud/gardener-extension-runtime-kata" \
	--sbom none -t $(TAG) --bare \
	--platform linux/amd64,linux/arm64 --push=$(PUSH) \
	--ldflags "$(LD_FLAGS)" \
	./cmd/$(EXTENSION_PREFIX)-$(NAME) \
	| tee controller-images.txt

.PHONY: installation-image
installation-image: $(KO) install-binaries ## Builds the data-only installation image (kata-static tarball as ko kodata)
	# The installation image only carries data (the tarball as kodata at /var/run/ko/); it is never run.
	# It is amd64-only for now, because the kata-static payload is architecture-specific.
	KO_DOCKER_REPO=$(REPOSITORY)/$(EXTENSION_PREFIX)-$(NAME_INSTALLATION)$(REPO_POSTFIX) \
	$(KO) build --image-label org.opencontainers.image.source="https://github.com/stackitcloud/gardener-extension-runtime-kata" \
	--sbom none -t $(TAG) --bare \
	--platform linux/amd64 --push=$(PUSH) \
	./cmd/$(EXTENSION_PREFIX)-$(NAME_INSTALLATION) \
	| tee installation-images.txt

.PHONY: generate-images-json
generate-images-json: images.json ## Generates a JSON file with all images used in the project
images.json: controller-images.txt installation-images.txt
	@jq -n \
		--arg controller "$$(cat controller-images.txt)" \
		--arg installation "$$(cat installation-images.txt)" \
		'{images: {"$(EXTENSION_PREFIX)-$(NAME)": $$controller, "$(EXTENSION_PREFIX)-$(NAME_INSTALLATION)": $$installation}}' > images.json

.PHONY: artifacts-only
artifacts-only: $(YQ) $(HELM) generate-images-json ## Packages and pushes the Helm chart(s)
	PUSH=$(PUSH) $(HACK_DIR)/push-artifacts.sh images.json

.PHONY: artifacts
artifacts: images artifacts-only ## Builds and pushes all artifacts (images + charts)

#####################################################################
# Rules for verification, formatting, linting, testing and cleaning #
#####################################################################

.PHONY: tidy
tidy:
	@GO111MODULE=on go mod tidy

init: tidy ## Run `make init` to perform an initial go mod cache sync which is required for other make targets
# needed so that check-generate.sh can call make revendor
revendor: tidy

.PHONY: clean
clean: ## Cleans the ./cmd and ./pkg packages and build artifacts
	@rm -rf images.json controller-images.txt installation-images.txt artifacts $(INSTALLATION_KODATA_DIR)/*.tar.gz
	@bash $(GARDENER_HACK_DIR)/clean.sh ./cmd/... ./pkg/...

.PHONY: check-generate
check-generate: ## Check if generate target has been run
	@bash $(GARDENER_HACK_DIR)/check-generate.sh $(REPO_ROOT)

.PHONY: check
check: $(GOIMPORTS) $(GOLANGCI_LINT) $(HELM) ## Runs golangci-lint, gofmt/goimports and checks the chart for validity
	@bash $(GARDENER_HACK_DIR)/check.sh --golangci-lint-config=./.golangci.yaml ./cmd/... ./pkg/... ./imagevector/... ./test...
	@bash $(GARDENER_HACK_DIR)/check-charts.sh ./charts

.PHONY: generate
generate: $(CONTROLLER_GEN) $(CRD_REF_DOCS) $(HELM) $(YQ) $(GOIMPORTS) ## Generates code, the controller-registration and the API reference docs
	@REPO_ROOT=$(REPO_ROOT) GARDENER_HACK_DIR=$(GARDENER_HACK_DIR) bash $(GARDENER_HACK_DIR)/generate-sequential.sh ./charts/... ./cmd/... ./example/... ./pkg/...
	$(MAKE) format

.PHONY: format
format: $(GOIMPORTS) $(GOIMPORTSREVISER) ## Formats all files in ./cmd, ./pkg and ./test
	@bash $(GARDENER_HACK_DIR)/format.sh ./cmd ./pkg ./test ./imagevector

.PHONY: check-format
check-format: format
	@if !(git diff --quiet HEAD); then \
		echo "Unformatted files detected, please run 'make format'"; exit 1; \
	fi

.PHONY: test
test: ## Runs the unit-test suite
	@LD_FLAGS="$(LD_FLAGS)" $(HACK_DIR)/test.sh ./cmd/... ./pkg/... ./imagevector/...

.PHONY: verify
verify: check check-format test ## Run check, format and test

.PHONY: verify-extended
verify-extended: check-generate check check-format test ## Run check-generate, check, format and test

help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

#####################################################################
# Rules for local environment                                       #
#####################################################################

# speed-up skaffold deployments by building all images concurrently
extension-%: export SKAFFOLD_BUILD_CONCURRENCY = 0
extension-%: export SKAFFOLD_DEFAULT_REPO = ghcr.io
extension-%: export SKAFFOLD_PUSH = true
# use static label for skaffold to prevent rolling all gardener components on every `skaffold` invocation
extension-%: export SKAFFOLD_LABEL = skaffold.dev/run-id=gardener-extension-runtime-kata
extension-%: export LD_FLAGS = $(LD_FLAGS)

extension-up: $(SKAFFOLD) install-binaries
	GARDENER_HACK_DIR=$(GARDENER_HACK_DIR) $(SKAFFOLD) run
extension-dev: $(SKAFFOLD) install-binaries
	$(SKAFFOLD) dev --cleanup=false --trigger=manual
extension-down: $(SKAFFOLD)
	$(SKAFFOLD) delete
