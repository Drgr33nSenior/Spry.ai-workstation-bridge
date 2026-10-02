# Spry.ai Workstation Bridge — Go implementation agent prompt

## Mission

Build a complete, integrated first release of Spry.ai Workstation Bridge: a
private workstation management API, a Go CLI, and a usable web management UI.
Implement the bounded feature set below in this task. Do not stop at an audit,
scaffold, read-only dashboard, proposed architecture, or disconnected mock UI.

This is a personal project for an experienced SRE to use for experimentation.
Prefer a maintainable single-workstation application over enterprise
infrastructure. The owner may sell some compute later. Preserve sensible
identity, authorization, API versioning and provenance boundaries, but do not
implement billing, payments, customer tenancy or a compute marketplace.

Use internal implementation steps and focused tests as needed. Deliver the
whole agreed feature set in one cohesive change set, not a series of proposed
future phases. A request for one implementation pass does not authorize unsafe
deployment, bypassing existing gates, or claiming unverified capabilities.

## Workspace, authority and discovery

Expected repositories, to verify rather than assume:

- New application: /Users/uk-gr9yjx0l0y/Projects/Spry.ai-workstation-bridge
- Existing installer/reference:
  /Users/uk-gr9yjx0l0y/Projects/ArchLinuxThreadripperAI
- Existing entry point: bin/workstationctl in the installer repository.

The Bridge repository was last observed without application code or commits,
with staged and untracked IDE metadata. Its PYTHON_MODULE declaration was IDE
metadata, not a Python implementation. Inspect again; preserve any subsequent
work. Resolve the Go module path from the actual repository identity without
printing credential-bearing remote URLs. Do not invent a GitHub organization.

Read applicable AGENTS.md files, project rules and existing documentation.
Inspect both working trees and relevant history when available. Do not assume
HEAD exists or that Git diff includes untracked files.

Implement Bridge in its own repository. Treat the installer as a compatibility
reference, not a collection of files to copy wholesale. Narrow, backward-
compatible installer changes are permitted only when a real integration
requires them; document and test them separately. Do not rewrite the installer,
change its language, or move its source into Bridge.

Inspect these installer surfaces and follow their actual paths if reorganized:

- bin/workstationctl; lib/workstation/runtime.sh.
- lib/workstation/session.sh, hardware.sh, resources.sh and build.sh.
- lib/workstation/rocm.sh, sunshine.sh, rag.sh and agents.sh.
- config/workstation.conf.example, versions.lock and relevant config parsers.
- apps/base, apps/overlays, infrastructure/ansible and infrastructure/gpu-operator.
- docs/WORKSTATION.md, HOME-LAB.md, AI-PERFORMANCE.md, MODELS.md, RAG.md,
  AGENT-HARNESSES.md and ISO.md.
- Relevant tests and the existing required check target.

Create a compact implementation matrix with:
Requirement | Existing source/contract | Bridge implementation | Tests | Hardware qualification.
Distinguish features already present, integration gaps and genuinely unsupported
operations. Do not convert an existing refusal or qualification requirement into
a successful response.

This task authorizes repository code, examples, packaging definitions,
documentation and bounded local tests. It does not authorize running the
installer, changing disks/boot/firmware, installing system services, modifying a
live cluster, opening externally reachable ports, configuring public DNS/VPN, rebooting, publishing
artifacts, or staging/committing/pushing Git changes. Do not retrieve secrets.
Use isolated fixtures instead of the development machine's hardware or cluster.
Ephemeral loopback listeners, temporary files and generated test credentials
are permitted for bounded application tests; do not use real user credentials.

Make reasonable implementation choices and record them. Ask only about a
material unresolved choice that cannot be answered from these instructions or
the repositories; continue independent work. Preserve any existing licence.
If no licence exists, record the owner decision as pending rather than selecting
distribution or commercial terms on the owner's behalf.

## Target and operational boundaries

Expected target, not discovered inventory:

