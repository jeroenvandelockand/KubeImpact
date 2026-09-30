#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

label="${1:-manual}"
require_safe_name "${label}"
require_command curl
require_command jq
require_command kubectl
ensure_runtime_dir
ensure_port_forward

payload="$(scan_request)"
response_file="$(mktemp "${RUNTIME_DIR}/accepted.XXXXXX")"
record_file="$(mktemp "${RUNTIME_DIR}/record.XXXXXX")"
cleanup() {
  rm -f -- "${response_file}" "${record_file}"
}
trap cleanup EXIT

info "Queuing asynchronous Kubernetes ${DEMO_CURRENT_VERSION} -> ${DEMO_TARGET_VERSION} scan"
http_status="$(curl --silent --show-error --connect-timeout 2 --max-time 15 \
  --output "${response_file}" --write-out '%{http_code}' \
  --request POST "http://127.0.0.1:${KUBEIMPACT_PORT}/api/v1/scans" \
  --header 'Content-Type: application/json' --data "${payload}")"
[[ "${http_status}" == "202" ]] || {
  sed -n '1,80p' "${response_file}" >&2
  fail "Expected HTTP 202 from scan API, got ${http_status}"
}
jq -e '.status == "pending" and (.id | type == "string" and length > 0)' "${response_file}" >/dev/null \
  || fail "The accepted scan response was not a pending record"

scan_id="$(jq -r '.id' "${response_file}")"
cp "${response_file}" "${RUNTIME_DIR}/${label}-accepted.json"
printf '%s\n' "${scan_id}" >"${RUNTIME_DIR}/${label}-scan-id"
printf '%s\n' "${scan_id}" >"${RUNTIME_DIR}/last-scan-id"
ok "Scan ${scan_id} accepted as pending"

deadline=$((SECONDS + SCAN_WAIT_SECONDS))
while true; do
  api_get "/api/v1/scans/${scan_id}" >"${record_file}"
  status="$(jq -r '.status' "${record_file}")"
  case "${status}" in
    completed)
      jq -e '.report != null' "${record_file}" >/dev/null || fail "Completed scan has no report"
      cp "${record_file}" "${RUNTIME_DIR}/${label}-report.json"
      cp "${record_file}" "${RUNTIME_DIR}/reports/${scan_id}.json"
      cp "${record_file}" "${RUNTIME_DIR}/last-report.json"
      ok "Scan ${scan_id} completed"
      break
      ;;
    failed)
      jq -r '.error // "scan failed without an error message"' "${record_file}" >&2
      fail "Scan ${scan_id} failed"
      ;;
    pending|running)
      ;;
    *)
      fail "Scan ${scan_id} returned unexpected status ${status}"
      ;;
  esac
  (( SECONDS < deadline )) || fail "Timed out after ${SCAN_WAIT_SECONDS}s waiting for scan ${scan_id}"
  sleep 2
done
