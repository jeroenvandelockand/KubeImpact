#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

require_command kubectl
require_command curl
cluster_exists || fail "Kind cluster ${CLUSTER_NAME} does not exist."
[[ -s "${RUNTIME_DIR}/initial-report.json" ]] || fail "Initial report is missing. Run make demo first."

info "Updating the same ConfigMap-backed source path to supported API versions"
kube apply -f "${REPO_ROOT}/demo/fixtures/remediated-configmap.yaml" >/dev/null
[[ "$(kube -n "${DEMO_NAMESPACE}" get configmap/kubeimpact-upgrade-source -o jsonpath='{.metadata.labels.kubeimpact\.io/fixture-phase}')" == "remediated" ]] \
  || fail "The remediated source ConfigMap was not applied"
restart_demo_deployment
ok "Remediated fixture is mounted at the unchanged /sources/upgrade request path"
