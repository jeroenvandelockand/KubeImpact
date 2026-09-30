#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

require_safe_name "${CLUSTER_NAME}"
require_safe_runtime_dir
if [[ -d "${RUNTIME_DIR}" ]]; then
  [[ -f "${RUNTIME_MARKER}" && ! -L "${RUNTIME_MARKER}" ]] \
    || fail "Refusing to use unmarked runtime directory: ${RUNTIME_DIR}"
fi
stop_port_forward

if command -v kind >/dev/null 2>&1 && cluster_exists; then
  info "Deleting only Kind cluster ${CLUSTER_NAME}"
  kind delete cluster --name "${CLUSTER_NAME}"
fi

if [[ -d "${RUNTIME_DIR}" ]]; then
  info "Removing ephemeral runtime files ${RUNTIME_DIR}"
  rm -rf -- "${RUNTIME_DIR}"
fi
ok "Demo resources removed"
