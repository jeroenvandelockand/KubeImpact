#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../scripts/lib/common.sh
source "${SCRIPT_DIR}/../scripts/lib/common.sh"

printf '\nKubeImpact upgrade-tool integration suite\n'
info "Restoring the blocked fixture and creating a fresh initial scan"
"${REPO_ROOT}/scripts/reset-fixture.sh"
"${REPO_ROOT}/scripts/scan.sh" initial
"${SCRIPT_DIR}/upgrade.sh"
"${SCRIPT_DIR}/remediation.sh"
"${SCRIPT_DIR}/persistence.sh"
printf '\nUpgrade-tool integration suite passed\n\n'
