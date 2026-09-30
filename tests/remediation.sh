#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../scripts/lib/common.sh
source "${SCRIPT_DIR}/../scripts/lib/common.sh"
# shellcheck source=lib/test.sh
source "${SCRIPT_DIR}/lib/test.sh"

initial_record="${RUNTIME_DIR}/initial-report.json"
initial_accepted="${RUNTIME_DIR}/initial-accepted.json"
[[ -s "${initial_record}" && -s "${initial_accepted}" ]] || fail "Initial scan artifacts are missing. Run make demo first."

"${REPO_ROOT}/scripts/remediate.sh"
"${REPO_ROOT}/scripts/scan.sh" remediated

remediated_record="${RUNTIME_DIR}/remediated-report.json"
remediated_accepted="${RUNTIME_DIR}/remediated-accepted.json"
printf '\nUpgrade remediation and comparison\n'
assert_jq "remediated scan completed" '.status == "completed" and .report != null' "${remediated_record}"
assert_jq "the remediated source has no 1.37 upgrade impacts" \
  '[.report.upgradeImpact[] | select((.source // "") | startswith("directory:upgrade"))] | length == 0' "${remediated_record}"

initial_request="$(jq -cS '.request' "${initial_accepted}")"
remediated_request="$(jq -cS '.request' "${remediated_accepted}")"
assert_equal "remediation uses the exact same scan request" "${initial_request}" "${remediated_request}"

initial_id="$(jq -r '.id' "${initial_record}")"
previous_id="$(jq -r '.report.comparison.previousScanId' "${remediated_record}")"
assert_equal "comparison links to the initial scan" "${initial_id}" "${previous_id}"

expected_ids="$(expected_rule_ids_json)"
resolved_ids="$(jq -c '[.report.comparison.resolvedItems[] | select(.type == "upgradeImpact" and ((.source // "") | startswith("directory:upgrade"))) | .rule] | unique | sort' "${remediated_record}")"
assert_equal "comparison resolves exactly the configured 1.37 rule IDs" "${expected_ids}" "${resolved_ids}"
assert_jq "resolved upgrade signals are marked resolved" \
  '[.report.comparison.resolvedItems[] | select(.type == "upgradeImpact" and ((.source // "") | startswith("directory:upgrade"))) | .change == "resolved"] | all' "${remediated_record}"

finish
