#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

echo "> Test"

test_flags=
# If running in Prow, generate a machine-readable output file under $ARTIFACTS for the JUnit view.
if [ -n "${CI:-}" -a -n "${ARTIFACTS:-}" ] ; then
  if which report-collector &>/dev/null; then
    mkdir -p "$ARTIFACTS"
    trap "report-collector \"$ARTIFACTS/junit.xml\"" EXIT
    test_flags="--ginkgo.junit-report=junit.xml"
  else
    echo "report-collector not found in PATH, not generating machine-readable test report"
  fi
  test_flags+=" --ginkgo.timeout=2m"
else
  timeout_flag=-timeout=2m
fi

ldflags_flag=()
if [ -n "${LD_FLAGS:-}" ]; then
  ldflags_flag=("-ldflags" "${LD_FLAGS}")
fi

go test "${ldflags_flag[@]}" -race ${timeout_flag:-} "$@" $test_flags | grep -v 'no test files'
