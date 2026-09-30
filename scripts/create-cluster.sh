#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

require_command docker
require_command kind
require_command kubectl
require_command jq
require_safe_name "${CLUSTER_NAME}"

if cluster_exists; then
  info "Kind cluster ${CLUSTER_NAME} already exists"
  verify_kind_cluster_node_image
  ok "Existing Kind cluster uses the pinned node image"
else
  info "Creating Kind cluster ${CLUSTER_NAME} with ${KIND_NODE_IMAGE}"
  kind create cluster \
    --name "${CLUSTER_NAME}" \
    --image "${KIND_NODE_IMAGE}" \
    --config "${REPO_ROOT}/bootstrap/kind/cluster.yaml" \
    --wait 180s
fi

kube wait --for=condition=Ready nodes --all --timeout=180s >/dev/null
server_minor="$(kube version -o json | jq -r '.serverVersion.minor' | tr -cd '0-9')"
[[ "${server_minor}" == "36" ]] || fail "Expected Kubernetes 1.36, got server minor ${server_minor:-unknown}"
ok "Kind cluster is ready on Kubernetes 1.36"