- Arch Linux; AMD Threadripper 9960X, 24 cores / 48 threads.
- Gigabyte TRX50 AI TOP; revision and firmware require target discovery.
- Two AMD Radeon AI PRO R9700 GPUs, 32 GiB each, expected gfx1201.
- 64 GiB host RAM in two 32 GiB RDIMMs now; expected dual-channel DDR5-5600.
  Actual trained speed and channel operation remain unverified.
- A future four-DIMM upgrade requires rediscovery and resource retuning.
- Two Samsung 9100 PRO NVMe drives; preserve discovered capacity and layout.
- Existing K3s AI and gaming workloads, persistent caches, no swap by default,
  and the established storage/encryption/NAS-backup choices.
- Local developer harness preference: Qwen Code, then DSH, then Hermes.
  These are client-side tools, not services to deploy in K3s.

Keep expected, observed, stale and unknown values distinct. Two GPUs do not
provide one transparent 64 GiB VRAM pool. Do not hardcode GPU enumeration or CPU
IDs, infer memory bandwidth from DIMM count, or qualify a model from its name.
Read selected models and versions from the reference project; verify upstream
identities and support before using them. A historical name in a prompt or a
checked-in default is not proof that a release, model or GPU kernel exists.

### Deployment and access

Run the management server on the workstation as an unprivileged systemd
service, outside K3s. Support a separate, tightly restricted host executor where
privileged operations require it. Keep the server and recovery access usable
when K3s is unavailable; report dependent operations as unavailable.

Keep user-facing chat/inference applications in the existing deployment.
Their eventual public-domain ingress is separate from Bridge. Do not proxy
inference through Bridge, expose its administration through public ingress, or
give public applications Bridge credentials.

Default to local-only access. Provide an explicit authenticated HTTPS mode for
a reviewed management interface or VPN address. No default wildcard listener,
unauthenticated LAN mode or implicit trust of private source IPs. VPN access
reduces network exposure; it does not replace operation authorization.

The private web UI uses the same management identity and policy as the CLI.
Family/friend access to public compute does not confer management access.
Public coding-agent execution, sandboxed customer workspaces and game-streaming
Internet transport are separate projects, not implicit features of this API.

## Implementation architecture

Use Go for the API, CLI, operation engine and required host-executor protocol.
Use one Go module with clear internal packages. Suggested entry points:

- cmd/bridged: API, operation coordination and embedded web UI.
- cmd/bridgectl: local/remote management client and explicit local administration.
- cmd/bridge-hostd: restricted host executor, only for the required privileged
  actions; no general shell or generic service manager API.

Adjust names to repository conventions. Do not introduce microservices, a
Kubernetes operator, plugin marketplace, distributed queue, service mesh or
generic multi-host scheduler.

Prefer the standard library, including net/http. Add dependencies for concrete
needs, such as client-go, OpenAPI tooling or embedded persistence; justify
licence, maintenance and runtime consequences. Match Kubernetes dependencies
to the discovered K3s compatibility requirements, not simply their latest tags.
Pin the current supported Go toolchain and dependency/tool versions. Keep
go.mod/go.sum and generation checks reproducible. Do not assume Go builds obey
the installer's CFLAGS or require CPU-specific compiler experiments.

Use a small embedded durable store for operations, identities and audit records
if needed; SQLite is a reasonable default. Document the driver, CGO implications,
transaction semantics, schema migrations and backup procedure. Do not require
PostgreSQL, Redis or K3s to start the controller.

Serve a functional UI from the application. Prefer embedded HTML/CSS/JavaScript
with no production Node.js runtime. A substantial frontend framework is not a
requirement. If additional frontend tooling materially simplifies the required
UI, justify it, pin it and test the built assets. Keep UI assets local: no CDN
scripts, external fonts or telemetry services required for operation.

### Configuration ownership

Keep one authority for every managed setting. Document the ownership map:

- Bridge service policy, credentials references and allowed targets are
  administrator-owned host configuration.
- Existing workstation settings, package/model locks and source manifests remain
  authoritative unless a specific field is deliberately migrated.
