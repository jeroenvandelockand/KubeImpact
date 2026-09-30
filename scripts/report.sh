#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

require_command jq
ensure_runtime_dir
label="${1:-}"
if [[ -n "${label}" ]]; then
  require_safe_name "${label}"
  record_file="$(saved_record_path "${label}")"
else
  record_file="${RUNTIME_DIR}/last-report.json"
fi
[[ -s "${record_file}" ]] || fail "Saved report not found: ${record_file}"

printf '\nUpgrade scan summary\n\n'
jq -r '
  .report as $r |
  "  scan:       \($r.scanId)",
  "  cluster:    \($r.clusterVersion)",
  "  target:     \($r.targetVersion)",
  "  score:      \($r.score)",
  "  findings:   critical=\($r.summary.critical) high=\($r.summary.high) medium=\($r.summary.medium) low=\($r.summary.low) info=\($r.summary.info)",
  "  comparison: new=\($r.comparison.new) unchanged=\($r.comparison.unchanged) resolved=\($r.comparison.resolved)"
' "${record_file}"

printf '\nKubernetes upgrade impacts\n'
jq -r '
  if (.report.upgradeImpact | length) == 0 then
    "  none"
  else
    .report.upgradeImpact[] |
    "  [\(.severity)] \(.rule)  \(.kind)/\(.name)  \(.currentValue) -> \(.expectedValue)  (\(.change))"
  end
' "${record_file}"

if [[ "$(jq '.report.comparison.resolvedItems | length' "${record_file}")" -gt 0 ]]; then
  printf '\nResolved since the comparable scan\n'
  jq -r '.report.comparison.resolvedItems[] | "  [\(.severity)] \(.rule)  \(.kind)/\(.name)  (\(.change))"' "${record_file}"
fi
printf '\nFull JSON: %s\n\n' "${record_file}"
