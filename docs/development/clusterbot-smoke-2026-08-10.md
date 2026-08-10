# Clusterbot smoke — 2026-08-10 (resume notes)

Smoke of **upstream `main` @ `7908c48`** on OpenShift clusterbot, then fixes on
branch `fix/clusterbot-kruize-rbac-ingress`. Cluster was expected to shut down
within hours — use this to continue on a fresh clusterbot tomorrow.

## What was deployed

| Piece | Namespace | Notes |
|-------|-----------|--------|
| Operator image | `koku-service-operator-system` | `quay.io/martin_povolny/koku-server-operator:fix-7908c48` (also `:dev`) |
| CMSC `cost-management` | `cost-onprem` | Bundled DB/cache; MinIO + AMQ Streams BYOI |
| MinIO | `cost-byoi-infra` | Storage secret `cost-management-storage-credentials` |
| Kafka | `kafka` | `./config/samples/byoi/deploy-kafka.sh` (`STORAGE_CLASS=gp3-csi`) |
| RHBK | `keycloak` | Chart `scripts/deploy-rhbk.sh` — **completed** before handoff |
| UI OAuth secret | `cost-onprem` | Stub first; real mirror from Keycloak **not confirmed** after interrupt |

Last known CMSC state (before Keycloak wiring finished):

- `phase=Ready`, `Available=True`, `UIReady=True` (secret present)
- All app Deployments **1/1** except UI (`oauth-proxy` CrashLoop — no Keycloak DNS at that time)
- Routes: API + UI present

Public Keycloak URL from RHBK script:

`https://keycloak-keycloak.apps.chat-bot-wqsg6-65d66e.crt-mce-aws.devcluster.openshift.com`

UI URL:

`https://cost-management-ui-cost-onprem.apps.chat-bot-wqsg6-65d66e.crt-mce-aws.devcluster.openshift.com`

## Fixes in this PR (code)

1. **Kruize ClusterRole escalate** — manager SA could not create `{cr}-kruize`
   ClusterRole (missing held permissions for pods/nodes/endpoints/metrics/…).
   Added kubebuilder RBAC markers → `config/rbac/role.yaml`.
2. **Empty ingress image** — omitted `spec.ingress.image` produced `:` /
   `InvalidImageName`. Default to `quay.io/iop/ingress:master` (chart parity);
   samples updated; unit tests added.

## Findings / gaps (not all fixed in code)

| Severity | Finding | Status |
|----------|---------|--------|
| **Blocker** | Manager cannot create Kruize ClusterRole (RBAC escalate) | **Fixed in PR** |
| **Blocker** | Empty ingress image → InvalidImageName | **Fixed in PR** |
| **High** | Sample `ui.oauthProxy.image` uses `registry.access.redhat.com/...` — clusterbot requires `registry.redhat.io/...` (terms) | **Should fix in PR samples** |
| **High** | Bundled sample `quay.io/martin_povolny/koku:latest` is **arm64-only** — migrate ImagePullBackOff on amd64 clusterbot | Doc / sample: use `cost-mgmt-dev-tenant/koku:d8055ac` on amd64 |
| **Expected BYOI** | RHBK not created by operator — need `deploy-rhbk.sh` | Deployed on this cluster; redo on next |
| **Expected BYOI** | UI OAuth Secret must be mirrored (`mirror-ui-oauth-secret.sh --force`) | Script exists; re-run after RHBK |
| **Config** | Set `spec.auth.keycloak.issuerURL` to public RHBK Route when `iss` ≠ in-cluster URL | Not applied after RHBK (interrupted) |
| **Ops** | `make deploy` OpenAPI validate can timeout on clusterbot — use `--validate=false` | Workaround only |
| **Ops** | Grant `anyuid` (+ often `privileged`) to `{cr}-koku` SA for migrate Jobs | Manual each cluster |
| **Cleanup** | Stale UI ReplicaSet pods can linger after image patches | Delete old pods / RS |

## Resume checklist (new clusterbot tomorrow)

```bash
# 0. Context
kubectl config use-context <new-clusterbot>

# 1. Operator (from this branch or rebuilt image)
cd .worktrees/fix-clusterbot-kruize-ingress   # or checkout the PR branch
export IMG=quay.io/martin_povolny/koku-server-operator:dev   # rebuild/push if needed
KUSTOMIZE=../../bin/kustomize   # or make kustomize
(cd config/manager && $KUSTOMIZE edit set image controller=$IMG)
$KUSTOMIZE build config/default | kubectl apply --validate=false --server-side --force-conflicts -f -

# 2. Infra BYOI
STORAGE_CLASS=gp3-csi LOG_LEVEL=INFO ./config/samples/byoi/deploy-kafka.sh
# MinIO: kubectl apply -f config/samples/byoi/infra/{namespace,serviceaccount,credentials,minio}.yaml
# + storage secret in cost-onprem (access-key/secret-key)

# 3. RHBK
cd ../cost-onprem-chart   # sibling checkout
STORAGE_CLASS=gp3-csi LOG_LEVEL=INFO \
  COST_MGMT_NAMESPACE=cost-onprem \
  COST_MGMT_RELEASE_NAME=cost-management \
  COST_MGMT_UI_BASE_URL="https://cost-management-ui-cost-onprem.apps.<DOMAIN>" \
  ./scripts/deploy-rhbk.sh

# 4. Mirror UI OAuth + wire CR
NAMESPACE=cost-onprem CR_NAME=cost-management \
  ./config/samples/byoi/mirror-ui-oauth-secret.sh --force

# Patch CMSC (example fields):
# - global.clusterDomain / storageClass=gp3-csi
# - objectStorage → MinIO
# - costManagement.api/masu image → amd64 koku tag
# - ui.oauthProxy.image → registry.redhat.io/rhceph/oauth2-proxy-rhel9:v7.6.0
# - auth.keycloak.url → http://keycloak-service.keycloak.svc.cluster.local:8080 (confirm svc name)
# - auth.keycloak.issuerURL → https://keycloak-keycloak.apps.<DOMAIN>
# - auth.keycloak.tls.insecureSkipVerify: true (lab) or caCertSecretName

oc adm policy add-scc-to-user anyuid -z cost-management-koku -n cost-onprem

# 5. Verify
kubectl -n cost-onprem get cmsc cost-management -o yaml | less
kubectl -n cost-onprem get deploy,pods
kubectl -n cost-onprem logs deploy/cost-management-ui -c oauth-proxy --tail=50
```

## Success criteria for “everything working”

- [ ] CMSC `phase=Ready`, `Available=True`, `UIReady=True`, no `Degraded`
- [ ] UI pod **2/2** Ready (oauth-proxy + app)
- [ ] Browser login via UI Route against RHBK
- [ ] API Route `/api` accepts JWT from Keycloak (not only gateway Ready)
- [ ] No ImagePullBackOff / InvalidImageName

## Related

- Code branch: `fix/clusterbot-kruize-rbac-ingress`
- Chart RHBK: `cost-onprem-chart/scripts/deploy-rhbk.sh`
- UI OAuth notes: wiki `docs/cmsc/koku-ui-oauth2-proxy.md` (personal wiki)
