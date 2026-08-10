# Code Review — 2026-08-10

Full-repository review of `koku-service-operator` at commit `15c7b1d` by Codex.

## Executive summary

Six actionable issues were found. **All six have been fixed** (commits on `main`):

| ID | Priority | Area | Status |
|---|---|---|---|
| F1 | P1 | Cluster-scoped ownership | ✅ Fixed — ClusterRole name includes `sha256(namespace)[:4]` |
| F2 | P1 | Status/error handling | ✅ Fixed — any phase error now sets `Degraded=True` |
| F3 | P2 | Object storage validation | ✅ Fixed — `checkSecretKeys` called in validation stage |
| F4 | P2 | OIDC validation | ✅ Fixed — probe fails on any non-2xx response |
| F5 | P2 | Monitoring reconciliation | ✅ Fixed — non-CRD errors are returned, not swallowed |
| F6 | P2 | Event handling | ✅ Fixed — `priorPhase` captured before overwrite |

F1 and F2 should be addressed before supporting more than one custom resource
or relying on status conditions for automation. F3 and F4 directly affect
whether a nominally Ready installation is usable.

## Findings

### F1 — Cluster-scoped resources collide across namespaces (P1) ✅ FIXED

**Locations**

- `internal/resources/kruize.go:27-34`
- `internal/resources/ui.go:58-61`
- `internal/controller/costmanagementserviceconfig_controller.go:97-121`
- `internal/controller/costmanagementserviceconfig_controller.go:651-654`

The ClusterRole, ClusterRoleBinding, and ConsoleLink names are based only on
`cfg.Name`. The CR is namespaced, so two valid installations may have the same
CR name in different namespaces. Both installations then target identical
cluster-scoped object names.

For example, these resources conflict:

```text
namespace team-a / CostManagementServiceConfig cost-management
namespace team-b / CostManagementServiceConfig cost-management

Both produce ClusterRoleBinding cost-management-kruize.
```

The binding contains a namespace-specific ServiceAccount subject. Because the
operator applies cluster-scoped resources using server-side apply with
`ForceOwnership`, the last reconciler to run can replace the subject namespace.
Kruize in the other installation then loses its intended binding.

Deletion is more dangerous: each CR finalizer unconditionally deletes the
shared object names. Deleting either CR can therefore remove resources still
needed by the other CR.

**Recommendation**

1. Include the namespace in every cluster-scoped resource name, with safe
   truncation and a hash where necessary to remain within Kubernetes limits.
2. Add ownership labels or annotations containing CR namespace, name, and UID.
3. Before deleting a cluster-scoped object, verify that its ownership metadata
   matches the deleting CR.
4. Add a test reconciling two same-named CRs in different namespaces and assert
   distinct object names, subjects, and deletion behavior.

### F2 — Ordinary reconciliation errors do not set degraded status (P1) ✅ FIXED

**Locations**

- `internal/controller/costmanagementserviceconfig_controller.go:133-153`
- `internal/controller/phases.go:47-69`

At the start of every pass, reconciliation sets:

```text
status.phase = Progressing
Progressing = True
```

When a phase returns an error, `applyPhaseError` updates status only if the
error contains a `PhaseError`. Although `NewPhaseError` exists, no production
call site uses it. Phase implementations return ordinary errors created with
`fmt.Errorf`, so `applyPhaseError` normally does nothing.

This produces misleading observable state after failures:

- `Phase` remains `Progressing` indefinitely.
- `Progressing=True` remains set even when reconciliation cannot progress.
- `Degraded=True` is not set.
- A prior generation's `Available=True` condition may remain present.
- `ObservedGeneration` is advanced before the desired generation succeeds,
  making the stale availability condition especially easy for automation to
  misinterpret.

This undermines the documented contract that conditions are the primary API.

**Recommendation**

Either require every phase to return a structured error or implement a generic
fallback in the top-level reconcile loop. On an unclassified error, the
controller should at minimum set `Degraded=True` and `Progressing=False`, with
the current generation and a useful reason/message. The desired semantics of
`Available` should be explicit: retain it only if the previously deployed
generation is genuinely still serving, otherwise set it false.

Add a table-driven test for errors from discovery, apply, readiness lookup,
migration, and monitoring. Assert the complete top-level condition set rather
than only `status.phase`.

### F3 — User-provided S3 credentials are trusted without validation (P2) ✅ FIXED

**Locations**

- `internal/controller/discovery_s3.go:50-52`
- `internal/controller/costmanagementserviceconfig_controller.go:175-190`
- `internal/resources/env.go` (`AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`)
- `internal/resources/ingress.go` (`INGRESS_MINIOACCESSKEY` and `INGRESS_MINIOSECRETKEY`)

