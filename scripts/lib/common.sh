#!/usr/bin/env bash

set -Eeuo pipefail

COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${COMMON_DIR}/../.." && pwd)"

# shellcheck disable=SC1091
source "${REPO_ROOT}/versions.env"

CLUSTER_NAME="${CLUSTER_NAME:-kubeimpact-demo}"
KUBE_CONTEXT="kind-${CLUSTER_NAME}"
DEMO_NAMESPACE="kubeimpact-demo"
KUBEIMPACT_PORT="${KUBEIMPACT_PORT:-18080}"
SCAN_WAIT_SECONDS="${KUBEIMPACT_SCAN_WAIT_SECONDS:-180}"
ROLLOUT_WAIT_SECONDS="${KUBEIMPACT_ROLLOUT_WAIT_SECONDS:-180}"
EXPECTED_137_RULE_IDS="${KUBEIMPACT_EXPECTED_RULE_IDS:-${EXPECTED_137_RULE_IDS}}"

RUNTIME_PARENT_DEFAULT="${TMPDIR:-/tmp}/kubeimpact-demo-${UID:-0}"
RUNTIME_DIR_DEFAULT="${RUNTIME_PARENT_DEFAULT}/runtime/${CLUSTER_NAME}"
RUNTIME_DIR="${KUBEIMPACT_RUNTIME_DIR:-${RUNTIME_DIR_DEFAULT}}"
RUNTIME_MARKER="${RUNTIME_DIR}/.kubeimpact-demo-runtime"
PORT_FORWARD_PID_FILE="${RUNTIME_DIR}/port-forward.pid"
PORT_FORWARD_LOG_FILE="${RUNTIME_DIR}/port-forward.log"

export REPO_ROOT CLUSTER_NAME KUBE_CONTEXT DEMO_NAMESPACE KUBEIMPACT_PORT
export SCAN_WAIT_SECONDS ROLLOUT_WAIT_SECONDS EXPECTED_137_RULE_IDS RUNTIME_DIR

color_enabled() {
  [[ -t 1 && -z "${NO_COLOR:-}" ]]
}

info() {
  if color_enabled; then printf '\033[1;34m==>\033[0m %s\n' "$*"; else printf '==> %s\n' "$*"; fi
}

ok() {
  if color_enabled; then printf '\033[1;32m[OK]\033[0m %s\n' "$*"; else printf '[OK] %s\n' "$*"; fi
}

fail() {
  if color_enabled; then printf '\033[1;31m[FAIL]\033[0m %s\n' "$*" >&2; else printf '[FAIL] %s\n' "$*" >&2; fi
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "Required command not found: $1"
}

require_safe_name() {
  [[ "$1" =~ ^[a-z0-9][a-z0-9-]{0,50}$ ]] || fail "Unsafe name: $1"
}

