# Chart operator acceptance gate (CI)

The Helm chart pytest suite is the runtime acceptance suite for
`CostManagementServiceConfig`. Tests that are known-green against the operator
carry `@pytest.mark.operator` on chart branch `feat/operator-test-compat`
(fork: `martinpovolny/cost-onprem-chart`).

## Local

Against an already-deployed operator stack:

```bash
export KUBECONFIG=...
export NAMESPACE=cost-tests HELM_RELEASE_NAME=cost-onprem
export KEYCLOAK_NAMESPACE=keycloak KAFKA_NAMESPACE=kafka
export DEPLOYMENT_MODE=operator

# Use a local chart checkout (recommended while iterating on markers):
CHART_DIR=/path/to/cost-onprem-chart ./hack/run-chart-operator-gate.sh

# Or clone the default fork branch:
./hack/run-chart-operator-gate.sh
```

Harness-only check (no cluster):

```bash
COLLECT_ONLY=1 ./hack/run-chart-operator-gate.sh
```

## GitHub Actions

Workflow: `.github/workflows/chart-operator-gate.yml`

| Job | When | What |
|-----|------|------|
| Collect | every PR/push to `main` | Clone chart ref, `pytest --collect-only -m operator` |
| E2E | `workflow_dispatch`, or when repo variable `OPERATOR_CHART_E2E_ENABLED=true` | Run `--operator-gate` with secret `OPENSHIFT_KUBECONFIG` |

Optional repo variables for e2e: `OPERATOR_CHART_NAMESPACE`,
`OPERATOR_CHART_RELEASE`, `OPERATOR_CHART_KEYCLOAK_NAMESPACE`,
`OPERATOR_CHART_KAFKA_NAMESPACE`, `OPERATOR_CHART_STORAGE_SECRET`.

Ephemeral OpenShift CI (OLM install + provision deps per PR) is tracked as
[COST-7699](https://redhat.atlassian.net/browse/COST-7699). This workflow is the
interim gate that reuses the chart suite.

## Growing the allowlist

When a new operator stage goes green on clusterbot/CRC, add
`@pytest.mark.operator` on the chart branch and bump `CHART_REF` if needed.
Do not mark flaky tests; keep exclusions documented in
`cost-onprem-chart/docs/development/operator-test-compat.md`.

## See also

See also: [operator-chart-testing.md](operator-chart-testing.md) for the full CRC/clusterbot runtime testing guide.
