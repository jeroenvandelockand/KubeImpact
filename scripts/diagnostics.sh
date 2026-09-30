#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

printf '\nKubeImpact demo diagnostics\n'
if ! command -v kind >/dev/null 2>&1 || ! command -v kubectl >/dev/null 2>&1 || ! cluster_exists; then
  printf 'Cluster %s is not available.\n' "${CLUSTER_NAME}"
  exit 0
fi

printf '\nResources\n'
kube get nodes -o wide 2>&1 || true
kube -n "${DEMO_NAMESPACE}" get all,pvc,configmap -o wide 2>&1 || true
printf '\nDeployment description\n'
kube -n "${DEMO_NAMESPACE}" describe deployment/kubeimpact 2>&1 || true
printf '\nKubeImpact logs (last 200 lines)\n'
kube -n "${DEMO_NAMESPACE}" logs deployment/kubeimpact --tail=200 2>&1 || true
printf '\nRecent events\n'
kube get events -A --sort-by=.lastTimestamp 2>&1 | tail -n 100 || true
if [[ -f "${PORT_FORWARD_LOG_FILE}" ]]; then
  printf '\nPort-forward log\n'
  tail -n 100 "${PORT_FORWARD_LOG_FILE}" || true
fi
if [[ -s "${RUNTIME_DIR}/last-report.json" ]]; then
  printf '\nLast locally saved scan record\n'
  jq '{id, status, error, request, report: (.report | {scanId, clusterVersion, targetVersion, summary, comparison, warnings, upgradeImpact})}' \
    "${RUNTIME_DIR}/last-report.json" 2>&1 || true
fi
