# Deep Code Review — 2026-08-10

Parallel follow-up review of `koku-service-operator` on `main` at `df36953`.
Three independent audits covered controller lifecycle, Kubernetes resource
construction/security, and API/delivery behavior. This report contains only
new findings beyond `docs/code-review-2026-08-10.md`.

## Fix status (as of 2026-08-10)

| ID | Status | Notes |
|----|--------|-------|
| D1 | ❌ OPEN | Readiness gate on all workloads |
| D2 | ❌ OPEN | Status hot loop / generation predicate |
| D3 | ❌ OPEN | AuthenticationReady overwritten by edge stage |
| D4 | ❌ OPEN | Migration fingerprint incomplete |
| D5 | ✅ FIXED | Multi-broker Kafka host parsing |
| D6 | ✅ FIXED | ROS API NetworkPolicy added |
| D7 | ✅ FIXED | Kruize secrets access removed from ClusterRole |
| D8 | ✅ FIXED | CronJob deleted when feature is disabled |
| D9 | ✅ FIXED | External DB secret not silently created |
| D10 | ❌ OPEN | Sample kustomization duplicate GVK |
| D11 | ✅ FIXED | Keycloak CA Secret mounted into Envoy |
| D12 | ❌ OPEN | ServiceAccount create=false ignored |
| D13 | ❌ OPEN | API controls (profile=ha, enabled fields) no effect |
| D14 | ❌ OPEN | Rollout readiness ignores observedGeneration |
| D15 | ✅ FIXED | Kruize credentials added to DB validation |
| D16 | ❌ OPEN | imagePullSecrets not propagated |
| D17 | ⚠️ PARTIAL | ServiceMonitor labels fixed; port names/Listener Service open |
| D18 | ✅ FIXED | RBAC cache port uses spec.cache.port |
| D19 | ❌ OPEN | Route TLS passthrough/reencrypt accepted but broken |
| D20 | ✅ FIXED | Envoy config hash annotation triggers pod restart |
| D21 | ❌ OPEN | StorageClass discovery blocks pure BYOI |
| D22 | ❌ OPEN | Long CR names produce invalid child resources |

## Executive summary

The deeper pass found 22 additional issues. The most urgent are:

- Ready status ignores most application workloads.
- Successful reconciliation can create a status-driven hot loop.
- Migration completion is cached with an incomplete identity.
- ROS can be reached without passing through the JWT gateway.
- Kruize can read every Secret in the cluster.
- Standard multi-broker Kafka configuration is rendered incorrectly.
- Disabling destructive cleanup does not stop existing CronJobs.
- The sample kustomization breaks bundle generation.

## P1 findings

### D1 — Ready ignores most workload health

**Locations:** `internal/controller/costmanagementserviceconfig_controller.go:379-438`,
`:445-489`, `:571-612`, and `:159-168`.

Only the bundled infrastructure, Koku API, and Envoy Deployment are gated on
readiness. Masu, Listener, Celery, ROS, RBAC, Kruize, and Ingress are applied
without readiness checks. `UIReady=True` means only that the OAuth client
Secret is valid; it is set before the UI Deployment is applied and that
Deployment is never checked.

A bad ROS, RBAC, Ingress, or UI image can therefore leave a critical component
at zero available replicas while the CR reports `Available=True`,
`Phase=Ready`, and `AllComponentsReady`.

**Recommendation:** gate every critical enabled Deployment on its current
rollout, and report per-component readiness conditions.

### D2 — Progressing status can cause a continuous reconcile loop

**Locations:** `internal/controller/costmanagementserviceconfig_controller.go:133-168`,
`:779-780`, and `:797-810`.

Each successful pass changes `Progressing` from false to true and back to false.
Those transitions refresh `LastTransitionTime`, so the final status differs
from the status read at the start. The controller patches status and watches
the primary CR without a generation-change predicate. That status update can
immediately enqueue another full reconciliation, which repeats the transition.

This undermines the intended five-minute drift interval and can continuously
reapply every resource, write status, and emit the duplicate events described
in the original review.

**Recommendation:** do not publish a transient true/false cycle within one
pass. Preserve an unchanged final condition when no external state changed,
and filter primary-resource updates with an appropriate predicate while still
allowing relevant deletion/status recovery events.

### D3 — A failed OIDC check is overwritten with success

**Locations:** `internal/controller/validation.go:117-133` and
`internal/controller/costmanagementserviceconfig_controller.go:518-535`.

