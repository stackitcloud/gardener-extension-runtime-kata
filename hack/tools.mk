# renovate: datasource=github-releases depName=ko-build/ko
KO_VERSION ?= v0.19.1

KO := $(TOOLS_BIN_DIR)/ko
$(KO): $(call tool_version_file,$(KO),$(KO_VERSION))
	GOBIN=$(abspath $(TOOLS_BIN_DIR)) go install github.com/google/ko@$(KO_VERSION)
