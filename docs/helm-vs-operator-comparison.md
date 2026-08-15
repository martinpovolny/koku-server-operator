# Helm Chart vs Operator Comparison Report

Updated: 2026-08-15 (validated against `gold` branch)

Systematic comparison of `cost-onprem-chart/cost-onprem/` (Helm chart) against
`koku-service-operator` (operator) — identifying deviations, missing pieces,
and bugs.

Both projects deploy the same Cost Management on-premise stack: Koku API,
Masu, Listener, Celery workers, RBAC, ROS, Kruize, Ingress, Envoy gateway,
and UI. The Helm chart uses `values.yaml` + Go templates; the operator uses
a `CostManagementServiceConfig` CR + Go reconciler.

---

## 1. Resource Inventory

| Component | Helm Chart | Operator | Status |
|-----------|-----------|----------|--------|
| PostgreSQL StatefulSet + Service | yes | yes | match |
| Valkey Deployment + PVC + Service | yes | yes | match |
| DB Credentials Secret | install script | auto-generated | operator better |
| Django Secret | install script | auto-generated | operator better |
| Storage Credentials Secret | install script | auto-generated (placeholder) | operator better |
| DB Init ConfigMap | yes | yes | match |
| AWS Config ConfigMap | yes | yes | match |
| CA Combine ConfigMap | yes | yes | match |
| Service CA ConfigMap | yes | yes | match |
| Koku API Deployment + Service | yes | yes | match |
| Masu Deployment + Service | yes | yes | **port mismatch** |
| Listener Deployment | yes | yes | match |
| Koku ServiceAccount | yes | yes | match |
| Koku Migration Job | yes | yes | match |
| Celery Beat Deployment | yes | yes | **no resources** |
| Celery Workers (10 queues) | yes | yes | match |
| RBAC API Deployment + Service | yes | yes | match |
| RBAC Worker Deployment | yes | yes | match |
| RBAC Migration Job | yes | yes | operator has richer seeding |
| RBAC Admin Bootstrap Job | yes | yes | match |
| RBAC Keycloak Sync CronJob + ConfigMap | yes | yes | match |
| RBAC NetworkPolicy | yes | yes | match |
| ROS API Deployment + Service | yes | yes | match |
| ROS Processor Deployment | yes | yes | match |
| ROS Processor Service | yes | **MISSING** | **gap** |
| ROS Recommendation Poller Deployment | yes | yes | match |
| ROS Recommendation Poller Service | yes | **MISSING** | **gap** |
| ROS Housekeeper Deployment | yes | yes | match |
| ROS Migration Job | yes | yes | match |
| ROS Partition Cleaner CronJob | yes | yes | match |
| ROS ServiceAccount | yes | yes | match |
| ROS API NetworkPolicy (access) | yes | yes | match |
| ROS/Koku metrics NetworkPolicies (4) | yes | **MISSING** | **gap** |
| Cdapp ConfigMap (ROS/Kruize) | yes | yes | match |
| Kruize Deployment + Service | yes | yes | match |
| Kruize ServiceAccount | yes | yes | match |
| Kruize ClusterRole + ClusterRoleBinding | yes | yes | match |
| Kruize ConfigMap | yes | yes | match |
| Kruize Partition CronJob | yes | yes | match |
| Kruize NetworkPolicy | yes | yes | match |
| Ingress Deployment + Service | yes | yes | match |
| Ingress NetworkPolicy | yes | yes | match |
| Envoy Gateway Deployment + Service | yes | yes | match |
| Envoy ConfigMap | yes | yes | routing differences |
| Gateway CA ConfigMap | yes | handled via combined CA | equivalent |
| Gateway NetworkPolicy | yes | yes | match |
| Gateway Route | yes | yes | path difference |
| UI Deployment (oauth-proxy + nginx) | yes | yes | match |
| UI Service | yes | yes | match |
| UI nginx ConfigMap | yes | yes | match |
| UI Route | yes | yes | match |
| UI ConsoleLink | yes | yes | match |
| Koku ServiceMonitor | yes | yes | match |
| Kruize ServiceMonitor | yes | yes | match |
| RBAC ServiceMonitor | yes | **MISSING** | **gap** |
| Gateway ServiceMonitor | yes | **MISSING** | **gap** |
| ROS Processor/Poller ServiceMonitors | yes | **MISSING** | **gap** |
| **PrometheusRules (5 alert rules)** | **no** | **yes** | **operator better** |
| Koku API NetworkPolicy | no | yes | operator better |
| Cache NetworkPolicy | no | yes | operator better |
| Database NetworkPolicy | no | yes | operator better |
| Keycloak Debug ConfigMap | yes | no | not needed |

