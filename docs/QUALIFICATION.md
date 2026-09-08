# Target-machine qualification

**NOT RUN — target hardware unavailable.** This is the owner qualification path,
not a claim about the development Mac or the expected Threadripper workstation.
Use the exact installed adapter contract v1 and reviewed non-production target.
The [remediation qualification and migration guide](REMEDIATION.md) adds the
restrictive-umask reader test, failed-restore-chain recovery, dedicated HTTPS
browser boundary and authorized disposable cgroup probe. Complete those checks
before relying on these boundaries on the target.
Do not execute destructive installer/boot/firmware commands as part of these
checks. A failed check is a refusal to proceed, not permission to bypass it.

Expected inventory: Arch Linux, Threadripper 9960X (24 cores/48 threads), Gigabyte
TRX50 AI TOP, two Radeon AI PRO R9700 GPUs with separate 32-GiB VRAM pools and
expected gfx1201, currently two 32-GiB RDIMMs, and two Samsung 9100 PRO NVMe drives.
Board revision/firmware, actual drive capacities/layout, trained memory speed and
channel operation require discovery. Do not infer bandwidth from DIMM count.
Repeat collection and resource planning after a four-DIMM upgrade or reboot.

## 1. Review prerequisites and collect evidence

Prerequisites: an owner-reviewed installed runtime at
`/usr/lib/bridge/workstation-runtime`, root configuration, dedicated Kubernetes
identity, and a new private qualification directory. Create the directory through
normal owner host administration; the commands below use
`/var/lib/bridge-qualification/run-001`. Do not reuse an existing output directory.

Run locally on the target and save observations under that private directory:

```sh
uname -r
systemd --version
/usr/bin/kubectl version --client=true --output=json
findmnt -n -o OPTIONS /proc
/usr/lib/bridge/workstation-runtime/bin/workstationctl --config /etc/workstation/workstation.conf hardware collect /var/lib/bridge-qualification/run-001/hardware
/usr/lib/bridge/workstation-runtime/bin/workstationctl --config /etc/workstation/workstation.conf resources plan /var/lib/bridge-qualification/run-001/hardware/hardware.json /var/lib/bridge-qualification/run-001/resources /usr/lib/bridge/workstation-runtime/versions.lock
```

Expected: observed hardware, a current boot ID, stable PCI identities and render
paths for both cards, discovered SMT groups, explicit unknown values where
collection is incomplete, and a resource plan that retains host/K3s/build
reserves. Existing no-swap, storage/encryption and NAS choices remain unchanged.
Read private raw reports locally; do not publish serials, drive layouts or
credential-bearing command output. If inventory is incomplete or from another
boot, stop and recollect. Do not guess CPU affinity or reduce model context.

Run the Bridge hardware-refresh plan through the UI/CLI after the helper is
installed. Confirm the reduced worker evidence remains root-owned and the API
report carries the new boot identity and report age. The API must not see raw
helper recovery state or credentials.

## 2. Validate private identity and installed service boundaries

Set `qual_context` to the reviewed context already recorded in root policy. The
node name in server/helper/workstation policy must agree. Use only the dedicated
helper kubeconfig. Set `qual_configmap` to the exact rendered ConfigMap name
captured in the root qualification evidence and named Role; do not broaden the
Role to allow all ConfigMaps:

