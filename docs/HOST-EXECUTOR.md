# Host executor contract v1

`internal/hostexec` is the only Bridge adapter that changes GPU workloads. The
API uses an authenticated Unix connection as the configured `bridge` UID. The
helper accepts fixed typed requests, hashes the operation ID, actor, draft and
desired configuration, then records intent in its own root-owned journal.
Changing the API database cannot authorize a different node, image, qualified
configuration, executable, state path or recovery operation.

The target name in helper policy is the exact Kubernetes node name. Environment
must be `dev`, `tst` or `int`. The configured workstation file must identify the
same context, node, namespace and deployments. The helper uses a dedicated
static Kubernetes identity. Exec credential plugins, auth providers, proxy URLs
and insecure TLS configuration are refused. The API cannot read this identity.

## Installed artifacts and ownership

Install a reviewed runtime bundle at `/usr/lib/bridge/workstation-runtime`.
Include the installer's `bin/workstationctl`, `lib/common.sh`, every sourced file
in `lib/workstation`, and `versions.lock`. The helper checks every regular file
in the bundle against `artifacts` in `/etc/bridge/host-policy.json`, rejects
missing/unlisted files and symlinks, and requires every path component to be
root-owned with no group/world write permission. It never runs the development
checkout. System executables are resolved only through `/usr/bin:/bin:/opt/rocm/bin` and
checked against `system_executables` before dispatch. The fixed closure includes
Bash, kubectl, jq, session utilities and installed hardware collection tools.
Root-owned package symlinks are resolved to protected files before hashing.
Retain the reviewed Arch package baseline and update hashes only after reviewing
package provenance and adapter compatibility.

Generate the manifest locally without installing or approving anything:

```sh
go run ./cmd/bridge-hostd --manifest /absolute/prepared/runtime-bundle
# On the Linux target, hashes files only; it does not execute these programs.
/usr/lib/bridge/bridge-hostd --system-manifest
```

Review this output, package provenance, and the owner-resolved service UID before
filling `deployment/host-policy.example.json`. Empty provenance and qualification
maps deliberately refuse startup or live activation. Install the reviewed
policy as root:root 0600. The existing workstation configuration and version lock
remain canonical for installer settings. Only the typed Bridge settings are
deliberately migrated to `/var/lib/bridge/managed-source.json`. The helper
compares that file's semantic content revision with the requested configuration,
then applies independent root policy; file ownership is not authorization.

`/etc/workstation/session-policy.conf` must be root:root 0600 and agree with the
helper's canonical state directory and dedicated kubeconfig. The installer
compatibility change rejects a different state directory when this policy exists.
Without the installed policy, the existing standalone interface is preserved.

## Continuous locking and recovery

The helper obtains `/var/lib/workstation-session/lock` before preflight and holds
the same flock through source checks, legacy calls, Kubernetes replacement,
DRM inspection, durable final records and build-inhibit disposition. It passes
the already-held file descriptor as FD3. The installed session code verifies the
descriptor's device/inode against the canonical lock and retains FD9 throughout
the session. Competing direct CLI calls use the same lock.

The session change preserves the original baseline snapshot and all existing
boot, CPU Manager, resource, UID, image/security, unmanaged Pod and complete
`/proc`/DRM gates. Interrupted operations retain build inhibition. A helper stop
drains its bounded action; an API disconnect does not cancel it. A forced helper
exit leaves durable unfinished intent. On restart the helper marks that record
`recovery-required`; it never retries a process or trusts a saved PID. The next
mutation must be an explicit `profile.restore` identifying the unresolved helper
operation. Restore also rechecks the saved target's current qualification and
all legacy gates. It can refuse safely if the GPU remains occupied.