- Bridge drafts, plans and operation records are not a competing source of
  deployment truth. Generated manifests are outputs, not manually edited inputs.

Implement a concrete source-update/export mechanism for supported configuration
changes. Use deterministic generation and revision/hash checks; reject external
edits detected since planning. Avoid arbitrary YAML/JSON patch endpoints.
Constrain runtime target paths and resources to an administrator-defined scope.
Record a successful source update separately from a successful live apply.
Specify exact canonical paths, import/export ownership and startup/reload
semantics. Use durable atomic file replacement, preserving permissions, and
test crashes between source updates, operation records and external effects.

Never let an API client supply a shell fragment, arbitrary executable path,
kubeconfig, target node, output path or unrestricted environment map. Preserve
the existing allowlisted configuration parser semantics; do not source user
input as shell code. No silent background reconciliation or Git push.

## Complete functional scope

Provide API, CLI and UI coverage for the supported management workflows below.
The UI can download client artifacts instead of executing client-local actions.
Hardware-dependent features must have real adapters plus accurate unavailable
states; fixtures alone do not implement the live integration.

### 1. Model and serving management

- Import/list the project's selected models with source revision, quantization,
  files, digests, licence metadata where available, size and staging status.
- Stage and verify selected models through existing supported mechanisms or a
  narrowly scoped adapter. Stream large downloads; do not load them into RAM.
  Use managed persistent paths, space budgets, integrity checks and atomic
  publication. Do not publish partial downloads as ready or silently redownload
  shared models into container writable layers.
- Reject arbitrary remote URLs. Restrict download origins and redirects to
  reviewed upstreams; prevent metadata/internal-network access and credential
  forwarding. Keep source credentials out of task records and logs.
- Edit validated serving options supported by the pinned engine: model,
  context, concurrency, request/output limits where enforceable, memory
  allocation, CPU offload and selected GPU count.
- Preview configuration and resource consequences, then apply an approved plan.
  Implement start, stop, restart and model switch with graceful termination,
  bounded waits and readiness checks.
- Preserve model quality and selected quantization. Do not reduce context or
  change the model to make validation pass. Distinguish requested limits from
  limits the engine can actually enforce.
- Keep existing image/model qualification gates. No API checkbox may turn
  unverified artifacts into qualified ones. Do not install another inference
  framework merely to demonstrate the controller.

### 2. Resource management

- Import and refresh supported target hardware/resource evidence. Show stable
  GPU identities, discovered render paths and report age/boot identity.
- Configure workload CPU, RAM, shared-memory and GPU-count budgets within the
  existing deployment abstraction. Count other consumers and host/K3s reserves.
- Validate Guaranteed QoS, whole-core/SMT requirements and supported kubelet
  options where the existing policy requires them. Unknown topology must not
  produce guessed CPU affinity.
- Account for limited current host bandwidth through explicit build, offload
  and concurrency budgets. Do not claim a measured bandwidth improvement.
- GPU resource counts are not deterministic physical-card selection. Honour
  the installed device plugin; reject unsupported placement requests.
- Host CPU Manager policy changes remain reviewed maintenance plan/export
  operations. Do not delete kubelet state, drain the node or rewrite host CPU
  policy as a side effect of changing a workload budget.

### 3. AI, gaming and maintenance profiles

- Implement explicit plan, switch, restore and operation inspection workflows
  using the existing session state machine and gates.
- Preserve host checks for boot identity, workload qualification, resource
  capacity, unmanaged GPU consumers, pod termination and live DRM holders.
- Serialize all conflicting GPU operations, including serving model switches,
  direct CLI session commands and Bridge requests. Do not create an independent
  lock that permits the existing command to race the new API.
- Resolve the shared legacy lock from root-owned policy. Hold it continuously
  across preflight, mutations, DRM checks, durable final state and build-gate
  disposition, not separately around individual helper calls. If the API exits,
  the lock owner must complete safely or persist recovery-required before release.
