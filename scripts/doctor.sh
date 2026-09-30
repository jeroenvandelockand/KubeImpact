#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

printf '\nKubeImpact upgrade demo - prerequisite check\n\n'

missing=0
for command_name in docker kind kubectl curl jq make sed awk grep; do
  if command -v "${command_name}" >/dev/null 2>&1; then
    printf '[OK]   %-12s %s\n' "${command_name}" "$(command -v "${command_name}")"
  else
    printf '[MISS] %-12s\n' "${command_name}"
    missing=1
  fi
done
(( missing == 0 )) || fail "Install the missing prerequisites and run make doctor again."

docker info >/dev/null 2>&1 || fail "Docker is installed but the daemon is unavailable."
ok "Docker daemon is reachable"

require_safe_name "${CLUSTER_NAME}"
require_safe_name "${DEMO_NAMESPACE}"
require_safe_port "${KUBEIMPACT_PORT}"
ensure_runtime_dir
ok "Runtime directory is safe and writable: ${RUNTIME_DIR}"

printf '\nPinned versions\n'
printf '  Kind:             %s\n' "${KIND_VERSION}"
printf '  Kubernetes node:  %s\n' "${KIND_NODE_IMAGE}"
printf '  kubectl:          %s\n' "${KUBECTL_VERSION}"
printf '  demo image:       %s\n\n' "${KUBEIMPACT_IMAGE}"