Validation sets `AuthenticationReady=False` when the JWKS endpoint is
unreachable but intentionally continues. Edge reconciliation later sets the
same condition to true solely because the Envoy Deployment and Route exist.
A dead Keycloak can therefore finish the same reconcile pass as
`AuthenticationReady=True` and overall Ready.

This is separate from the original finding that the probe accepts HTTP 4xx.

**Recommendation:** use separate conditions for OIDC reachability and gateway
rollout, or compute `AuthenticationReady` from both signals without allowing a
later stage to erase a failure.

### D4 — Completed migrations are reused for a different database

**Locations:** `internal/controller/costmanagementserviceconfig_controller.go:276-298`
and `:349-359`; `internal/resources/migration.go:473-491`.

Migration identity consists only of an image tag annotation. Changing an
external database host or credential Secret while retaining the image tag
causes all completed migrations to be reused. Applications then start against
the new, potentially empty database without running schema migrations.

Changing an image repository while retaining a tag is also ignored. Admin
bootstrap input changes have the same incomplete-cache problem.

**Recommendation:** annotate Jobs with a deterministic fingerprint of the full
desired migration input: complete image reference, database target and Secret
identity, migration command, and bootstrap inputs.

### D5 — Multi-broker Kafka configuration produces an invalid host — ✅ FIXED — `firstBroker()` parses first entry before extracting host:port

**Locations:** `internal/resources/names.go:123-143`,
`internal/resources/ros.go:69-86` and `:161-173`,
`internal/resources/env.go:41-42`, and `internal/resources/kruize.go:106-124`.

For a normal HA bootstrap list such as:

```text
broker-a:9092,broker-b:9092
```

`KafkaHost` searches for the last colon in the entire string and returns
`broker-a:9092,broker-b`. ROS then waits forever for that invalid hostname.
Koku and generated cdapp configuration receive the same malformed split.
Validation hides the inconsistency because `kafkaTCPProbe` correctly parses
the comma-separated list.

**Recommendation:** parse the broker list first, use `net.SplitHostPort` for
individual endpoints, and pass the complete bootstrap list to clients that
support it.

### D6 — ROS API bypasses the JWT gateway — ✅ FIXED — `ROSAPINetworkPolicy` added, allows only gateway pods on port 8000

**Locations:** `internal/resources/ros.go:242-335`,
`internal/controller/costmanagementserviceconfig_controller.go:541-552`, and
`internal/resources/networkpolicies.go`.

Envoy is the intended JWT enforcement point, but the ROS Service is directly
reachable from other pods and no ROS NetworkPolicy is created. Any workload
with namespace/cluster network access can bypass Envoy and call ROS on port
8000 directly, outside the gateway's JWT validation and identity-injection
path.

**Recommendation:** add a ROS policy permitting API traffic only from gateway
pods and metrics traffic only from the intended monitoring namespaces.

### D7 — Kruize can read every Secret in the cluster — ✅ FIXED — `secrets` removed from Kruize ClusterRole

**Location:** `internal/resources/kruize.go:49-84`.

The Kruize ClusterRole grants `get`, `list`, and `watch` on `secrets` and is
bound cluster-wide to the ServiceAccount used by the Kruize pod. A compromise
of Kruize or one of its dependencies therefore exposes credentials from every
namespace.

**Recommendation:** remove Secret access unless a concrete runtime requirement
can be demonstrated, and reduce other permissions to namespaced Roles or the
narrowest resource set possible.

### D8 — Disabling cleanup does not stop existing destructive CronJobs — ✅ FIXED — `else` branch deletes CronJob when disabled

**Locations:** `api/v1alpha1/costmanagementserviceconfig_types.go:352-360` and
`:402-407`; `internal/controller/costmanagementserviceconfig_controller.go:465-473`.

When either partition-cleaner flag changes from true to false, reconciliation
merely omits the CronJob from the apply list. It never deletes the previously
created object, so the CronJob continues deleting partitions despite the
requested disablement.

The same stale-resource pattern affects monitoring and bundled infrastructure,
but cleanup Jobs have the clearest data-lifecycle risk.

**Recommendation:** explicitly delete owned optional resources when their
desired state becomes disabled, and test true-to-false transitions.

### D9 — A missing external database Secret is silently generated — ✅ FIXED — `ensureSecret` skipped when `database.secretName` is set

**Locations:** `internal/controller/costmanagementserviceconfig_controller.go:175-179`,
`internal/resources/names.go:34-39`, and `internal/resources/secrets.go:19-42`.

`spec.database.secretName` is documented as the name of an existing Secret.
Nevertheless, shared reconciliation always calls `ensureSecret`; when the
named external Secret is absent, the operator creates it with random
credentials. Key validation then succeeds even though those credentials do
not match the external database, moving the failure to migration authentication
and obscuring the actual configuration error.