- Follow existing AI/gaming coexistence limitations. If the current controller
  unloads all AI before gaming, preserve that safe behaviour. Do not promise
  one GPU for AI and one for gaming without a supported, qualified allocation
  mechanism.
- Pausing incoming requests is not model unloading. Do not report handover
  success while the old workload still owns the device or observation is
  incomplete.
- Honour cooperative build inhibition and existing temporary-setting restoration.
  Do not claim unrelated or already-running builds were suspended if they were
  not.
- Preserve failure recovery. A timeout, cancellation or failed start must not
  restart AI over a possibly occupied GPU or erase the previous snapshot.
- Keep Sunshine and Steam Remote Play as explicit alternative session paths.
  Do not implement a new streaming transport.

### 4. Builds and persistent assets

- Expose named, existing supported build recipes, their fixed source revisions,
  target options, output locations and required qualification.
- Implement queued start, progress, cancellation where safe, result inspection
  and artifact provenance for those recipes. No arbitrary command editor.
- Reuse the existing compilation budget and ccache rules; bound job count,
  memory, queue depth, logs and scratch usage. Respect gaming/maintenance policy.
- Run compilation under a dedicated unprivileged worker context, separated from
  the API's credentials and privileged helper. Building source executes code.
  Do not give the API membership in a root-equivalent container-engine group.
- Enforce worker containment beyond its UID: no management credentials, helper
  socket or unrelated device access; bounded filesystem access; controlled
  environment; and no network by default. Stage inputs separately or permit only
  reviewed recipe-specific network access. Track the job's process group/cgroup
  so cancellation accounts for descendants, not only the parent PID.
- Require reviewed recipe/source updates before a new build. Do not accept an
  arbitrary repository URL, Dockerfile, build script or image promotion through
  the browser.
- Build completion does not install/promote a package, replace a kernel or
  qualify a GPU image. Keep those existing approval boundaries.
- Configure budgets and show usage for managed model/compiler/shader caches.
  Preserve existing shared storage and NAS backup ownership. Automatic deletion
  of models or general work/build directories is outside this release; provide
  a bounded cleanup preview rather than a broad delete endpoint.

### 5. Developer client configuration

- Export non-secret endpoint/model profiles and supported native configuration
  bundles for Qwen Code, DSH and Hermes, in that preference order.
- Reuse the existing generator/integrity contract. Preserve manual harness
  selection, existing policy refusals and unsupported client-mode limitations.
- Keep client credentials in supported local secret inputs, not exported files.
  Do not include management tokens, kubeconfigs or host-executor access.
- Provide explicit client-local CLI integration for configuration and, where
  already supported, launch. The server cannot launch an IDE client on the Mac.
- Do not auto-install harnesses, expose their tool execution remotely or imply
  that they automatically inherit the RAG database.

### 6. Usable management UI and CLI

Deliver working pages for model/serving configuration, resource budgets,
operating profiles, build jobs, client-profile export and operation recovery.
This is a management application, not a GPU-monitoring dashboard.

Provide forms with validation, change previews, confirmation of the exact
target, progress, actionable failures and explicit unsupported states.
Surface destructive/disruptive consequences before submission. Handle keyboard
navigation, labels, loading/empty states and a narrow/mobile layout.

Use the same service contracts and authorization for UI and CLI. No duplicate
business rules in JavaScript. Provide machine-readable CLI output and stable
exit codes, endpoint/context selection, deadlines and operation wait/status.
Do not put credentials in command-line arguments or switch to a local privileged
execution path silently when a remote API fails.

## Authentication, authorization and host execution

Implement real authentication in this release, not TODO middleware.

- Provide local-only initial owner bootstrap and recovery through explicit CLI
  administration. No default password, unauthenticated HTTP registration or
  network-accessible first-user takeover.
- Authorize bootstrap/recovery through root or an explicitly allowlisted owner
  UID, using Unix peer credentials or an offline command with the service stopped
  and store exclusively locked. Never expose these operations over TCP, including
  loopback. Another local account is not implicitly an administrator.