```sh
qual_context=reviewed-workstation-context
qual_configmap=sglang-profile-replace-with-rendered-hash
/usr/bin/kubectl --kubeconfig /etc/bridge/helper.kubeconfig --context "$qual_context" auth can-i list pods --all-namespaces
/usr/bin/kubectl --kubeconfig /etc/bridge/helper.kubeconfig --context "$qual_context" auth can-i get nodes
/usr/bin/kubectl --kubeconfig /etc/bridge/helper.kubeconfig --context "$qual_context" auth can-i get "configmap/$qual_configmap" -n ai-home-lab
/usr/bin/kubectl --kubeconfig /etc/bridge/helper.kubeconfig --context "$qual_context" get deployments -n ai-home-lab --field-selector metadata.name=sglang -o name
/usr/bin/kubectl --kubeconfig /etc/bridge/helper.kubeconfig --context "$qual_context" auth can-i update deployment/sglang -n ai-home-lab
/usr/bin/kubectl --kubeconfig /etc/bridge/helper.kubeconfig --context "$qual_context" auth can-i patch deployment/sglang --subresource=scale -n ai-home-lab
/usr/bin/kubectl --kubeconfig /etc/bridge/helper.kubeconfig --context "$qual_context" auth can-i get secrets --all-namespaces
/usr/bin/kubectl --kubeconfig /etc/bridge/helper.kubeconfig --context "$qual_context" auth can-i create pods/exec -n ai-home-lab
/usr/bin/kubectl --kubeconfig /etc/bridge/helper.kubeconfig --context "$qual_context" auth can-i '*' '*'
systemd-analyze verify deployment/systemd/bridged.service deployment/systemd/bridge-hostd.service deployment/systemd/bridge-hostd.socket deployment/systemd/bridge-worker.service
```

Expected: required reads and exact named mutations are allowed; Secrets, pod exec
and wildcard authority are denied. Check both game and AI Deployment permissions
and deny another Deployment name. Source-level YAML checks are not RBAC/admission
tests. Any unexpected grant requires owner correction before proceeding.

Check installed service identities, socket ownership and namespaces using:

```sh
systemctl show bridged bridge-hostd bridge-worker -p User -p Group -p MainPID -p PrivateDevices -p ProtectProc -p ProcSubset -p ReadWritePaths
systemctl status bridged bridge-hostd bridge-worker --no-pager
ss -lnt
ss -lx
```

Expected: the API is unprivileged; the worker is a different UID; the helper has
only a protected Unix listener and complete host process/DRM visibility. Only the
reviewed management interface listens on TCP. Do not create a public ingress or
weaken an existing `hidepid`/LSM/device rule to pass. Incomplete visibility must
produce a refused transition.

Bootstrap through explicit local CLI with the service stopped. Attempt offline
bootstrap/recovery from a different unprivileged account and expect denial.
Attempt a helper connection from that account and the worker and expect peer
authorization denial. Test owner/viewer/operator credentials through the actual
HTTPS endpoint, expiry/revocation/logout, Host/Origin/CSRF refusal and private CA
validation. Never put a real credential into a command argument or test log.

## 3. Qualify model, image and serving configuration

Review exact catalog model revisions and the pinned SGLang image. Stage a
selected model with a `model.stage` plan and inspect its operation and file
provenance. Confirm budget/free-space refusal before a large download. Interrupt
a controlled stage; no partial directory may appear ready. Re-plan/verify using
the pinned revision after resolving the interruption. Do not delete shared model
directories or redownload into writable container layers.

Use the existing owner model/ROCm qualification procedure in reference
`docs/MODELS.md`, `AI-PERFORMANCE.md` and `ROCM.md`. Available diagnostic contracts
include:

```sh
/usr/lib/bridge/workstation-runtime/bin/workstationctl --config /etc/workstation/workstation.conf rocm plan /var/lib/bridge-qualification/run-001/hardware/hardware.json /var/lib/bridge-qualification/run-001/rocm-plan
/usr/lib/bridge/workstation-runtime/bin/workstationctl --config /etc/workstation/workstation.conf rocm validate /var/lib/bridge-qualification/run-001/rocm-validation
```

These require the existing owner-installed SDK/runtime and still do not qualify
every selected model/kernel. Record actual engine/model readiness, quality,
context, concurrency, memory/offload settings and each card's independent VRAM
usage. Do not qualify from a model name or a successful metadata download.

Export the current named Deployment and referenced ConfigMaps to protected local
files using the dedicated identity. Before accepting a change, produce the fixed
candidate with the host helper's offline `--preview-serving` tool, review the
entire diff, then record exact configuration, template, ConfigMap and model-file
hashes plus expiry under the root qualification policy. See
[HOST-EXECUTOR.md](HOST-EXECUTOR.md) for the exact argument contract and hash tools.
An API account cannot add these entries. Retain explicitly qualified previous
templates for the rollback window.

