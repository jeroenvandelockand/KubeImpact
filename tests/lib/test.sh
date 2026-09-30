#!/usr/bin/env bash

set -Eeuo pipefail

pass_count=0
fail_count=0

pass() {
  pass_count=$((pass_count + 1))
  printf 'PASS  %s\n' "$*"
}

record_fail() {
  fail_count=$((fail_count + 1))
  printf 'FAIL  %s\n' "$*" >&2
}

assert_command() {
  local description="$1"
  shift
  if "$@" >/dev/null 2>&1; then pass "${description}"; else record_fail "${description}"; fi
}

assert_jq() {
  local description="$1" expression="$2" file="$3"
  if jq -e "${expression}" "${file}" >/dev/null 2>&1; then pass "${description}"; else record_fail "${description}"; fi
}

assert_equal() {
  local description="$1" expected="$2" actual="$3"
  if [[ "${expected}" == "${actual}" ]]; then
    pass "${description}"
  else
    record_fail "${description} (expected ${expected}, got ${actual})"
  fi
}

expected_rule_ids_json() {
  printf '%s\n' "${EXPECTED_137_RULE_IDS}" |
    tr '[:space:]' '\n' |
    sed '/^$/d' |
    jq -R . |
    jq -cs 'unique | sort'
}

finish() {
  printf '\n%d passed, %d failed\n' "${pass_count}" "${fail_count}"
  (( fail_count == 0 ))
}
