#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../scripts/lib/common.sh
source "${SCRIPT_DIR}/../scripts/lib/common.sh"
# shellcheck source=lib/test.sh
source "${SCRIPT_DIR}/lib/test.sh"

record_file="${RUNTIME_DIR}/initial-report.json"
accepted_file="${RUNTIME_DIR}/initial-accepted.json"
[[ -s "${record_file}" && -s "${accepted_file}" ]] || fail "Initial scan artifacts are missing. Run make demo first."

printf '\nKubernetes 1.37 upgrade detection\n'
source_mount_is_read_only() {
  kube -n "${DEMO_NAMESPACE}" get deployment/kubeimpact -o json |
    jq -e '[.spec.template.spec.containers[] | select(.name == "kubeimpact") | .volumeMounts[] | select(.mountPath == "/sources" and .readOnly == true)] | length == 1'
}
assert_command "fixture volume is mounted read-only at /sources" source_mount_is_read_only
assert_jq "scan was accepted asynchronously as pending" '.status == "pending"' "${accepted_file}"
assert_jq "scan completed with a persisted report" '.status == "completed" and .report != null' "${record_file}"
assert_jq "the connected cluster is Kubernetes 1.36" '.report.clusterVersion | sub("^v"; "") | startswith("1.36.")' "${record_file}"
assert_jq "the scan targets Kubernetes 1.37" '.report.targetVersion == "1.37"' "${record_file}"
assert_jq "the directory source uses the mounted upgrade path" '[.report.sources[] | select(.type == "directory" and .location == "upgrade")] | length == 1' "${record_file}"

expected_ids="$(expected_rule_ids_json)"
actual_ids="$(jq -c '[.report.upgradeImpact[] | select((.source // "") | startswith("directory:upgrade")) | .rule] | unique | sort' "${record_file}")"
assert_equal "fixture produces exactly the configured 1.37 rule IDs" "${expected_ids}" "${actual_ids}"
assert_jq "every configured upgrade impact has a stable fingerprint" \
  '[.report.upgradeImpact[] | select((.source // "") | startswith("directory:upgrade")) | .fingerprint | length > 0] | all' "${record_file}"

finish