Apply a serving plan with exact target/source revision. Expected: graceful
termination, no live DRM holders from the old workload, resourceVersion/UID-safe
template update, and readiness for the requested model/configuration. A stopped
workload remains stopped. Inspect separate source/live success flags. Change a
fixture qualification expiry or Deployment UID under controlled owner testing;
the next live action must refuse. Never edit production identity or fabricate a
qualification result to exercise this check.

## 4. AI/gaming transition and failure recovery

Review preconfigured AI/gaming/maintenance plans first. For gaming, explicitly
select the existing Sunshine or Steam Remote Play path in root workstation
configuration. Current safe handover unloads all AI. GPU count alone does not
support one-card AI/one-card gaming coexistence.

With idle, qualified workloads, submit AI then gaming through the CLI/UI as
documented in [CLI-AND-UI.md](CLI-AND-UI.md). Inspect each operation. In a controlled
test, start a competing legacy `session switch` using exactly the policy's
canonical state directory while Bridge holds the lock. It must fail to acquire
the same lock; it must not use an alternate directory. Do not run competing
hardware stress workloads to simulate this case.

Confirm that the build-inhibit marker blocks new managed/cooperative builds.
It must not claim to suspend already-running or unrelated builds. Test bounded
pod shutdown/readiness failure and a known occupied GPU only through the owner's
reviewed fixture/test process. Expected: recovery-required with previous snapshot
retained, no AI restart over uncertain ownership, and continued inhibition.

Disconnect the API client and restart the controller while the helper holds a
controlled transition. The helper must finish or persist recovery-required before
releasing its lock. The controller must inspect its journal without redispatch.
Request explicit recovery and confirm restoration only after current boot,
qualification, capacity, complete `/proc` and DRM checks pass. If the helper
cannot prove safety, preserve its journal and use separate owner diagnostics.

For existing Sunshine qualification use its documented diagnostic contract:

```sh
/usr/lib/bridge/workstation-runtime/bin/workstationctl --config /etc/workstation/workstation.conf sunshine diagnose /var/lib/bridge-qualification/run-001/sunshine
```

Encoding, frame pacing, transport and display behavior require actual devices and
clients. Bridge does not implement or qualify Internet streaming transport.

## 5. Build containment and persistent assets

Prepare the worker's exact locked source/toolchain manifests, current reduced
hardware evidence and bounded scratch/ccache filesystems before starting it.
For Sunshine, also stage the reviewed immutable base OCI archive and complete
signed offline package closure. See `REFERENCE-CONTRACTS.md`. There is no browser
input for repositories, Dockerfiles, scripts, output paths or executable arguments.

Run a named build plan and verify its process cgroup, actual CPU/memory/swap/PID
limits, scratch/cache bounds and artifact hashes. Through a separately reviewed
benign containment fixture, attempt reads of controller credentials and helper
socket, unrelated device access, parent-path escape and outbound network access.
All must fail. Spawn a harmless descendant within the fixture, request
cancellation and verify `cgroup.events` reports no populated descendants before
the worker returns cancelled. A lost response or missing cgroup observation must
remain recovery-required. Do not test containment by exposing real credentials.

Build completion produces unqualified local candidates. It must not install or
promote a package, replace a kernel, push an image or update source locks. Confirm
cache previews contain only managed roots and do not remove model/workspace/NAS
data. Recalculate build/offload/concurrency budgets after memory topology changes.

## Qualification record

Record date, kernel/systemd/K3s/SDK versions, source/runtime manifests, boot ID,
observed hardware, exact workloads/options, command outcomes, errors and tested
recovery path. Keep expected, observed, stale and unknown fields separate.
The record must explicitly distinguish unsupported source capabilities from
unprepared integration prerequisites. Do not infer ROCm/model performance,
P2P, ECC stability, memory bandwidth, encoding quality or workstation safety
from repository tests, cross-builds or this checklist.
