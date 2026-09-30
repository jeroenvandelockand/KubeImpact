#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

require_command docker
require_command kind
require_safe_name "${CLUSTER_NAME}"
cluster_exists || fail "Kind cluster ${CLUSTER_NAME} does not exist. Run make cluster first."

info "Building local image ${KUBEIMPACT_IMAGE}"
docker build --tag "${KUBEIMPACT_IMAGE}" "${REPO_ROOT}"

image_archive_dir="$(mktemp -d "${TMPDIR:-/tmp}/kubeimpact-image.XXXXXX")"
image_archive="${image_archive_dir}/kubeimpact-image.tar"
cleanup() {
  rm -rf -- "${image_archive_dir}"
}
trap cleanup EXIT

info "Saving ${KUBEIMPACT_IMAGE} for direct containerd import"
docker image save --output "${image_archive}" "${KUBEIMPACT_IMAGE}"

# `kind load docker-image` parses containerd's config before loading. Older
# Kind clients only understand config versions 2 and 3, while Kubernetes 1.36
# node images use version 4. Importing with ctr talks to the running daemon and
# works independently of the config-file version.
kind_node_count=0
while IFS= read -r kind_node; do
  [[ -n "${kind_node}" ]] || continue
  kind_node_count=$((kind_node_count + 1))
  info "Importing ${KUBEIMPACT_IMAGE} into ${kind_node}"
  docker exec -i "${kind_node}" \
    ctr --namespace k8s.io images import - <"${image_archive}" >/dev/null \
    || fail "Could not import ${KUBEIMPACT_IMAGE} into ${kind_node}"

  node_images="$(docker exec "${kind_node}" \
    ctr --namespace k8s.io images list --quiet)" \
    || fail "Could not inspect images in ${kind_node}"
  grep -Fq "${KUBEIMPACT_IMAGE}" <<<"${node_images}" \
    || fail "Imported image is not visible in ${kind_node}'s k8s.io namespace"
done < <(kind get nodes --name "${CLUSTER_NAME}")

(( kind_node_count > 0 )) || fail "No nodes found for Kind cluster ${CLUSTER_NAME}"

ok "Demo image is available on every Kind node"
