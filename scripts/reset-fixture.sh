#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

require_command kubectl
require_command curl
cluster_exists || fail "Kind cluster ${CLUSTER_NAME} does not exist."

info "Restoring the blocked fixture at the same source path"
kube apply -f "${REPO_ROOT}/demo/fixtures/blocked-configmap.yaml" >/dev/null
restart_demo_deployment
ok "Blocked fixture restored"