- Provide identified, revocable and expiring scoped CLI credentials. Use
  cryptographically random secrets and store verifiers rather than reusable
  plaintext. Deliver generated credentials to a new owner-only file or another
  secure local mechanism, not logs, ordinary command output or command flags.
- Provide an actual browser login/logout path. Use short-lived server-side
  sessions with HttpOnly/SameSite cookies, Secure cookies on HTTPS, CSRF
  protection and session revocation. Do not store management bearer tokens in
  browser localStorage or URLs. A local credential exchange is sufficient;
  do not require a new external identity-provider service for initial use.
- Require TLS and valid server identity for network access. Do not add insecure
  certificate-verification bypasses. Trust forwarded identity/headers only from
  an explicitly configured authenticated proxy path.
- Validate Host for HTTP requests and apply Origin/Fetch Metadata and CSRF
  controls to cookie-authenticated browser mutations. Bearer-authenticated CLI
  requests may omit Origin; do not require fake browser headers. Apply
  request-size, timeout and authentication-attempt limits. Keep CORS restrictive.
- Support a small role set: owner/admin, session operator and viewer. Operators
  can request preconfigured sessions, not edit policy, install software or submit
  arbitrary builds. Enforce policy server-side for every action and artifact.

Use a dedicated Kubernetes identity with the permissions actually required.
Inventory cluster-wide read access required by the existing GPU-consumer checks
separately from narrow mutation permissions. Do not copy the unrestricted K3s
admin kubeconfig or grant cluster-admin to simplify integration. Configure an
explicit reviewed cluster identity; do not follow the developer's current
context. Document credential provisioning/rotation without collecting secrets.
Require an explicit environment classification and preserve existing restrictions
on permitted targets. Do not relabel a production target to bypass a source gate.

The privileged host executor must:

- Have no network listener; authenticate local peers and restrict its socket.
- Expose only fixed, typed operations needed by this release.
- Revalidate targets, policy, artifact identity and preconditions independently.
- Resolve paths from root-owned policy; never execute API-controlled scripts or
  trust files merely because they reside under a familiar directory name.
- Execute only reviewed installed runtime artifacts, not a writable development
  checkout. Record/version the adapter contract and verify its provenance.
- Use explicit executable arguments, controlled environment and bounded output;
  never shell interpolation, arbitrary sudo, generic systemctl or pod exec.
- Maintain existing shared operation locks and durable recovery state.

Keep helper policy and recovery state root-owned. The API database must not be
authoritative for privileged authorization, fencing or recovery. Bind helper
requests independently to an operation ID, validated payload hash and execution
identity. Modifying an API record must not forge or broaden privileged work.

Do not apply identical systemd sandbox settings blindly to server and helper.
The GPU checks require a complete host process/DRM view; a private /proc or
device namespace can make them invalid. Keep the API restricted, grant only
the helper visibility it needs, and test failure when visibility is incomplete.
Do not weaken the underlying host security policy to make a helper check pass.

## Operation semantics and persistence

Use typed plans and operations rather than a remote wrapper around all CLI
arguments. An apply request references a validated plan, target and source
revision. Recheck mutable preconditions immediately before execution.

Persist operation intent before dispatch. Record actor, action, target,
configuration/artifact revisions, timestamps, phases, result and recovery
requirements. Bound record/log retention and redact sensitive output.

Long operations return an operation ID. Implement idempotency keys with
payload-conflict detection, optimistic concurrency and bounded execution queues.
Serialize conflicting actions across API, CLI, worker and helper boundaries.

Separate queued, running, succeeded, failed, cancel-requested, cancelled and
recovery-required states as needed. Cancellation is a request, not proof that a
process stopped. HTTP disconnects and timeouts do not implicitly cancel work.

On restart, reconcile outstanding operations against the actual executor and
resource state. Never blindly retry a partially executed mutation or claim
exactly-once external side effects. Avoid trusting a recycled PID. Preserve an
uncertain operation as recovery-required when its outcome cannot be proved.
Rollback is an explicit validated operation, not a promise of atomic reversal.

