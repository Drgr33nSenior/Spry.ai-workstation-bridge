# Installation, access and recovery

See [PERFORMANCE.md](PERFORMANCE.md) for owner-only analysis/selection exports,
retained failed reports and original-helper inspection after interruption.
Historical warm reports do not establish current readiness. No new operation
installs a candidate, deletes caches or bypasses qualification/recovery fences.

These procedures are for an authorized owner on the non-production workstation.
No installation, service change, cluster change, VPN/DNS setup or reboot was run
during implementation. Review the target checklist before installing artifacts.

## Prepare installation artifacts

Run `make check package` in the Bridge repository. Inspect the archive file list
and `dist/SHA256SUMS`. The archive contains binaries, deployment examples, API
contract and documentation. It does not include credentials, model weights,
installer source or build inputs. The application has no selected distribution
licence; resolve the owner licence decision before sharing or selling artifacts.

For a pacman-managed installation, use the [Arch package procedure](ARCH-PACKAGING.md).
The tagged workflow builds a source-based `PKGBUILD` into a `.pkg.tar.zst`; local
`make arch-source VERSION=v1.0.0` generates the recipe and verified source input.
The package ships examples, not active configuration, and does not start services.
Arch system hooks can create declared accounts/directories and reload unit
definitions. Review allocated UIDs and complete the steps below before startup.

Prepare a separate reviewed installed runtime bundle from the reference project.
The bundle must include `bin/workstationctl`, `lib/common.sh`, all sourced
`lib/workstation/*.sh`, `versions.lock`, and supporting files required by the
selected operations. Keep its directory tree root-owned and non-writable by the
API or worker. Generate its exact manifest with
`./bin/bridge-hostd --manifest /absolute/prepared/runtime-bundle` from the Bridge
checkout after the local build. This is an inspection
command; a printed digest is not approval or a signed-package provenance claim.
Install only after checking the prepared bundle against reviewed source and the
owner's package/signature process.

The separate read-only reference tree for catalog import must contain the
reviewed `versions.lock`, workstation example config, selected deployment
profiles and RAG integrity files used by `internal/catalog`. Reference and runtime
trees can be packaged from the same reviewed source revision, but must not be a
writable development checkout. Record the reviewed source revision and the
content hashes of the prepared files. Initial implementation evidence predates
the repositories' first commits and uses content hashes; that historical limit
does not remove the need to record a revision for a later installation.

Resolve UIDs on the target. The examples' numeric values are placeholders, not
the target inventory. `bridge` runs the controller, `bridge-worker` runs builds,
and root runs only the helper. Review `deployment/systemd/bridge.sysusers` and
`bridge.tmpfiles` before using the target's normal package installation process.
Do not add either unprivileged account to a container-engine or privileged group.

| Installed path | Owner and purpose |
|---|---|
| `/usr/lib/bridge/{bridged,bridgectl,bridge-hostd,bridge-worker}` | root-owned reviewed binaries |
| `/usr/lib/bridge/workstation-runtime` | root-owned hash-verified installed runtime |
| `/usr/lib/bridge/workstation-reference` | root-owned reviewed reference inputs |
| `/etc/bridge/server.json` | root-owned 0640, readable by `bridge`; contains references, no tokens |
| `/etc/bridge/host-policy.json`, `/etc/workstation/session-policy.conf` | root-owned 0600; helper authorization and shared legacy lock |
| `/etc/bridge/helper.kubeconfig` | root-owned 0600 dedicated restricted identity, inaccessible to API/worker |
| `/var/lib/bridge` | `bridge`, 0700; API store and `managed-source.json` |
| `/var/lib/bridge-hostd`, `/var/lib/workstation-session` | root, 0700; independent journal and legacy recovery snapshot |
| `/srv/ai/bridge-models` | managed persistent model storage; controller stages, workloads read only |
| Worker scratch/cache/source paths | dedicated, separately bounded storage defined in worker policy |
| `/run/bridge-hostd/control.sock`, `/run/bridge-worker/control.sock` | local typed protocols with peer-UID checks; never TCP |

Review the worker-specific policy, containment, offline source staging and
systemd instructions in [reference contracts](REFERENCE-CONTRACTS.md). Its source
and toolchain manifests, read-only hardware evidence, cgroup delegation and
scratch/cache filesystem bounds must exist before it reports recipes available.
Missing prerequisites produce explicit unavailable states.

