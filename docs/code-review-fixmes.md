# Code Review — Open Issues (2026-08-15)

### P1 — Status / lifecycle

#### 1. Ready ignores most workload health [D1]

Only 4 resources are readiness-gated: Database StatefulSet, Cache Deployment,
Koku API Deployment, and Envoy Deployment. Everything else — Masu, Listener,
Celery, all ROS deployments, RBAC API/Worker, Kruize, Ingress, UI — is applied
without readiness checks. A bad image on any of those leaves the CR reporting
`Available=True` with zero running replicas.

#### 2. Progressing status causes a continuous reconcile loop [D2]

Every successful pass transitions `Progressing` True → False. Each transition
refreshes `LastTransitionTime`, so the status patch differs from what was read.
No generation-change predicate on the primary watch, so the status update
re-enqueues immediately. The 5-minute drift interval is effectively bypassed.

#### 3. Failed OIDC check overwritten with success [D3]

Validation sets `AuthenticationReady=False` when JWKS is unreachable.
`reconcileEdge` later unconditionally sets it `True` when the Envoy Deployment
and Route exist. A dead Keycloak can finish a pass as `AuthReady=True`.

#### 4. Rollout readiness accepts stale replicas [D14]

`isDeploymentReady` only checks `AvailableReplicas >= spec.replicas`.
No `ObservedGeneration`, `UpdatedReplicas`, or revision check. After an image
change, old replicas satisfy the gate before any new pod exists.

### P1 — Resources

#### 5. ConsoleLink name collides across namespaces [F1, partial]

Kruize ClusterRole/Binding names now include a namespace hash — fixed. But
`NameConsoleLink` still uses only `cfg.Name + "-cost-management"`. Two CRs
with the same name in different namespaces collide. Finalizer deletion still
does not verify ownership before deleting cluster-scoped resources.

### P2 — Data integrity

#### 6. Migration identity is image-tag only [D4]

Migration Job completion is keyed on the image tag annotation alone. Changing
the database host, credentials Secret, or image repository while keeping the
same tag skips migrations. Applications start against a potentially empty
database.

### P2 — Validation / discovery

#### 7. BYOI deployments require a default StorageClass [D21]

`resolveStorageClass` runs unconditionally. When both database and cache are
external (no PVCs needed), a missing default StorageClass still blocks
discovery with `DiscoveryComplete=False`.

### P2 — API contract

#### 8. Accepted Route TLS modes cannot reach plaintext Envoy [D19]

The CRD accepts `edge`, `passthrough`, and `reencrypt`, but Envoy has only a
plaintext HTTP listener. `passthrough` and `reencrypt` produce unusable Routes.

#### 9. Zero replicas are silently overridden to 1 [D13, partial]

Koku API, Masu, and Listener convert `replicas: 0` back to 1 (or 2). Users
cannot disable a component via replicas. The `profile` field is a documented
no-op (tracked separately). The `enabled` fields on Koku sub-components were
removed.

### P2 — Monitoring

#### 10. ServiceMonitors cannot scrape most targets [D17]

ServiceMonitors select port `"metrics"`, but Koku, Masu, and Kruize Services
expose port `"http"`. Listener has no Service. No dedicated metrics ports are
defined. Prometheus cannot discover scrape targets for most components.

### P3 — Operational

#### 11. No watches on Routes, ServiceMonitors, ClusterRoles [I7]

`Owns()` covers core resources but not Routes, ServiceMonitors,
PrometheusRules, or cluster-scoped resources. External deletion relies on the
5-minute drift requeue. Needs discovery-gated dynamic watches.

#### 12. Long CR names make generated resources invalid [D22]

No name truncation or label-value length validation. A CR name near 63 chars
plus suffixes like `-celery-worker-{queue}` exceeds Kubernetes limits. The CR
is admitted but all child creation fails.

