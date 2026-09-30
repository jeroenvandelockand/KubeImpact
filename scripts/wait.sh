#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

require_command kubectl
require_command curl
require_command jq
cluster_exists || fail "Kind cluster ${CLUSTER_NAME} does not exist."

wait_for_deployment
ensure_port_forward
response="$(api_get /api/v1/health)"
[[ "$(jq -r '.status' <<<"${response}")" == "ok" ]] || fail "KubeImpact health response was not OK"
ok "KubeImpact API is ready at http://127.0.0.1:${KUBEIMPACT_PORT}"