Expose liveness separately from K3s availability. If storage/audit persistence
needed for safe mutation is unavailable or full, refuse new mutations. Keep a
bounded diagnostic path without marking unfinished operations successful.

## Development, packaging and documentation

Deliver a fixture-backed development mode runnable without root, K3s, AMD GPUs,
private endpoints or the installer being installed. Use a separate state
directory and unmistakable demo labeling. Demo mode must not contact real
adapters or turn into live mode implicitly.
Select adapter mode at process startup under local administrator control, not
through the API. Separate demo/live state, credentials and sockets. Bind demo
mode only to loopback and never fall back to fixtures after a live probe fails.

Also implement the real adapters. Missing workstation hardware justifies
unrun physical tests, not replacing live behaviour with permanent stubs.
Do not execute large model downloads or workstation build recipes as ordinary
test fixtures. Building and testing Bridge itself is required.

Provide:

- README with local demo and GoLand development instructions.
- API contract and repeatable generation/validation commands.
- Architecture/decision notes covering configuration ownership, trust boundaries,
  privilege separation, adapter compatibility and restart recovery.
- Example private configuration, TLS/credential setup and scoped Kubernetes RBAC.
- systemd service/socket definitions with distinct server/helper permissions.
- Local packaging/build targets for Linux amd64 server/helper and macOS/Linux
  CLI. Document any platform/CGO restrictions; cross-building is not runtime
  validation.
- Safe installation, upgrade, state-backup and recovery procedures. Generate
  installation artifacts but do not install them during this coding task.
- Fixture demo, tests, dependency/security checks and source-only CI without
  publishing/deploying or requesting secrets.
- A target-machine qualification checklist with exact commands, prerequisites,
  expected observations and recovery steps.

Keep OpenTelemetry/Elastic/Prometheus deployment out of scope. Provide structured
redacted logs and operation audit events; permit later instrumentation without
building a second monitoring stack. Do not log prompts, private corpora or
credential-bearing client bundles by default.

Do not implement billing, public signup, quotas for paying customers, payment
providers, an inference gateway, a model marketplace, HA or multi-node scheduling.
Do not expose disk partitioning, firmware flashing, Secure Boot key generation,
kernel promotion or arbitrary backups/retention deletion through this API.
Document them as existing host-administration boundaries, not missing CRUD.

## Verification and acceptance

Run focused checks during implementation and the complete applicable checks at
the end. At minimum, run formatting verification, go vet, go test and supported
race tests, build targets and dependency vulnerability checks. Pin auxiliary
tools. Do not auto-upgrade unrelated dependencies or suppress findings.

Add contract and integration tests that exercise the actual API and CLI, not
only isolated handlers. Validate the generated OpenAPI contract and manifests.
Kubernetes fake clients do not reproduce admission, scheduling or RBAC: add
appropriate local validation or label those checks as target-only. Do not use
the developer's kubeconfig for tests.

Exercise these cases with controlled fixtures and fault injection:

1. Fresh startup, local owner bootstrap, login/logout, credential expiry and
   revocation, denial of bootstrap/recovery to another local UID, and refusal of
   unsafe network configuration.
2. Viewer/operator denial of administrative actions; CSRF, host/origin checks,
   request bounds, credential redaction and malformed input.
3. Matching CLI/UI/API validation, an exact change preview, successful apply,
   source drift between plan/apply, conflicting concurrent edits and recovery.
4. Two/four DIMMs, unknown topology, insufficient RAM, SMT constraints, duplicate
   GPU models, changed enumeration and unsupported physical-device selection.
5. Repeated profile requests, competing direct CLI/API requests, occupied GPUs,
   incomplete /proc visibility, failed starts and bounded handover timeouts.
6. Disconnect/cancel/restart during each external-effect boundary, including a
   worker/helper completing after the API disconnects. No duplicate execution
   or invented success; inspect durable recovery state.