**Recommendation:** generate credentials only when no external Secret name is
provided. An explicit missing reference should produce a clear blocking
condition.

### D10 — Sample kustomization breaks bundle generation

**Locations:** `config/samples/kustomization.yaml:1-4`,
`config/manifests/kustomization.yaml`, and the `bundle` target in `Makefile`.

Both sample YAML files define the same GVK, name, and namespace, and both are
listed by one kustomization. This is reproducible with:

```text
kubectl kustomize config/samples
```

which fails with `may not add resource with an already registered id`. The
bundle target includes the samples package, so bundle generation is also
blocked. CI does not exercise bundle generation.

**Recommendation:** provide separate sample overlays or give the examples
distinct identities, and add kustomize/bundle validation to CI.

## P2 findings

### D11 — Keycloak TLS settings do not apply to Envoy — ✅ FIXED — `caCertSecretName` Secret mounted into Envoy via `envoyVolumes()`

**Locations:** `api/v1alpha1/costmanagementserviceconfig_types.go:262-270`,
`internal/resources/envoy.go:169-288`, and `internal/resources/ui.go:396-423`.

The API describes `caCertSecretName` and `insecureSkipVerify` as Keycloak TLS
controls. The custom CA is mounted only into the UI oauth-proxy. Envoy's JWKS
client always uses the combined system/OpenShift service CA with strict SAN
verification; neither option changes its behavior.

A private or self-signed Keycloak can therefore work with the UI while Envoy
cannot fetch signing keys and all API authentication fails.

**Recommendation:** mount/merge the selected CA into Envoy and render the
requested verification mode, or narrow the API documentation and supported
configuration.

### D12 — ServiceAccount `create=false` is ignored and external SAs are adopted

**Locations:** `api/v1alpha1/costmanagementserviceconfig_types.go:59-63`,
`internal/controller/costmanagementserviceconfig_controller.go:201-203`,
`:412-420`, and `:657-723`.

Koku and ROS ServiceAccounts are always force-applied with a controller owner
reference. If a user specifies an existing ServiceAccount with `create:false`,
the operator mutates and adopts it. Deleting the CR can then garbage-collect a
ServiceAccount owned by the user and potentially used by other workloads.

**Recommendation:** when `create=false`, validate and reference the existing
object without applying it or assigning ownership.

### D13 — Several advertised API controls have no effect

**Locations:** `api/v1alpha1/costmanagementserviceconfig_types.go:36-45`,
`:436-465`, and `:566-583`; `internal/resources/koku.go:16-118`.

- `profile=ha` is never read and changes no replica or resource settings.
- Koku API, Masu, and Listener `enabled` fields are never read.
- Zero replicas for those components are converted back to one or two.

Users can submit an accepted CR that claims HA sizing or disables a component,
while the resulting deployment remains unchanged.

**Recommendation:** implement these contracts or remove them before the API is
treated as stable. Add desired-state transition tests for every enable/disable
field.

### D14 — Rollout readiness accepts stale replicas

**Locations:** `internal/controller/costmanagementserviceconfig_controller.go:729-754`.

Deployment readiness checks only `AvailableReplicas >= spec.replicas`, and the
StatefulSet check only compares `ReadyReplicas`. Neither verifies
`status.observedGeneration` or rollout revisions. Immediately after applying a
new image or pod template, old replicas can satisfy the gate and the CR can
become Ready before a new pod exists. Database migrations may similarly begin
while a database rollout is still pending.

**Recommendation:** require observed generation and the controller-specific
rollout invariants (`UpdatedReplicas`, unavailable replicas, and StatefulSet
revision equality).

### D15 — External DB validation omits Kruize credentials — ✅ FIXED — `kruize-user`/`kruize-password` added to required key list

**Locations:** `api/v1alpha1/costmanagementserviceconfig_types.go:115-119`,
`internal/controller/validation.go:45-52`, and
`internal/resources/kruize.go:138-169`.

The API documents `kruize-user` and `kruize-password` as required, and Kruize
consumes both, but validation does not check them. The database condition can
become Ready and migrations can succeed before Kruize fails to start.

### D16 — Image pull configuration is ignored

**Locations:** `api/v1alpha1/costmanagementserviceconfig_types.go:52-57` and
`:84-93`; `internal/resources/database.go:202-208`.

`global.imagePullSecrets` has no production consumer. Per-image `pullPolicy`
fields are also ignored because every container uses only the global helper.
Private-registry deployments fail despite configured pull secrets, and a
component-specific `Always` policy silently becomes the global/default value.

