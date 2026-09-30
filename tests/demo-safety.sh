#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../scripts/lib/common.sh
source "${SCRIPT_DIR}/../scripts/lib/common.sh"

test_root="$(mktemp -d "${TMPDIR:-/tmp}/kubeimpact-demo-safety.XXXXXX")"
cleanup() {
  rm -rf -- "${test_root}"
}
trap cleanup EXIT

custom_runtime="${test_root}/runtime/${CLUSTER_NAME}"
mkdir -p "${custom_runtime}"
printf 'preserve me\n' >"${custom_runtime}/user-file"
if KUBEIMPACT_RUNTIME_DIR="${custom_runtime}" bash -c \
  'source "$1"; ensure_runtime_dir' _ "${REPO_ROOT}/scripts/lib/common.sh" >/dev/null 2>&1; then
  fail "A nonempty unmarked custom runtime directory was claimed"
fi
[[ -f "${custom_runtime}/user-file" && ! -e "${custom_runtime}/.kubeimpact-demo-runtime" ]] \
  || fail "Refusing a custom runtime directory did not preserve its contents"

empty_runtime="${test_root}/runtime/empty-demo"
mkdir -p "${empty_runtime}"
CLUSTER_NAME=empty-demo KUBEIMPACT_RUNTIME_DIR="${empty_runtime}" bash -c \
  'source "$1"; ensure_runtime_dir' _ "${REPO_ROOT}/scripts/lib/common.sh" >/dev/null
[[ -f "${empty_runtime}/.kubeimpact-demo-runtime" && -d "${empty_runtime}/reports" ]] \
  || fail "An empty custom runtime directory could not be safely claimed"

expected_id="sha256:$(printf 'a%.0s' {1..64})"
matching_node_id="${expected_id}" bash -c '
  source "$1"
  KIND_NODE_IMAGE="kindest/node:test@sha256:fixture"
  kind() { printf "%s\n" test-control-plane; }
  docker() {
    if [[ "$1" == image ]]; then printf "%s\n" "$matching_node_id"; else printf "%s\n" "$matching_node_id"; fi
  }
  verify_kind_cluster_node_image
' _ "${REPO_ROOT}/scripts/lib/common.sh" >/dev/null

mismatched_id="sha256:$(printf 'b%.0s' {1..64})"
if matching_node_id="${expected_id}" mismatched_node_id="${mismatched_id}" bash -c '
  source "$1"
  KIND_NODE_IMAGE="kindest/node:test@sha256:fixture"
  kind() { printf "%s\n" test-control-plane; }
  docker() {
    if [[ "$1" == image ]]; then printf "%s\n" "$matching_node_id"; else printf "%s\n" "$mismatched_node_id"; fi
  }
  verify_kind_cluster_node_image
' _ "${REPO_ROOT}/scripts/lib/common.sh" >/dev/null 2>&1; then
  fail "A reused Kind cluster with the wrong node image was accepted"
fi

grep -Fq 'require_command sha256sum' "${REPO_ROOT}/scripts/ci-tools.sh" \
  || fail "CI tool downloads must retain checksum verification"
if grep -Fq 'sha256sum' "${REPO_ROOT}/scripts/doctor.sh"; then
  fail "The normal prerequisite check must not require the CI-only checksum tool"
fi

awk '
  /scripts\/reset-fixture\.sh/ { reset = NR }
  /scripts\/scan\.sh" initial/ { scan = NR }
  /SCRIPT_DIR.*upgrade\.sh/ { assertions = NR }
  END { exit !(reset > 0 && reset < scan && scan < assertions) }
' "${REPO_ROOT}/tests/all.sh" \
  || fail "The integration suite must reset the blocked fixture and scan it before assertions"

ok "Demo lifecycle safety checks passed"
