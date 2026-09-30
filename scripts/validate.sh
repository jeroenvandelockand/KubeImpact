#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

require_command kubectl

info "Checking shell syntax"
while IFS= read -r script; do
  bash -n "${script}"
done < <(find "${REPO_ROOT}/scripts" "${REPO_ROOT}/tests" -type f -name '*.sh' | sort)

info "Checking demo lifecycle safety"
"${REPO_ROOT}/tests/demo-safety.sh"

info "Checking Kubernetes manifest syntax with the local Kustomize parser"
for manifest_dir in \
  "${REPO_ROOT}/demo" \
  "${REPO_ROOT}/demo/fixtures/blocked" \
  "${REPO_ROOT}/demo/fixtures/remediated"; do
  kubectl kustomize --load-restrictor=LoadRestrictionsNone "${manifest_dir}" >/dev/null
done

info "Checking pinned and local-only image configuration"
grep -Fq 'KIND_NODE_IMAGE=kindest/node:v1.36.' "${REPO_ROOT}/versions.env" \
  || fail "versions.env must pin a Kubernetes 1.36 Kind node image"
grep -Eq '^KIND_NODE_IMAGE=.*@sha256:[0-9a-f]{64}$' "${REPO_ROOT}/versions.env" \
  || fail "Kind node image must include an immutable sha256 digest"
grep -Fq 'image: kubeimpact:demo' "${REPO_ROOT}/demo/kubeimpact.yaml" \
  || fail "Demo deployment must use kubeimpact:demo"
grep -Fq 'imagePullPolicy: Never' "${REPO_ROOT}/demo/kubeimpact.yaml" \
  || fail "Demo deployment must not pull the local image"
grep -Fq 'mountPath: /sources' "${REPO_ROOT}/demo/kubeimpact.yaml" \
  || fail "Demo source must be mounted at /sources"
grep -A2 -F 'mountPath: /sources' "${REPO_ROOT}/demo/kubeimpact.yaml" | grep -Fq 'readOnly: true' \
  || fail "/sources must be mounted read-only"

info "Checking required demo files"
for path in \
  Makefile versions.env bootstrap/kind/cluster.yaml demo/kubeimpact.yaml \
  demo/fixtures/blocked-configmap.yaml demo/fixtures/remediated-configmap.yaml \
  scripts/lib/common.sh scripts/scan.sh scripts/diagnostics.sh tests/all.sh docs/demo.md \
  .github/workflows/integration.yml; do
  [[ -f "${REPO_ROOT}/${path}" ]] || fail "Missing required demo file: ${path}"
done

info "Checking fixture coverage"
for signature in \
  'scheduling.k8s.io/v1alpha2 Workload' \
  'scheduling.k8s.io/v1alpha2 PodGroup' \
  'kubeadm.k8s.io/v1beta3 InitConfiguration' \
  'kubeadm.k8s.io/v1beta3 ClusterConfiguration' \
  'kubeadm.k8s.io/v1beta3 JoinConfiguration'; do
  api_version="${signature% *}"
  kind="${signature##* }"
  grep -Fq "apiVersion: ${api_version}" "${REPO_ROOT}/demo/fixtures/blocked-configmap.yaml" \
    || fail "Blocked fixture is missing ${api_version}"
  grep -Fq "kind: ${kind}" "${REPO_ROOT}/demo/fixtures/blocked-configmap.yaml" \
    || fail "Blocked fixture is missing ${kind}"
done

info "Checking configured rule IDs exist in the Kubernetes 1.37 knowledge base"
configured_ids="$(printf '%s\n' "${EXPECTED_137_RULE_IDS}" | tr '[:space:]' ' ' | sed 's/  */ /g')"
duplicate_id="$(printf '%s\n' "${EXPECTED_137_RULE_IDS}" | tr '[:space:]' '\n' | sed '/^$/d' | awk 'seen[$0]++ {print; exit}')"
[[ -z "${duplicate_id}" ]] || fail "Configured demo rule ID is duplicated: ${duplicate_id}"
while IFS= read -r rule_id; do
  [[ -n "${rule_id}" ]] || continue
  grep -Fq "id: \"${rule_id}\"" "${REPO_ROOT}/rules/kubernetes/1.37.yaml" \
    || fail "Configured demo rule is missing from rules/kubernetes/1.37.yaml: ${rule_id}"
done < <(printf '%s\n' "${EXPECTED_137_RULE_IDS}" | tr '[:space:]' '\n' | sed '/^$/d')
while IFS= read -r rule_id; do
  [[ " ${configured_ids} " == *" ${rule_id} "* ]] \
    || fail "Kubernetes 1.37 rule is not covered by the demo expected set: ${rule_id}"
done < <(sed -n 's/^[[:space:]]*- id: "\([^"]*\)"/\1/p' "${REPO_ROOT}/rules/kubernetes/1.37.yaml")

ok "Offline demo validation passed"
