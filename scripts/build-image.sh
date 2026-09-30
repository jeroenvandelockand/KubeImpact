#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

require_command docker
require_command kind
cluster_exists || fail "Kind cluster ${CLUSTER_NAME} does not exist. Run make cluster first."

info "Building local image ${KUBEIMPACT_IMAGE}"
docker build --tag "${KUBEIMPACT_IMAGE}" "${REPO_ROOT}"
info "Loading ${KUBEIMPACT_IMAGE} into ${CLUSTER_NAME}"
kind load docker-image --name "${CLUSTER_NAME}" "${KUBEIMPACT_IMAGE}"
ok "Demo image is available in Kind"
