# renovate: datasource=github-releases depName=ko-build/ko
KO_VERSION ?= v0.19.1

KO := $(TOOLS_BIN_DIR)/ko
$(KO): $(call tool_version_file,$(KO),$(KO_VERSION))
	GOBIN=$(abspath $(TOOLS_BIN_DIR)) go install github.com/google/ko@$(KO_VERSION)

# renovate: datasource=github-releases depName=google/go-containerregistry
CRANE_VERSION ?= v0.22.1

CRANE := $(TOOLS_BIN_DIR)/crane
$(CRANE): $(call tool_version_file,$(CRANE),$(CRANE_VERSION))
	GOBIN=$(abspath $(TOOLS_BIN_DIR)) go install github.com/google/go-containerregistry/cmd/crane@$(CRANE_VERSION)

# renovate: datasource=go depName=golang.org/x/vuln
GOVULNCHECK_VERSION ?= v1.8.0

GOVULNCHECK := $(TOOLS_BIN_DIR)/govulncheck
$(GOVULNCHECK): $(call tool_version_file,$(GOVULNCHECK),$(GOVULNCHECK_VERSION))
	GOBIN=$(abspath $(TOOLS_BIN_DIR)) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