Failed restore attempts retain `RecoveryID` links in this root journal. The helper
admits a retry only when every unresolved helper record belongs to that validated
chain and target. Missing/cyclic links remain fenced; API records cannot supply
the missing authority. Successful C is durable before A/B settlement. If a parent
write fails, admission remains poisoned and startup finishes only the proven
journal settlement, without rerunning C. Status and duplicate submissions expose
`recovery-settlement-pending` until the helper can establish durable settlement;
the API must not use the retained internal success proof to clear its fence early.
Startup also acquires the canonical lock for pending settlement; contention
refuses startup. Older successful restores cannot settle later or unordered
uncertain attempts. The lock is held through settlement.
Original failures remain failed history. See [recovery commands](REMEDIATION.md#recovery-and-baseline-session-restoration).

Every journal replacement syncs the file, renames it and syncs the parent.
Installer state writes now use Linux `sync -f` before and after replacement.
Storage failure fences new mutations. The journal has a configured record cap;
archive records only during owner-reviewed stopped-service maintenance. Back up
the helper journal, legacy state, root policy and runtime manifest together.
The API store is a separate backup and cannot replace privileged recovery data.

Interrupted read-only hardware refresh, CPU-policy exports and memory
imports/exports become failed diagnostic operations at restart, not GPU recovery
fences. Their incomplete outputs are not reported successful. Mutating operations
retain the conservative unknown-effect behavior above.

## Serving qualification

The helper supports the pinned SGLang deployment's context, concurrency, static
memory fraction, model path/revision/name, GPU count, CPU/RAM/shared-memory
budgets and CPU offload. It preserves model dtype, quantization and parsers.
It refuses physical-card selectors and global request/output limits that this
deployment does not enforce. CPU offload uses `--cpu-offload-gb`; the pinned
implementation converts that value to GiB. The helper budgets offload plus
shared memory before submission. Upstream verification on 2026-09-08 resolved
`v0.5.15.post1` to commit
`0b3bb0cbe31873994c9f989fddfe2f87ca839fdd`; see its
[server arguments](https://github.com/sgl-project/sglang/blob/0b3bb0cbe31873994c9f989fddfe2f87ca839fdd/python/sglang/srt/server_args.py)
and [offloader](https://github.com/sgl-project/sglang/blob/0b3bb0cbe31873994c9f989fddfe2f87ca839fdd/python/sglang/srt/utils/offloader.py).
This verifies the argument mechanism, not ROCm/model performance.

`qualified_configurations` keys are exact hashes of the desired serving and
resource fields. Each owner-written value supplies `image` (immutable digest),
`model_revision` (40 hexadecimal characters), `model_path` under `/models/`, and
an `expires_at` RFC3339 timestamp, `host_model_path`, and a `files` map of relative
paths to SHA256 digests. `host_model_path` must be under policy `model_root`;
its relative path must equal the suffix of `model_path` under `/models/`.
The owner must verify the existing model PVC mounts that same managed storage.
The helper streams and checks every model file against this independent
manifest before AI activation or configuration. It rejects symlinks, extra files
and missing files; the stager receipt is advisory and never authorizes execution.
Obtain the configuration key from an exported configuration:

```sh
go run ./cmd/bridge-hostd --hash-configuration /absolute/reviewed-management.json
```

`session_qualifications.ai` and `.gaming` each contain `template_hash` and
`expires_at`, plus `config_maps` mapping referenced ConfigMap names to their
content hashes. Keys such as `ai/current` and `ai/previous` permit several
explicitly qualified templates during a reviewed update/recovery window.
Retain the prior template's qualification until the update is complete.
Hash reviewed exported objects with:

```sh
go run ./cmd/bridge-hostd --hash-template /absolute/reviewed-deployment.json
go run ./cmd/bridge-hostd --hash-configmap /absolute/reviewed-configmap.json
go run ./cmd/bridge-hostd --manifest /absolute/staged-model-revision
```

Before applying a changed serving configuration, export the existing Deployment
through the owner's scoped Kubernetes identity. Render the exact fixed candidate
offline using a draft qualification policy and the desired management JSON:

```sh
go run ./cmd/bridge-hostd --policy /absolute/draft-host-policy.json --preview-serving /absolute/reviewed-management.json --deployment /absolute/current-deployment.json
```

Save and review that JSON, then hash its template. This renders the same typed
field changes as the helper and performs no network or host action. Qualification
still requires the target checks; generating a hash alone is not approval.

For the model integrity map, omit `.bridge-receipt.json` from manifest output;
it is the stager's non-authoritative receipt. The template check covers referenced
ConfigMap contents as well as the Pod template; a mutable ConfigMap change
cannot silently alter a qualified engine or gaming profile.

These files and hashes are owner approval inputs after target qualification;
the API has no operation that creates them. A changed pod template needs a new
owner qualification. Missing or expired entries refuse activation even if an
API record or deployment annotation says qualified. The source's existing
qualification annotation and legacy gates must also pass.

Serving configuration stops both managed workloads, waits for termination and
DRM release, then changes only the fixed SGLang environment/resources with an
optimistic UID/resourceVersion check. It restores AI only if AI was running
before the operation. A stopped workload stays stopped. Gaming must first be
changed explicitly to AI or maintenance. Failure preserves the source update
and a recovery requirement; source publication and live apply are separate.

## Hardware and CPU maintenance export

`hardware.refresh` invokes the installed read-only collector into a new
operation-owned directory. It publishes sanitized CPU/RAM/GPU evidence; raw
serials, disk layouts, package lists and command output stay in root storage.
Current boot, report age, duplicate-model GPU PCI identities, render paths and
unknown topology remain distinct. The inventory endpoint supplements this with
current node/Pod/ReplicaSet observations using the dedicated identity. An outage
returns unavailable cluster status rather than fixture data.

If `worker_evidence_dir` is `/var/lib/spry-bridge-evidence`, the helper also
publishes root-owned mode 0644 `hardware.json` and `boot-id.txt` in that root-owned
0755 directory. This reduced raw contract preserves `status: observed` and
CPU/RAM/GPU evidence, excludes disk/serial/package records, and grants no access
to helper credentials or recovery state. Configure the build worker's hardware
path to this report. A report never becomes workload qualification through
publication.

`cpu-policy.export` invokes the existing `resources plan` contract and returns
`resource-plan.json` and `ansible-vars.json`. It does not drain a node, delete CPU
Manager state, edit kubelet configuration or apply Ansible. Those actions remain
owner-reviewed host maintenance.

## Memory evidence and candidate export

The owner-only `memory.evidence.import` and `memory.plan.export` actions use the
independent helper journal and canonical session lock. They preserve evidence
and generate unqualified artifacts; they do not resize Pods, restart serving or
change managed source. Follow [memory budgets](MEMORY-BUDGETS.md) for collection,
sealing, import, export and separate candidate qualification.

The non-root workstation owner runs collection with the reviewed installer
commands. The helper does not run those long experiments as root. An owner
provisions protected sealed inputs through offline administration and references
them in `memory_sources`. Installed runtime/tool hashes still require separate
root-policy approval; sealing or importing evidence cannot approve changed code.

Export requires the complete live Pod template to match the sealed baseline,
including independently qualified ConfigMaps. Observed model files must exactly
match the qualification's `files` map, except the existing non-authoritative
`.bridge-receipt.json` exemption. The collector hashes that receipt, and the
sealed evidence binds it, but it cannot authorize weights. Launch/settings,
current hardware/boot and observation node must also match. The current collector does not attest the
CPU-offload setting, so nonzero CPU-offload memory export is unavailable.
Missing observations or drift cannot be treated as zero usage or qualification.

`plan.json`, `patch.json` and `rollback.json` remain private because the baseline
Pod spec can contain secrets. The owner-only download path rechecks the baseline
and artifact hashes; operation records expose sanitized summaries and metadata.
The optional Agents API adviser runs in the controller, not this helper. It sees
sanitized evidence only and cannot approve policy, apply a patch or qualify a
candidate.

## systemd qualification

The service examples use distinct API and helper sandboxes. The API has private
devices and a restricted process view; the helper needs the host device and
complete process view for DRM checks. The helper has no TCP listener. Its only
outbound network use is the explicitly configured Kubernetes identity. No K3s
unit is a startup dependency of the API or helper.

The directives were checked against upstream systemd v257
[execution](https://github.com/systemd/systemd/blob/v257/man/systemd.exec.xml),
[socket](https://github.com/systemd/systemd/blob/v257/man/systemd.socket.xml) and
[service](https://github.com/systemd/systemd/blob/v257/man/systemd.service.xml)
sources on 2026-09-08. The target systemd version is not discovered; review its
installed manuals before installation. On that Linux machine, first run:

```sh
systemd --version
systemd-analyze verify deployment/systemd/bridged.service deployment/systemd/bridge-hostd.service deployment/systemd/bridge-hostd.socket
findmnt -n -o OPTIONS /proc
```

Expected: no unknown directives, the reviewed paths and service account exist,
and procfs visibility permits complete descriptor enumeration. Do not weaken
`hidepid`, security policy or namespaces to obtain a passing result. An
incomplete view must produce a refused session operation. Inspect root journal
and legacy state, then restore only after the underlying cause is resolved.

NOT RUN — target hardware unavailable: actual systemd sandbox behavior, Unix
peer checks on Linux, K3s admission/RBAC, CPU Manager, live DRM handover, ROCm
model readiness and physical resource safety. Cross-building does not establish
these capabilities. The helper fixtures test authority, persistence, locks,
idempotency, source drift, failure handling and resource-accounting contracts.