**Recommendation:** propagate pull secrets to every PodSpec and resolve pull
policy with component-specific precedence over the global default.

### D17 — Generated ServiceMonitors cannot scrape most targets

**Locations:** `internal/resources/monitoring.go:15-65`,
`internal/resources/koku.go:48-93`, `internal/resources/kruize.go:281-290`,
and `internal/resources/networkpolicies.go`.

The ServiceMonitors request a Service port named `metrics`, but Koku, Masu, and
Kruize expose differently named ports, Listener has no Service, and Ingress's
NetworkPolicy does not permit Prometheus to reach its metrics port. Only ROS is
reliably selectable and reachable. Alerts based on an absent `up` series also
remain silent rather than firing.

**Recommendation:** expose consistently named metrics Service ports, create the
missing Listener Service, permit monitoring traffic, and test selectors against
rendered Services and NetworkPolicies.

### D18 — RBAC ignores the configured external cache port — ✅ FIXED — `cachePortStr()` helper used instead of hardcoded 6379

**Location:** `internal/resources/rbac.go:17-39`.

External-cache validation and init containers honor `spec.cache.port`, but RBAC
always receives `REDIS_PORT=6379`. An external cache on another port passes
validation and the TCP init check, after which RBAC API and worker connect to
the wrong port.

### D19 — Accepted Route TLS modes cannot reach the plaintext gateway

**Locations:** `api/v1alpha1/costmanagementserviceconfig_types.go:544-550`,
`internal/resources/route.go:49-78`, and `internal/resources/envoy.go:118-185`.

The CRD accepts `edge`, `passthrough`, and `reencrypt`, but Envoy exposes only a
plaintext HTTP listener. Passthrough forwards TLS bytes to that listener, while
reencrypt expects a TLS backend. Both accepted settings make the API Route
unusable.

**Recommendation:** restrict validation to `edge` until a TLS backend listener
and destination CA handling exist.

### D20 — Envoy authentication configuration changes do not roll out pods — ✅ FIXED — `envoy-config-hash` annotation added to pod template

**Locations:** `internal/resources/envoy.go:103-113`, `:137-253`, and `:255-305`.

Changing Keycloak URL, issuer, realm, or audiences updates the ConfigMap, but
the Envoy pod template contains only the constant ConfigMap name and no content
checksum. Envoy reads static configuration at process start and no reload
mechanism is configured. Existing gateways can therefore validate the old
issuer/audiences indefinitely.

**Recommendation:** add a deterministic ConfigMap-content hash to the pod
template or implement a supported dynamic reload path.

### D21 — BYOI deployments unnecessarily require a default StorageClass

**Location:** `internal/controller/discovery.go:38-46` and `:83-90`.

Discovery blocks when no default StorageClass exists even if both database and
cache are external and the operator will create no PVC. This contradicts the
production BYOI target and forces users to provide an irrelevant override.

**Recommendation:** require StorageClass discovery only when an enabled bundled
component needs persistent storage.

### D22 — Long valid CR names make generated resources invalid

**Locations:** `internal/resources/labels.go:18-35` and
`internal/resources/names.go:19-99`.

Kubernetes permits CR names longer than the 63-character label-value and child
name limits used here. The builders copy the full CR name into labels and append
suffixes without truncation or hashing. Such a CR is admitted but every child
creation fails validation.

**Recommendation:** constrain CR names at admission or introduce centralized,
collision-resistant name and label truncation.

## Test gaps that allowed these issues

- The envtest suite starts an API server but has no full reconciliation specs.
- The e2e suite remains scaffold-level and never creates a
  `CostManagementServiceConfig`.
- CI does not run e2e, bundle generation, or kustomize validation.
- Most resource tests inspect isolated object fragments rather than lifecycle
  transitions such as enable-to-disable, rollout, failure, and deletion.

## Recommended implementation order (remaining open items)

Items D5–D9, D11, D15, D18, D20 have been fixed. Remaining:

1. **D2** — Stop the status hot loop (generation predicate on primary-resource watch).
2. **D1** — Make Ready reflect all critical workload readiness conditions.
3. **D3** — Prevent edge stage from overwriting a failed OIDC condition.
4. **D4** — Correct migration identity (DB host + credential fingerprint).
5. **D10** — Repair sample kustomization duplicate GVK; add bundle CI.
6. **D13** — Implement or remove nonfunctional API controls (profile=ha, enabled fields).
7. **D14** — Require observedGeneration in rollout readiness checks.
8. **D12, D16, D17, D19, D21, D22** — Remaining correctness and API gaps.
