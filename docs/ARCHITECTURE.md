# Architecture and ownership

Bridge is one Go module with four runtime entry points. `bridged` owns the API,
browser sessions, plans, operation coordination and embedded assets. `bridgectl`
uses that API and provides explicit offline administration and client-local
bundle integration. `bridge-hostd` owns privileged GPU fencing and recovery.
`bridge-worker` executes only named recipes under a separate unprivileged UID,
filesystem namespace and resource cgroup. It has no management credentials or
helper access.

The controller can start without K3s. Liveness is `/health/live`; authenticated
inventory reports unavailable cluster dependencies separately. A failed live
probe never selects fixtures. Only local startup policy selects adapter mode.
Demo/live stores reject each other's mode marker and use different directories
and credentials. No runtime endpoint changes service policy, targets or adapters.

## One authority per setting

| Setting | Canonical authority | Import/export and reload |
|---|---|---|
| Listener, allowed Hosts, owner UID, role credentials, adapter mode and paths | Root-owned `/etc/bridge/server.json`; credential verifiers in `/var/lib/bridge/state.json` | Local policy edit and controller restart. Initial credential/recovery commands require the stopped store's exclusive lock. |
| Helper target, shared lock, runtime hashes, qualification and Kubernetes identity reference | Root-owned `/etc/bridge/host-policy.json`, `/etc/workstation/session-policy.conf` | Owner review and helper restart. No API write. |
| Installer workstation settings and package/model source locks | Reviewed `/etc/workstation/workstation.conf` and installed `versions.lock`; source repository remains the maintenance authority | Installer's allowlisted literal parser remains authoritative. Never source API input as shell. Reference import checks pinned values and source hashes. |
| Serving model/context/concurrency/memory/offload and workload CPU/RAM/shm/GPU budgets | Deliberately migrated fields in `/var/lib/bridge/managed-source.json` | Initial offline import from a reviewed file. Subsequent typed plans update this file atomically. API/CLI/UI export the same JSON. Helpers regenerate only fixed fields. |
| Managed cache admission/build budgets | `managed-source.json`, bounded by independently installed worker policy | Plan/apply updates future admission. Existing data is not removed. Root worker ceilings still apply. |
| Deployment non-managed fields, storage/PVCs, image locks, NAS backup, K3s/CPU Manager host policy | Existing source manifests, installer/Ansible and owner review | No broad apply/patch endpoint. CPU policy produces a maintenance export only. |
| Plans, generated manifests, build/model receipts and operations | Derived outputs and evidence | Not independent deployment truth; no silent reconciliation or Git push. |

Initial migration requires the owner to compare
`deployment/management.initial.example.json` with the installed selected profile,
resource plan and existing manifest. Preserve model revision and quantization;
the example is a single-GPU configuration, not discovered target inventory.
Use `bridged --config /etc/bridge/server.json --import-source /absolute/reviewed.json`
with the service stopped. It computes the content revision and refuses overwrite.
Leave all unrelated installer fields under their current authority. Applying an
old Kustomize profile later can overwrite migrated live fields; re-import/re-plan
deliberately. UID, resourceVersion, hardware and qualification checks prevent
Bridge from silently taking over a replaced workload.

Each plan binds target, actor, source revision, desired typed values and observed
reference/boot preconditions. It expires after ten minutes. Applying requires the
same actor, exact target and an idempotency key; reuse with another payload is a
conflict. The queue rechecks current credentials, source, mutable preconditions
and outstanding recovery before dispatch. HTTP disconnects do not cancel work.
Cancellation requires the observed operation revision and records a request;
only the executor can establish that descendants or device owners have stopped.

## Persistence and external effects

The small embedded store uses bounded JSON snapshots instead of a SQLite driver.
This keeps all runtime binaries in the standard library with no CGO or external
database dependency. The tradeoff is rewriting up to 32 MiB per transaction;
this is intended for one workstation's low-volume control operations.

A process-wide flock admits one writer or one offline administrator. An update
clones the state, validates it, writes a new file, syncs it, renames it and syncs
the directory. Existing permissions and ownership are preserved. Failed commits
do not become in-memory truth. A persistence error poisons mutation admission
until an owner inspects and restarts from durable state. The controller retains
2000 audit entries, up to 200 live/recent plans, 500 operation records and 128
credentials/browser sessions. Successful/failed/cancelled operations age out
after 30 days; uncertain operations are never pruned. Capacity exhaustion refuses
new work. No automatic deletion of model/build directories exists.

Schema version 1 is explicit. Unknown versions refuse startup; there is no
implicit destructive migration. A future schema change needs a tested offline
migration and a stopped-service backup. Back up `state.json`, `store.lock`'s
directory permissions, `managed-source.json` and adapter receipts together with
the separate root helper and worker journals. The lock file itself is not a
saved process lock. Restore only with all controllers stopped.

