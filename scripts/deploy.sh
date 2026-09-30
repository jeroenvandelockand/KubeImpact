#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

require_command kubectl
require_command curl
cluster_exists || fail "Kind cluster ${CLUSTER_NAME} does not exist. Run make cluster first."

stop_port_forward
info "Applying the KubeImpact demo deployment"
kube apply -f "${REPO_ROOT}/demo/kubeimpact.yaml" >/dev/null
info "Mounting the blocked fixture read-only at /sources"
kube apply -f "${REPO_ROOT}/demo/fixtures/blocked-configmap.yaml" >/dev/null
restart_demo_deployment
ok "KubeImpact and the blocked upgrade fixture are deployed"
