#!/usr/bin/env bash

set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

require_command curl
require_command sha256sum

case "$(uname -m)" in
  x86_64)
    arch=amd64
    kind_sha="${KIND_LINUX_AMD64_SHA256}"
    kubectl_sha="${KUBECTL_LINUX_AMD64_SHA256}"
    ;;
  aarch64|arm64)
    arch=arm64
    kind_sha="${KIND_LINUX_ARM64_SHA256}"
    kubectl_sha="${KUBECTL_LINUX_ARM64_SHA256}"
    ;;
  *)
    fail "Unsupported CI architecture: $(uname -m)"
    ;;
esac

install_dir="${RUNNER_TEMP:-/tmp}/kubeimpact-demo-tools"
mkdir -p "${install_dir}"

info "Installing Kind ${KIND_VERSION}"
curl --fail --location --retry 3 --output "${install_dir}/kind" \
  "https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-linux-${arch}"
printf '%s  %s\n' "${kind_sha}" "${install_dir}/kind" | sha256sum --check --status
chmod +x "${install_dir}/kind"

info "Installing kubectl ${KUBECTL_VERSION}"
curl --fail --location --retry 3 --output "${install_dir}/kubectl" \
  "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${arch}/kubectl"
printf '%s  %s\n' "${kubectl_sha}" "${install_dir}/kubectl" | sha256sum --check --status
chmod +x "${install_dir}/kubectl"

if [[ -n "${GITHUB_PATH:-}" ]]; then
  printf '%s\n' "${install_dir}" >>"${GITHUB_PATH}"
else
  printf 'Add %s to PATH.\n' "${install_dir}"
fi
ok "Pinned CI tools installed"