Source replacement and external effects are not one transaction. Intent is
durable before source update and again before executor dispatch. The operation
records `source_updated`, `dispatched` and `live_applied` separately. A crash
between source rename and record update is uncertain until source inspection.
An explicit owner recovery plan can reconcile a failure before any dispatch by
checking both possible source revisions; it keeps the original operation failed
and performs no GPU change. A dispatched mutation needs independent executor
evidence or a validated restore. The helper's root journal, not the API store,
authorizes privileged recovery. No saved PID is treated as execution identity.

On startup the controller inspects dispatched operations and does not resend
them. A surviving helper/worker continues independently. Unknown outcomes remain
`recovery-required`; an explicit recovery request obtains the executor's final
result or creates a restore plan. Already queued work cannot cross an unresolved
recovery fence. Restore is not a promise that every earlier effect is reversible.

Recovery uses persisted parent links within each authority's journal. Retained
attempts keep their ancestors; malformed links and unrelated fences are not
merged. Failed restores can be retried as a chain. Successful helper recovery
is durable before parent settlement, and restart rolls forward settlement without
repeating effects. Local model inspection binds the original execution hash to
receipt, manifest and publication evidence; worker inspection uses worker status.
Neither selects GPU restoration. Source-only reconciliation never dismisses an
uncertain external effect. See [migration and recovery](REMEDIATION.md).

## Authentication and network boundary

Offline bootstrap/recovery checks root or the explicit owner UID and acquires
the same exclusive store lock as the service. These commands have no HTTP route,
including on loopback. Credential files are new owner-only files in owner-only
directories; the database stores SHA-256 verifiers of 256-bit random secrets.
Roles are owner, operator (preconfigured sessions only), and viewer. All roles
have read access to non-secret client bundles; only owners edit configuration,
start named builds, revoke credentials or inspect the audit stream.

Browser sessions live server-side, expire within 30 minutes or the underlying
credential's expiry, and are revoked by logout or credential revocation. Cookies
use HttpOnly and SameSite=Strict. Live browser sessions require explicit local
policy and a dedicated trusted HTTPS hostname; their `__Host-` cookies are Secure,
host-only and Path=/. All HTTPS ports at that hostname share the trust boundary.
Live loopback HTTP is bearer CLI-only; demo HTTP uses a separate cookie purpose.
Persisted session purpose rejects pre-upgrade and cross-mode tokens even if a
client renames the cookie. Host, Origin, Fetch Metadata
and CSRF validation apply before browser mutation. Bearer clients can omit
browser headers. Body/header/time bounds and bounded authentication-attempt
limits protect admission. There is no permissive CORS, forwarded-header trust,
network bootstrap or public-management route.

Large model files stream to private staging directories. Only reviewed immutable
origins and redirects are allowed, DNS destinations are checked against private
and metadata networks, credentials are never forwarded, hashes are verified and
publication is atomic. Model storage is separate from the controller database.
Qualification cannot be granted by a successful download, build, source update
or a browser checkbox.

## Dependencies and evidence

Go 1.27.1 was checked against [official release metadata](https://go.dev/dl/?mode=json)
on 2026-09-08. HTTP timeout, TLS and shutdown mechanisms follow the
[Go HTTP contract](https://pkg.go.dev/net/http). The four runtime binaries have no
third-party library imports. Tool-only `kin-openapi v0.149.0` (MIT, commit
`1a812b4b73ede7fa295c63a5c89d1ca7250dcc07`) validates OpenAPI 3.1.1;
`golang.org/x/vuln v1.7.0` (Go BSD licence, commit
`617f44b718537dccdea1915395650e0529e3b72e`) runs vulnerability checks. Their
transitive module checksums are pinned; they are not linked into runtime binaries.
The Go vulnerability workflow follows [official guidance](https://go.dev/doc/security/vuln/).

The helper uses fixed `kubectl` arguments from the reviewed K3s v1.35.7+k3s1
contract. No `client-go` dependency is needed for this narrow adapter. The
[client-go compatibility matrix](https://github.com/kubernetes/client-go#compatibility-matrix)
and [out-of-cluster example at v0.35.7](https://github.com/kubernetes/client-go/tree/v0.35.7/examples/out-of-cluster-client-configuration)
were reviewed; neither authorizes using the developer's current context.
K3s documents the administrative nature of its default kubeconfig in
[cluster access](https://docs.k3s.io/cluster-access). Provision a separate identity
using the permissions described in the deployment and qualification guides.
Authorization and network separation follow the concrete controls in
[Kubernetes RBAC guidance](https://kubernetes.io/docs/concepts/security/rbac-good-practices/)
and [OWASP REST security guidance](https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html).
