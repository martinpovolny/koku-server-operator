#!/usr/bin/env bash
# Run the cost-onprem-chart operator acceptance gate against a live cluster.
#
# The gate is the chart pytest suite filtered by @pytest.mark.operator
# (see cost-onprem-chart docs/development/operator-test-compat.md).
#
# Usage:
#   # Collect-only (no cluster required) — validates markers/harness:
#   COLLECT_ONLY=1 ./hack/run-chart-operator-gate.sh
#
#   # Full gate against an already-deployed operator stack:
#   export KUBECONFIG=...
#   export NAMESPACE=cost-tests
#   export HELM_RELEASE_NAME=cost-onprem
#   ./hack/run-chart-operator-gate.sh
#
# Chart source (defaults point at the fork branch with operator markers):
#   CHART_REPO   git URL (default: https://github.com/martinpovolny/cost-onprem-chart.git)
#   CHART_REF    branch/tag/sha (default: feat/operator-test-compat)
#   CHART_DIR    existing local checkout (skips clone when set)

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_REPO="${CHART_REPO:-https://github.com/martinpovolny/cost-onprem-chart.git}"
CHART_REF="${CHART_REF:-feat/operator-test-compat}"
COLLECT_ONLY="${COLLECT_ONLY:-0}"

cleanup_chart_dir=""
if [[ -n "${CHART_DIR:-}" ]]; then
  chart_dir="${CHART_DIR}"
  if [[ ! -d "${chart_dir}" ]]; then
    echo "CHART_DIR does not exist: ${chart_dir}" >&2
    exit 1
  fi
else
  chart_dir="$(mktemp -d "${TMPDIR:-/tmp}/cost-onprem-chart.XXXXXX")"
  cleanup_chart_dir=1
  echo "Cloning ${CHART_REPO}@${CHART_REF} -> ${chart_dir}"
  git clone --depth 1 --branch "${CHART_REF}" "${CHART_REPO}" "${chart_dir}"
fi

cleanup() {
  if [[ -n "${cleanup_chart_dir}" ]]; then
    # Preserve reports for CI artifacts when present
    if [[ -d "${chart_dir}/tests/reports" && -n "${OPERATOR_GATE_REPORT_DIR:-}" ]]; then
      mkdir -p "${OPERATOR_GATE_REPORT_DIR}"
      cp -a "${chart_dir}/tests/reports/." "${OPERATOR_GATE_REPORT_DIR}/" || true
    fi
    rm -rf "${chart_dir}"
  fi
}
trap cleanup EXIT

export DEPLOYMENT_MODE="${DEPLOYMENT_MODE:-operator}"
export NAMESPACE="${NAMESPACE:-cost-tests}"
export HELM_RELEASE_NAME="${HELM_RELEASE_NAME:-cost-onprem}"
export KEYCLOAK_NAMESPACE="${KEYCLOAK_NAMESPACE:-keycloak}"
export KAFKA_NAMESPACE="${KAFKA_NAMESPACE:-kafka}"

cd "${chart_dir}"

if [[ "${COLLECT_ONLY}" == "1" ]]; then
  echo "Collect-only: validating @pytest.mark.operator selection"
  python3 -m venv .venv-operator-gate
  # shellcheck disable=SC1091
  source .venv-operator-gate/bin/activate
  pip install -q -r tests/requirements.txt
  cd tests
  # pytest.ini uses --strict-markers; operator must be registered on the chart ref
  out="$(pytest --collect-only -q -m 'operator')"
  echo "${out}"
  if ! echo "${out}" | grep -Eq '[1-9][0-9]*/[0-9]+ tests collected'; then
    echo "Expected to collect operator-marked tests; got:" >&2
    echo "${out}" >&2
    exit 1
  fi
  echo "Collect-only OK (operator root ${ROOT})"
  exit 0
fi

echo "Running operator acceptance gate against namespace=${NAMESPACE} release=${HELM_RELEASE_NAME}"
./scripts/run-pytest.sh --operator-gate