7. K3s outage, insufficient RBAC, replaced Deployment UID, expired qualification
   and stale hardware boot identity.
8. Interrupted or corrupt downloads, disk-full conditions, path traversal,
   symlinks, disallowed redirects and attempted credential forwarding.
9. Named build cancellation, resource limits, gaming inhibition, untrusted
   recipe rejection and bounded output. Test prohibited credential reads,
   network access, path escape and surviving child processes.
10. Native harness bundle integrity, secret-free export and unsupported-mode
    refusals.
11. UI browser flows: authentication, draft edit, plan/apply, profile transition,
    failure/recovery and reconnect after refresh. Use browser tooling when
    available; do not call handler tests browser validation.
12. Systemd definitions and helper visibility on a compatible Linux environment,
    plus macOS CLI builds. Label unavailable platform checks explicitly.
13. Tampered API state cannot authorize privileged work; the helper retains
    authoritative recovery state and continuous transition locking.
14. Demo/live isolation, refusal of API-driven adapter changes and no fallback
    to fixtures after a live dependency failure.

For changed installer files, run their focused tests and its required check
target. Preserve unrelated edits and review both final diffs, including new
untracked files. Do not manufacture a clean Git baseline.

Completion requires an integrated fixture demo and real implementation paths
for the listed supported workflows. Unsupported source capabilities must have
explicit reasons and tested refusals. Do not use the demo or unavailable
hardware as an excuse to omit ordinary API, CLI, UI, authentication or adapter
implementation.

Never claim ROCm/model performance, GPU handover, encoding, P2P, ECC stability,
memory bandwidth or workstation safety from CI. Mark unperformed checks:
NOT RUN — target hardware unavailable, with the exact owner qualification path.

Finish with:

- Implemented capabilities and concrete paths in each repository.
- Commands actually run, outcomes and all skipped/blocked checks.
- Exact demo, server, CLI and UI usage, including local credential bootstrap.
- Private network/VPN configuration examples with no public management route.
- A realistic example from model configuration through plan/apply and operation
  inspection, and an AI/gaming transition with failure recovery.
- Configuration ownership/migration and adapter version requirements.
- Known limitations and commercial-readiness work deliberately not implemented.

Do not finish with only a plan or an offer to implement the remaining scope.
If a necessary external decision truly blocks a feature, report it precisely,
complete unrelated work and distinguish partial completion from success.

## Primary evidence to verify during implementation

Resolve concrete versions against the current project and upstream sources.
Record the verification date, exact revision/version and relevant mechanism.
Do not treat a rolling documentation page as an immutable compatibility pin.

- Go HTTP/TLS and graceful shutdown:
  [net/http](https://pkg.go.dev/net/http).
- Go dependency security and vulnerability checks:
  [Go security guidance](https://go.dev/doc/security/).
- Kubernetes client version semantics and compatibility:
  [client-go](https://github.com/kubernetes/client-go#compatibility-matrix).
- Host-resident Kubernetes client authentication:
  [out-of-cluster example](https://github.com/kubernetes/client-go/tree/master/examples/out-of-cluster-client-configuration).
- K3s admin kubeconfig privilege boundary:
  [cluster access](https://docs.k3s.io/cluster-access).
- Least-privilege resource authorization:
  [Kubernetes RBAC guidance](https://kubernetes.io/docs/concepts/security/rbac-good-practices/).
- Management network separation, endpoint authorization and audit:
  [OWASP REST security](https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html).
- API specification and tooling compatibility:
  [OpenAPI publications](https://spec.openapis.org/).
- systemd sandbox, credentials, service and socket semantics: use the installed
  target manual and matching upstream source. Verify options against the target
  release before generating units.
- Model, ROCm, serving-engine and harness contracts: start from the installer's
  locks and source register, then verify their exact upstream revisions.

External documentation supplies evidence, not authority to install software,
change security controls, retrieve credentials or mutate a live workstation.
