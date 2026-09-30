#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

require_command kind
require_command kubectl
require_command jq
require_command curl

printf '\nKubeImpact demo status\n\n'
if ! cluster_exists; then
  printf 'Cluster %s does not exist. Run make demo.\n' "${CLUSTER_NAME}"
  exit 0
fi

printf 'Kubernetes version\n'
kube version -o json 2>/dev/null | jq -r '"  client: \(.clientVersion.gitVersion)\n  server: \(.serverVersion.gitVersion)"' || true
printf '\nKubeImpact workload and storage\n'
kube -n "${DEMO_NAMESPACE}" get deployment,pod,service,pvc -o wide 2>/dev/null || true
printf '\nFixture\n'
kube -n "${DEMO_NAMESPACE}" get configmap/kubeimpact-upgrade-source \
  -o custom-columns='NAME:.metadata.name,PHASE:.metadata.labels.kubeimpact\.io/fixture-phase,RESOURCE_VERSION:.metadata.resourceVersion' 2>/dev/null || true

if deployment_ready; then
  ensure_port_forward
  printf '\nAPI health\n'
  api_get /api/v1/health | jq . || true
  printf '\nLatest report\n'
  if latest="$(api_get /api/v1/report/latest 2>/dev/null)"; then
    jq '{scanId, clusterVersion, targetVersion, score, summary, comparison, upgradeRules: [.upgradeImpact[].rule]}' <<<"${latest}"
  else
    printf '  no completed report yet\n'
  fi
fi
printf '\nRuntime files: %s\n\n' "${RUNTIME_DIR}"