---

## 2. Open Issues (Broken / Wrong)

### 2.1 Celery beat has zero resource limits

**Severity: MEDIUM — unbounded resource consumption**

`CeleryBeatDeployment()` in `koku.go` passes `corev1.ResourceRequirements{}`
(empty). The chart sets requests `{cpu: 50m, mem: 200Mi}` and limits
`{cpu: 100m, mem: 400Mi}`.

### 2.2 Masu Service port mismatch

**Severity: MEDIUM — metrics scraping may break**

Operator `MasuService()` exposes port 9000 (the metrics port). The Helm
chart's Masu service exposes port 8000 (the Gunicorn HTTP port). This should
be a two-port service (8000 for http, 9000 for metrics) or at minimum match
the chart's port 8000.

---

## 3. Missing Resources (Gaps)

### 3.1 ROS Processor and Recommendation Poller Services

The chart creates Service objects for both with a metrics port (9000),
enabling Prometheus scraping. The operator creates Deployments but no
Services.

### 3.2 Metrics-scraping NetworkPolicies

The chart has 4 NetworkPolicies allowing OpenShift Prometheus (via
`openshift-monitoring` namespace selector) to reach metrics ports on
ros-api, cost-api, ros-processor, and ros-recommendation-poller. The
operator has none of these. In a default-deny environment, Prometheus
can't scrape any application metrics.

The ROS API access NetworkPolicy was added (restricts traffic to gateway
only) but these metrics-scraping policies are still missing.

### 3.3 ServiceMonitor gaps

The chart creates per-component ServiceMonitors (one each for ros-api,
ros-processor, ros-recommendation-poller, kruize, cost-management-api,
and gateway). Each targets a specific component label and metrics port.

The operator's `AppServiceMonitor` does not include `rbac-api`,
`ros-processor`, `ros-recommendation-poller`, or `gateway` (Envoy admin
`/stats/prometheus` endpoint). Additionally, the operator selects port
`"metrics"` but most Services expose port `"http"` — see
[code-review-fixmes.md](code-review-fixmes.md) #10.

---

## 4. Env Var Differences

### 4.1 Koku env var gaps

The Helm chart sets these env vars with defaults. The operator does NOT set
them (users can provide them via `spec.costManagement.api.env`):

| Env Var | Chart Default | Operator | Impact |
|---------|---------------|----------|--------|
| `ENHANCED_ORG_ADMIN` | `"False"` | not set | **critical for RBAC scoping** |
| `DEVELOPMENT` | `"False"` | not set | koku defaults to "True" in dev? |
| `KOKU_ENABLE_SENTRY` | `"False"` | not set | Sentry SDK may try to phone home |
| `S3_VERIFY_SSL` | `"false"` | not set | may fail on self-signed S3 |
| `INITIAL_INGEST_NUM_MONTHS` | `"2"` | not set | may over-ingest |
| `INITIAL_INGEST_OVERRIDE` | `"False"` | not set | probably fine |
| `CACHED_VIEWS_DISABLED` | `"False"` | not set | probably fine (app default) |
| `NOTIFICATION_CHECK_TIME` | `"24"` | not set | probably fine |
| `RBAC_CACHE_TIMEOUT` | `"300"` | not set | probably fine |
| `CACHE_TIMEOUT` | `"3600"` | not set | probably fine |
| `TAG_ENABLED_LIMIT` | `"200"` | not set | probably fine |
| `USE_READREPLICA` | `"False"` | not set | probably fine |

Previously missing `RETAIN_NUM_MONTHS` is now set via a dedicated CR field
(default `4`; chart used `3`).

`ENHANCED_ORG_ADMIN` is particularly important: when True, Koku treats all
org_admin users as having full access without checking RBAC. The chart's
keycloakSync template validates this at render time. The operator should
hardcode this to `"False"`.

### 4.2 Logging env vars partially set

`KOKU_LOG_LEVEL`, `DJANGO_LOG_LEVEL`, and `DJANGO_LOG_FORMATTER` are set
in RBAC and migration containers but not in `KokuCommonEnv` — Koku API,
Masu, and workers miss them. `GUNICORN_LOG_LEVEL` is not set anywhere.

### 4.3 Missing `POLLING_TIMER` env var

The chart sets `POLLING_TIMER` (default: 86400 = 24h). The operator does
not set this.

### 4.4 RBAC env vars: `ROLE_CREATE_ALLOW_LIST`