When `spec.objectStorage.secretName` is non-empty, discovery immediately returns
a user-provided S3 configuration. It does not retrieve the Secret or verify the
documented `access-key` and `secret-key` entries. Shared configuration also
skips creation of the generated storage Secret in this case.

The application environment uses optional Secret key selectors for these
credentials. A missing Secret or missing keys therefore does not necessarily
prevent pod startup. Discovery records `StorageReady=True`, and the complete
pipeline can eventually report `Available=True` even though uploads and
S3-backed operations cannot authenticate.

**Recommendation**

Validate the referenced Secret during the validation phase using the existing
`checkSecretKeys` helper. A missing or incomplete explicitly configured Secret
should set `StorageReady=False` and block final readiness. Optional selectors
are appropriate for automatically discovered anonymous/no-credential cases,
but not for an explicit credential reference.

Tests should cover a missing Secret, each missing key, empty values, and a valid
Secret.

### F4 — OIDC/JWKS validation accepts HTTP error responses (P2) ✅ FIXED

**Location**

- `internal/controller/validation.go:167-197`

`httpProbe` considers every response below HTTP 500 successful. For a JWKS
endpoint this accepts responses such as:

- `401 Unauthorized`
- `403 Forbidden`
- `404 Not Found` from an incorrect realm or URL
- other non-JWKS 2xx/3xx content

The controller can consequently set `AuthenticationReady=True` even though
Envoy cannot obtain signing keys and authenticated API traffic fails.

The helper also creates its timeout context from `context.Background()` rather
than the reconcile context. Controller shutdown or reconcile cancellation does
not cancel an in-flight probe until its independent timeout expires.

**Recommendation**

Pass the reconcile context into the probe, require a 2xx response, bound the
response body size, decode the JSON, and verify a non-empty JWKS `keys` array.
Tests should cover 200 with valid JWKS, 200 with invalid JSON, 200 with an empty
key set, redirects, 401, 404, 500, timeout, and parent-context cancellation.

### F5 — Monitoring apply failures are logged and discarded (P2) ✅ FIXED

**Location**

- `internal/controller/costmanagementserviceconfig_controller.go:619-644`

The monitoring stage is intentionally skipped when ServiceMonitor and
PrometheusRule CRDs are unavailable. However, the implementation continues for
every apply error, including:

- authorization failures;
- rejected or invalid manifests;
- API server timeouts and outages;
- quota or admission-policy failures;
- field ownership conflicts.

Non-`NoMatch` failures are logged at error level but the phase still returns
success. Reconciliation can then mark the entire CR Ready even though the user
explicitly left monitoring enabled and the desired monitoring resources were
not created.

The earlier 2026-08-09 review marked this issue fixed after distinguishing log
levels. That improved visibility but did not fix reconciliation semantics or
status accuracy.

**Recommendation**

Continue only for `NoMatch` when optional CRDs are absent. Return other errors
through normal phase error handling. If monitoring must remain best-effort,
introduce a `MonitoringReady` condition and set it false for real failures so
the failure is visible without log access.

### F6 — Ready events are emitted on every successful pass (P2) ✅ FIXED

**Location**

- `internal/controller/costmanagementserviceconfig_controller.go:133-168`

The code intends to emit a Ready event only on the first transition to Ready:

```go
if cfg.Status.Phase != costv1alpha1.PhaseReady {
    // emit event
}
```

However, `cfg.Status.Phase` is overwritten with `PhaseProgressing` at the start
of the same function. By the time the check executes, it can never equal
`PhaseReady`. Every successful periodic drift pass therefore emits another
Ready event. Owned-resource changes can trigger additional duplicates.

This creates event noise, makes genuine readiness transitions harder to find,
and can churn the namespace Event objects indefinitely.

**Recommendation**

Capture the prior Ready/Available state before mutating status, or use the
previous `Available` condition and its transition time. Add a test that runs two
successful reconciliations and asserts exactly one Ready event, followed by a
failure/recovery test that asserts one additional event on the real recovery.

## Verification performed

- `go test` passed for all non-e2e packages.
- `go vet ./...` passed.
- `golangci-lint` passed after toolchain alignment.
- `govulncheck` reported no reachable vulnerabilities.
- Full e2e execution was unavailable because the Docker daemon was not running;
  the e2e suite failed during image-build setup before executing either spec.

## Suggested implementation order

1. Fix cluster-scoped naming and deletion ownership, including multi-namespace tests.
2. Make top-level status truthful for ordinary phase errors.
3. Block readiness on invalid explicit S3 credentials and invalid JWKS responses.
4. Define and enforce monitoring failure semantics.
5. Correct Ready event transition detection.
