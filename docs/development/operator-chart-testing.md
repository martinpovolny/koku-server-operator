# Operator runtime testing (chart pytest)

Until OpenShift CI / Prow lands ([COST-7699](https://redhat.atlassian.net/browse/COST-7699)),
runtime acceptance for `CostManagementServiceConfig` is the **Helm chart pytest
suite** run manually (or via optional GHA) against an operator-managed stack on
OpenShift (clusterbot / CRC).

The Kind Ginkgo job in `.github/workflows/test-e2e.yml` only covers manager
install/scaffold checks. It is **not** a substitute for this suite.

## Chart branch (test harness + allowlist)

| Item | Value |
|------|-------|
| Fork | [`martinpovolny/cost-onprem-chart`](https://github.com/martinpovolny/cost-onprem-chart) |
| Branch | [`feat/operator-test-compat`](https://github.com/martinpovolny/cost-onprem-chart/tree/feat/operator-test-compat) |
| Compat notes | [operator-test-compat.md](https://github.com/martinpovolny/cost-onprem-chart/blob/feat/operator-test-compat/docs/development/operator-test-compat.md) |

That branch adds:

- `DEPLOYMENT_MODE=operator` harness hooks in `tests/conftest.py` / `scripts/run-pytest.sh`
- `@pytest.mark.operator` allowlist (acceptance gate)
- `./scripts/run-pytest.sh --operator-gate`

Upstream chart (`insights-onprem/cost-onprem-chart`) does not yet carry these
markers; point CI and local runs at the fork branch until it merges.

## How to run

Prerequisites: operator deployed, BYOI deps (PostgreSQL, Valkey/Redis, Kafka /
AMQ Streams, S3 or MinIO), Keycloak/RHBK, and a Ready `CostManagementServiceConfig`
whose **name** matches `HELM_RELEASE_NAME` (resource names are `{cr.name}-*`).

```bash
# From cost-onprem-chart @ feat/operator-test-compat
export NAMESPACE=cost-tests
export HELM_RELEASE_NAME=cost-onprem
export KEYCLOAK_NAMESPACE=keycloak
export KAFKA_NAMESPACE=kafka
export DEPLOYMENT_MODE=operator
export STORAGE_SECRET_NAME=cost-onprem-storage-credentials

./scripts/run-pytest.sh --operator-gate
```

From this repo (clones the chart branch by default):

```bash
./hack/run-chart-operator-gate.sh
# or local checkout:
CHART_DIR=/path/to/cost-onprem-chart ./hack/run-chart-operator-gate.sh
```

See also [chart-operator-gate.md](chart-operator-gate.md) (GHA wiring) and
[crc-testing.md](crc-testing.md) (local CRC).

## Gate design

`@pytest.mark.operator` is an **allowlist**, not “everything that passed once”:

- Mark tests that are intentionally supported against the operator
- Omit known flakes and incomplete stages
- Grow the marker as stages go green; do not mark intermittent failures

Currently excluded from the gate (intentionally unmarked):

- `test_oauth_proxy_no_tls_errors` — UI oauth-proxy TLS noise
- `test_sources_endpoint_accessible_via_gateway` — intermittent Envoy 503 / connect timeout

## Status (2026-08-09, clusterbot)

Stack: operator image `quay.io/martin_povolny/koku-server-operator:dev`, CR
`cost-onprem` in `cost-tests`, BYOI in `cost-byoi-infra`, Kafka in `kafka`,
Keycloak in `keycloak`.

| Metric | Count |
|--------|------:|
| Chart suite total | 533 |
| Operator gate (`-m operator`) | 112 |
| Passed | 101 |
| Failed | 6 |
| Skipped | 5 |

### Failures (gate run)

| Test | Notes |
|------|-------|
| `TestGatewayJWTAuthentication::test_sources_api_accessible` | Gateway / sources path |
| `TestRBACGateway::test_gateway_openshift_costs_user_without_rbac_returns_403` | Expected 403 vs actual response |
| `TestRBACSecurityBoundaries::test_org_id_tenant_isolation_boundary_cases[overflow]` | Tenant isolation assert |
| `TestRBACSecurityBoundaries::test_org_id_tenant_isolation_boundary_cases[wrong-tenant]` | Tenant isolation assert |
| `TestKokuSourcesHealth::test_koku_sources_endpoint_responds` | Inter-pod curl timeout (60s) |
| `TestAuthenticationErrors::test_non_admin_source_creation` | Inter-pod curl timeout (60s) |

Sources timeouts match the known Envoy → koku connect-timeout flake under load
(first tenant schema clone / worker starvation). Re-run alone often passes.

### Skips (expected on BYOI)

- In-namespace DB pod checks (external PostgreSQL)
- Some celery-worker readiness / AWS-region defaults / IAM self-modify cases

## Related CI / Jira

| Work | Tracking |
|------|----------|
| Adapt chart pytest for operator | [COST-7697](https://redhat.atlassian.net/browse/COST-7697) |
| Operator-specific E2E scenarios | [COST-7698](https://redhat.atlassian.net/browse/COST-7698) |
| OLM bundle | [COST-7695](https://redhat.atlassian.net/browse/COST-7695) |
| Bundle / CatalogSource CI | [COST-7696](https://redhat.atlassian.net/browse/COST-7696) |
| OpenShift CI (Prow, S4 + RHBK + AMQ Streams, OLM install, pytest) | [COST-7699](https://redhat.atlassian.net/browse/COST-7699) |

Interim GitHub Actions (collect-only + optional kubeconfig e2e):
[chart-operator-gate workflow](../../.github/workflows/chart-operator-gate.yml)
(see fork PR for `ci/chart-operator-gate` when open).

Upstream stack PR (Kind e2e + full operator):
[project-koku/koku-service-operator#10](https://github.com/project-koku/koku-service-operator/pull/10).

## Updating this doc

After a meaningful gate run on clusterbot/CRC, update the **Status** table and
failure list. When markers change on `feat/operator-test-compat`, note the
chart commit SHA in the Status section.