require_safe_port() {
  [[ "$1" =~ ^[0-9]+$ ]] && (( 10#$1 >= 1024 && 10#$1 <= 65535 )) || fail "Unsafe TCP port: $1"
}

require_safe_runtime_dir() {
  local runtime_parent runtime_leaf
  [[ "${RUNTIME_DIR}" == /* ]] || fail "Runtime path must be absolute: ${RUNTIME_DIR}"
  runtime_leaf="${RUNTIME_DIR##*/}"
  runtime_parent="$(dirname "${RUNTIME_DIR}")"
  [[ "${runtime_leaf}" == "${CLUSTER_NAME}" ]] || fail "Runtime directory must end in the cluster name: ${RUNTIME_DIR}"
  [[ "${runtime_parent##*/}" == "runtime" ]] || fail "Runtime directory parent must be named runtime: ${RUNTIME_DIR}"
  if [[ -z "${KUBEIMPACT_RUNTIME_DIR:-}" ]]; then
    [[ "${RUNTIME_DIR}" == "${RUNTIME_DIR_DEFAULT}" ]] || fail "Unexpected default runtime directory: ${RUNTIME_DIR}"
  fi
}

runtime_dir_has_entries() (
  shopt -s dotglob nullglob
  local entries=("${RUNTIME_DIR}"/*)
  (( ${#entries[@]} > 0 ))
)

ensure_runtime_dir() {
  require_safe_runtime_dir
  [[ ! -L "${RUNTIME_DIR}" ]] || fail "Runtime directory must not be a symbolic link: ${RUNTIME_DIR}"
  [[ ! -e "${RUNTIME_DIR}" || -d "${RUNTIME_DIR}" ]] \
    || fail "Runtime path exists but is not a directory: ${RUNTIME_DIR}"
  if [[ -n "${KUBEIMPACT_RUNTIME_DIR:-}" && -d "${RUNTIME_DIR}" && ! -f "${RUNTIME_MARKER}" ]] \
    && runtime_dir_has_entries; then
    fail "Refusing to claim nonempty unmarked runtime directory: ${RUNTIME_DIR}"
  fi
  [[ ! -L "${RUNTIME_MARKER}" ]] || fail "Runtime marker must not be a symbolic link: ${RUNTIME_MARKER}"
  [[ ! -e "${RUNTIME_MARKER}" || -f "${RUNTIME_MARKER}" ]] \
    || fail "Runtime marker exists but is not a regular file: ${RUNTIME_MARKER}"
  mkdir -p "${RUNTIME_DIR}/reports"
  if [[ ! -f "${RUNTIME_MARKER}" ]]; then
    : >"${RUNTIME_MARKER}"
  fi
  [[ -w "${RUNTIME_DIR}" ]] || fail "Runtime directory is not writable: ${RUNTIME_DIR}"
}

verify_kind_cluster_node_image() {
  local expected_image_id nodes node actual_image_id
  expected_image_id="$(docker image inspect --format '{{.Id}}' "${KIND_NODE_IMAGE}" 2>/dev/null)" \
    || fail "Cannot resolve the pinned Kind node image locally: ${KIND_NODE_IMAGE}"
  [[ "${expected_image_id}" =~ ^sha256:[0-9a-f]{64}$ ]] \
    || fail "Pinned Kind node image returned an invalid image ID: ${expected_image_id:-empty}"

  nodes="$(kind get nodes --name "${CLUSTER_NAME}" 2>/dev/null)" \
    || fail "Cannot inspect nodes for existing Kind cluster ${CLUSTER_NAME}"
  [[ -n "${nodes}" ]] || fail "Existing Kind cluster ${CLUSTER_NAME} has no nodes"
  while IFS= read -r node; do
    [[ -n "${node}" ]] || continue
    actual_image_id="$(docker inspect --type container --format '{{.Image}}' "${node}" 2>/dev/null)" \
      || fail "Cannot inspect Kind node container ${node}"
    [[ "${actual_image_id}" == "${expected_image_id}" ]] \
      || fail "Kind node ${node} uses image ID ${actual_image_id:-unknown}; expected ${expected_image_id} from ${KIND_NODE_IMAGE}"
  done <<<"${nodes}"
}

cluster_exists() {
  kind get clusters 2>/dev/null | grep -Fxq "${CLUSTER_NAME}"
}

kube() {
  kubectl --context "${KUBE_CONTEXT}" "$@"
}

wait_until() {
  local timeout="$1" interval="$2" description="$3"
  shift 3
  local deadline=$((SECONDS + timeout))
  until "$@"; do
    (( SECONDS >= deadline )) && fail "Timed out after ${timeout}s waiting for ${description}"
    sleep "${interval}"
  done
}

deployment_ready() {
  [[ "$(kube -n "${DEMO_NAMESPACE}" get deployment/kubeimpact -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)" == "1" ]]
}

wait_for_deployment() {
  wait_until "${ROLLOUT_WAIT_SECONDS}" 2 "KubeImpact deployment" deployment_ready
}

stop_port_forward() {
  local pid="" command_line=""
  if [[ -f "${PORT_FORWARD_PID_FILE}" ]]; then
    pid="$(<"${PORT_FORWARD_PID_FILE}")"
    if [[ "${pid}" =~ ^[0-9]+$ ]] && kill -0 "${pid}" 2>/dev/null; then
      command_line="$(ps -p "${pid}" -o command= 2>/dev/null || true)"
      if [[ "${command_line}" == *"port-forward service/kubeimpact ${KUBEIMPACT_PORT}:80"* ]]; then
        kill "${pid}" 2>/dev/null || true
        wait "${pid}" 2>/dev/null || true
      else
        info "Ignoring stale port-forward PID ${pid}; it belongs to another process" >&2
      fi
    fi
    rm -f -- "${PORT_FORWARD_PID_FILE}"
  fi
}

port_forward_process_ready() {
  local pid="" command_line=""
  [[ -f "${PORT_FORWARD_PID_FILE}" ]] || return 1
  pid="$(<"${PORT_FORWARD_PID_FILE}")"
  [[ "${pid}" =~ ^[0-9]+$ ]] && kill -0 "${pid}" 2>/dev/null || return 1
  command_line="$(ps -p "${pid}" -o command= 2>/dev/null || true)"
  [[ "${command_line}" == *"port-forward service/kubeimpact ${KUBEIMPACT_PORT}:80"* ]]
}

api_health_ready() {
  port_forward_process_ready || return 1
  curl --silent --show-error --fail --connect-timeout 1 --max-time 3 \
    "http://127.0.0.1:${KUBEIMPACT_PORT}/api/v1/health" >/dev/null 2>&1
}

ensure_port_forward() {
  require_safe_port "${KUBEIMPACT_PORT}"
  ensure_runtime_dir
  if api_health_ready; then
    return 0
  fi
  stop_port_forward
  info "Starting API port-forward on http://127.0.0.1:${KUBEIMPACT_PORT}" >&2
  nohup kubectl --context "${KUBE_CONTEXT}" -n "${DEMO_NAMESPACE}" \
    port-forward service/kubeimpact "${KUBEIMPACT_PORT}:80" \
    </dev/null >"${PORT_FORWARD_LOG_FILE}" 2>&1 &
  printf '%s\n' "$!" >"${PORT_FORWARD_PID_FILE}"
  wait_until 30 1 "KubeImpact API port-forward" api_health_ready
}

api_get() {
  local path="$1"
  ensure_port_forward
  curl --silent --show-error --fail --connect-timeout 2 --max-time 15 \
    "http://127.0.0.1:${KUBEIMPACT_PORT}${path}"
}

scan_request() {
  jq -cn \
    --arg target "${DEMO_TARGET_VERSION}" \
    '{targetVersion: $target, includeCluster: true, sources: [{type: "directory", path: "upgrade"}]}'
}

last_scan_id() {
  [[ -s "${RUNTIME_DIR}/last-scan-id" ]] || fail "No saved scan ID. Run make demo or make scan first."
  <"${RUNTIME_DIR}/last-scan-id"
}

saved_record_path() {
  local label="$1"
  printf '%s/%s-report.json\n' "${RUNTIME_DIR}" "${label}"
}

restart_demo_deployment() {
  stop_port_forward
  kube -n "${DEMO_NAMESPACE}" rollout restart deployment/kubeimpact >/dev/null
  kube -n "${DEMO_NAMESPACE}" rollout status deployment/kubeimpact \
    --timeout="${ROLLOUT_WAIT_SECONDS}s" >/dev/null
  wait_for_deployment
  ensure_port_forward
}