The chart exposes `rbac.roleCreateAllowList`. The operator doesn't expose
or set this.

---

## 5. Good Deviations (Operator is Better)

### 5.1 PrometheusRules with 5 alert rules

The operator creates a `PrometheusRule` with:
- `CostManagementMigrationFailed` — critical, fires on failed migration jobs
- `CostManagementDegraded` — critical, fires when Degraded condition is true for 5m
- `CostManagementSchemaOutOfDate` — warning, fires when SchemaUpToDate is false for 15m
- `CostManagementAPIDown` — critical, fires when koku-api metrics are unreachable for 5m
- `CostManagementNotProgressing` — warning, fires when Available is false for 30m

The chart has none of these.

### 5.2 Auto-discovery phase

The operator auto-detects cluster domain, default StorageClass, and S3
endpoint + credentials (from OBC/NooBaa). The chart relies on the install
script passing these as `--set` overrides.

### 5.3 Phased reconciliation with readiness gates

The operator won't deploy services until the database is ready, won't
start workers until the API is healthy. The chart deploys everything at
once and relies on init containers for ordering.

### 5.4 Comprehensive RBAC migration + seeding Job

The operator's RBAC migration Job does Django migrations, built-in seeds,
Cost Management permission/role seeding, admin_default group creation,
bootstrap_tenants, and platform_default cleanup — a superset of the chart.

### 5.5 Image-tag-based migration re-run

The operator annotates migration Jobs with the image tag and re-creates
on image change for automatic upgrade migrations. The chart relies on
Helm pre-upgrade hooks.

### 5.6 Drift correction every 5 minutes

The operator re-applies desired state on a 5-minute interval, reverting
manual edits. Helm only applies on install/upgrade.

### 5.7 Auto-generated secrets with secure random passwords

The operator generates DB credentials, Django secret key, and storage
credentials with 32-character random passwords. The chart relies on the
install script.

### 5.8 Additional NetworkPolicies

The operator creates Cache and Database NetworkPolicies not present in
the chart, restricting access to the bundled infrastructure.

---

## 6. Neutral Deviations

### 6.1 Envoy routing config: largely equivalent

Both define the same 5 Envoy route entries with matching timeouts.

| Route prefix | Cluster | Chart timeout | Operator timeout |
|---|---|---|---|
| `/api/cost-management/v1/recommendations/openshift` | ros-api-backend | 30s | 30s |
| `/api/rbac/` | rbac-api-backend | 30s | 30s |
| `/api/cost-management/` | koku-api-backend | 60s | 60s |
| `/api/ingress/ready` | ingress-backend | 10s | 10s |
| `/api/ingress/` | ingress-backend | 180s | 180s |

### 6.2 Route path: `/api` vs `/`

Operator creates the gateway Route with `spec.path: /api`. The chart
creates a broader Route at `/`. Correct — the gateway only handles API
traffic; the UI has its own Route.

### 6.3 Two separate Routes (API + UI) vs one

Both create two Routes: one for the API gateway (edge TLS) and one for
the UI (passthrough TLS to oauth2-proxy). Architecturally sound.

### 6.4 Labels

Operator uses `app.kubernetes.io/{name,instance,component,managed-by}`.
Chart uses Helm-standard labels. Both are valid.

### 6.5 Gateway Route timeout annotation not set by default

The operator doesn't set `haproxy.router.openshift.io/timeout` on the
gateway Route (user can set it via `spec.gatewayRoute.annotations`). The
chart sets `180s`. The Envoy-side timeout is now correct (180s), but the
OpenShift Route annotation default should also be set for consistency.

---

## 7. Remaining Fixes (Priority Order)

1. **Set `ENHANCED_ORG_ADMIN=False`** in `KokuCommonEnv()` — critical for RBAC scoping
2. **Add Celery beat resources** (`koku.go`): set `{cpu: 50m, mem: 200Mi}` / `{cpu: 100m, mem: 400Mi}`
3. **Fix Masu Service port** (`koku.go`): expose port 8000 (http) + 9000 (metrics)
4. **Add ROS Processor + Poller Services**: needed for Prometheus metrics scraping
5. **Add metrics-scraping NetworkPolicies**: allow Prometheus to reach metrics ports
6. **Add RBAC + Gateway + ROS components to ServiceMonitors**: close monitoring gaps
7. **Set default Route timeout annotation** to 180s in GatewayAPIRoute
8. **Add remaining env var defaults**: `S3_VERIFY_SSL`, `INITIAL_INGEST_NUM_MONTHS`, logging vars
9. **Expose `roleCreateAllowList`** in the RBAC CR section