Provision the managed model root with the existing reviewed workload reader GID
and mode 2750 before starting the controller. Only that root needs setgid for
new revision construction. Published directories are 0750, files/receipts 0640;
the controller inherits and verifies GIDs, never changes them. Keep both
`UMask=0077` and `RestrictSUIDSGID=yes`. Follow the bounded
[snapshot permission migration](REMEDIATION.md#published-model-permissions)
for legacy snapshots; do not recursively chmod shared storage.

Use `deployment/server.local.example.json` as `/etc/bridge/server.json`. Select
the actual node name consistently in the server, helper and workstation config.
Classify the target `dev`, `tst` or `int` from the owner's inventory; production is
refused. Do not relabel a target to bypass a source restriction. Import the
reviewed initial managed fields while the service is stopped:

```sh
/usr/lib/bridge/bridged --config /etc/bridge/server.json --import-source /absolute/reviewed-management.json
bridgectl admin bootstrap --config /etc/bridge/server.json --output /absolute/owner-private/bridge-owner.token --ttl 24h
```

Run these local administration commands as root or the explicitly allowed owner
UID with the required filesystem access. Precreate `/var/lib/bridge` for the
service account and the credential output directory as owner-only. Root writes
preserve existing service-directory ownership. Import and bootstrap refuse an
existing source/credential output. Do not solve a permission error by making the
state directory broadly readable.

Complete the root helper policy and independent qualification entries as
described in [HOST-EXECUTOR.md](HOST-EXECUTOR.md). Empty maps in the examples are
intentional refusals. Validate the installed systemd units against the installed
manuals. Start services only through the owner's normal host-management change
process. The controller is unprivileged and refuses live startup as root.

## Memory evidence and optional advice

Follow [memory budgets](MEMORY-BUDGETS.md) before using `memory.evidence.import`
or `memory.plan.export`. Collect the baseline and cold/warm observations as the
non-root workstation owner, then seal a new private bundle locally. Provision
the reviewed copy and its `memory_sources` entry through offline helper-policy
administration. Runtime manifests remain separately root-approved; do not update
approved hashes merely to make changed collection or planner code run.

Owner-authenticated previews do not import evidence or apply a candidate.
Confirming an import/export plan records evidence or generates unqualified
artifacts; it does not change the running Pod or managed source. Keep downloaded
plan/patch/rollback files private. Their download rechecks the complete baseline,
observed model-file hashes, launch settings, hardware/boot and target node. Nonzero CPU-offload
memory export is currently unavailable because the collector cannot attest that
setting. Candidate trials, rollback and qualification require separate reviewed
maintenance.

The optional actual OpenAI Agents API adviser is disabled by default. Enabling it
requires reviewed server policy, a provisioned credential-file reference and
separate provider spending controls. Review the guide's privacy and spending
limits first. Only selected sanitized evidence leaves the controller; no private
artifact, Kubernetes identity or Bridge management credential is sent. Advisory
text cannot approve or apply a plan. Deterministic planning needs no OpenAI account.

## Private HTTPS and VPN access

Live HTTP binds only loopback and is CLI-only (`browser_sessions: false`).
Browser management requires `browser_sessions: true`, a dedicated trusted HTTPS
hostname and a valid certificate, including for a loopback listener. All services
at that hostname share the cookie trust boundary; ports do not isolate cookies.
See [session migration](REMEDIATION.md#private-browser-migration).
A network listener requires explicit HTTPS,
an exact management origin and a certificate whose SAN identifies its hostname.
`deployment/server.vpn.example.json` uses the reserved documentation address
`192.0.2.10`; replace it with the already reviewed private interface or VPN
address. Do not use `0.0.0.0`, `::`, public ingress or an automatically discovered
interface. Configure DNS/VPN/firewall outside Bridge.

Have the owner's private certificate process issue the server certificate and
key. Install the key root-owned and readable only by the controller's service
group. The controller checks certificate identity and validity; the CLI uses
system roots or the context's explicit `ca_file`. It refuses redirects and has
no insecure mode. For example:

```json
{
  "endpoint": "https://bridge.internal.example:8743",
  "credential_file": "/absolute/owner-private/bridge-owner.token",
  "ca_file": "/absolute/owner-private/management-ca.pem",
  "timeout_seconds": 30
}
```

Open that exact origin in the browser. An alias absent from `allowed_hosts` or
`external_url` is refused. The application does not trust forwarded identity or
proxy headers. Family/friend public application accounts, VPN membership and
private source IPs do not grant Bridge access. Keep chat/inference ingress and
credentials separate; Bridge does not proxy their requests.

## Kubernetes identity and rotation

Review the scoped RBAC example in `deployment` before owner-controlled apply.
The session gates need read access to all Pods/nodes to identify unmanaged GPU
consumers, plus ReplicaSet ownership and namespace/security metadata. Mutation
is restricted to the configured namespace and named AI/gaming Deployments and
their scale subresources. Additional ConfigMap reads establish qualification
integrity. There is no Secret read, pod exec, arbitrary workload creation,
cluster-admin grant or default-context selection.

The named Deployment rule includes LIST because pinned
[kubectl v0.35.7 rollout status](https://github.com/kubernetes/kubectl/blob/v0.35.7/pkg/cmd/rollout/rollout_status.go#L170-L185)
uses a `metadata.name`-filtered List/Watch. The matching
[API-server request parser](https://github.com/kubernetes/apiserver/blob/v0.35.7/pkg/endpoints/request/requestinfo.go#L204-L230)
retains that exact name for authorization. This mechanism was verified on
2026-09-08. Keep `resourceNames`; an unfiltered list must remain denied.

Provision a dedicated service-account credential or client certificate using
the cluster's existing approved issuance process. Write credentials directly to
protected files; do not put them into commands, logs or this repository. The
owner constructs `/etc/bridge/helper.kubeconfig` with one explicit cluster,
context and static identity, valid CA/server identity and no exec/auth plugin,
proxy or insecure-TLS option. Never copy K3s's unrestricted admin kubeconfig.

For a short-lived token, renewal is an owner/platform responsibility. Before
expiry, replace the protected kubeconfig atomically, retain mode 0600, and use
the owner-approved helper restart procedure. For certificate rotation, update
the referenced protected certificate/key through the same process. Confirm
read and narrow mutation permissions with the target commands in
[QUALIFICATION.md](QUALIFICATION.md). Bridge must report an expired or
insufficient identity as unavailable; it must not request broader authority.

## Upgrade and backup

Before an upgrade, inspect operations from the CLI/UI. Stop admission by stopping
the controller. Allow the bounded helper operation to complete or record
recovery-required; do not terminate a handover simply to replace its executable.
Stop the helper and worker through the reviewed service procedure before
replacing their binaries, policies or journals. Preserve their current recovery
records and the legacy session snapshot.

With all writers stopped, take an owner-controlled backup of:

- `/var/lib/bridge` including source, operation store and adapter receipts;
- `/var/lib/bridge-hostd` and `/var/lib/workstation-session`;
- the worker journal and artifact provenance;
- root policy, approved runtime/source manifests and configuration references.

Use the existing encrypted/NAS backup arrangement. Bridge does not create a new
backup repository or expose retention/deletion APIs. Include credentials only
under the owner's established secret-backup controls; do not export them through
Bridge. Verify backup readability and permissions in an isolated directory.

Check the new schema and adapter contract versions before replacing binaries.
Version 1 retains recovery links without journal migration; old browser sessions
are invalidated by the new generation binding. Follow [REMEDIATION.md](REMEDIATION.md)
before upgrading an existing HTTP browser configuration. Unknown store/helper versions refuse
startup. Restore a backup only with all relevant writers stopped. Never restore
an old API database to infer that newer GPU effects did not occur. Inspect the
root helper journal and real workload/device state first. Credential recovery
revokes old sessions and credentials but does not settle GPU operations.

## Failure recovery

For an unavailable K3s identity or cluster, liveness remains available. Read the
operation's fixed failure category and inspect the dedicated identity/cluster
through authorized host tooling. Do not fall back to another kubeconfig. For a
source-drift refusal, export current source and create a new plan. For disk-full
or persistence failure, stop admission, preserve all journals, restore capacity
through owner maintenance and reopen the store before submitting work.

Use `bridgectl recover OPERATION_ID` to inspect an uncertain operation and obtain
a validated recovery plan. If the executor is still running, wait; if its final
result is known, refresh operation status. Before-dispatch source failures can
be reconciled without GPU changes. A handover requires a qualified explicit
restore under the legacy lock. Failed restore attempts form one persisted chain;
a validated retry can reference the original or an unresolved attempt. Local model
recovery inspects publication receipts/hashes; build recovery uses the worker's
own records and cgroups, not host restoration. The prior snapshot is retained; an occupied GPU,
incomplete process visibility, changed boot, expired qualification or failed
readiness can continue to refuse restore. Resolve the actual cause before retry.

Do not delete kubelet state, drain the node, kill unrelated consumers, remove a
build-inhibit marker or rewrite helper journals to obtain a successful status.
Disk/boot/firmware changes, Secure Boot key management, kernel promotion and
backup retention remain separate owner host-administration procedures.

For a detached memory import/export, use `bridgectl memory-inspect OPERATION_ID`
and inspect the original operation. This checks independent helper completion
without redispatch or GPU restoration. Retain failed inputs and partial outputs;
they are not qualified or exportable successes. See the
[memory failure procedure](MEMORY-BUDGETS.md#import-plan-confirm-and-export).
