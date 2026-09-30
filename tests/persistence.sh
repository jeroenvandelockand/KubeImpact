#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../scripts/lib/common.sh
source "${SCRIPT_DIR}/../scripts/lib/common.sh"
# shellcheck source=lib/test.sh
source "${SCRIPT_DIR}/lib/test.sh"

record_file="${RUNTIME_DIR}/remediated-report.json"
[[ -s "${record_file}" ]] || fail "Remediated scan artifact is missing. Run make test-remediation first."
scan_id="$(jq -r '.id' "${record_file}")"
initial_id="$(jq -r '.id' "${RUNTIME_DIR}/initial-report.json")"

api_get "/api/v1/scans/${scan_id}" >/dev/null
old_pod="$(kube -n "${DEMO_NAMESPACE}" get pod -l app.kubernetes.io/name=kubeimpact -o jsonpath='{.items[0].metadata.name}')"
restart_demo_deployment
new_pod="$(kube -n "${DEMO_NAMESPACE}" get pod -l app.kubernetes.io/name=kubeimpact -o jsonpath='{.items[0].metadata.name}')"

after_restart="$(mktemp "${RUNTIME_DIR}/persistence.XXXXXX")"
history_file="$(mktemp "${RUNTIME_DIR}/history.XXXXXX")"
cleanup() {
  rm -f -- "${after_restart}" "${history_file}"
}
trap cleanup EXIT
api_get "/api/v1/scans/${scan_id}" >"${after_restart}"
api_get '/api/v1/reports?limit=20' >"${history_file}"

printf '\nSQLite report persistence across pod restart\n'
if [[ -n "${old_pod}" && -n "${new_pod}" && "${old_pod}" != "${new_pod}" ]]; then
  pass "deployment created a replacement pod"
else
  record_fail "deployment did not create a replacement pod"
fi
# Keep the jq checks separate so scan IDs remain data, not jq source.
if jq -e --arg id "${scan_id}" '.status == "completed" and .report.scanId == $id' "${after_restart}" >/dev/null; then
  pass "remediated report is readable by scan ID after restart"
else
  record_fail "remediated report is readable by scan ID after restart"
fi
if jq -e --arg initial "${initial_id}" --arg remediated "${scan_id}" \
  '[.reports[].id] as $ids | ($ids | index($initial)) != null and ($ids | index($remediated)) != null' \
  "${history_file}" >/dev/null; then
  pass "report history retains both comparable scans after restart"
else
  record_fail "report history retains both comparable scans after restart"
fi

finish
