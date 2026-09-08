# Bridge remediation: implementation-agent prompt

Work in `/Users/uk-gr9yjx0l0y/GolandProjects/Spry.ai-workstation-bridge`.
Verify the repository location, current revision and applicable `AGENTS.md`
instructions before proceeding. The review baseline is commit `264e09e`, reviewed
on 8 September 2026. Line numbers below refer to that baseline and may have moved.

Implement and validate fixes for the five findings below. An audit, proposed plan
or documentation-only change does not complete this task. First reproduce each
applicable defect, then make the smallest correct implementation change and add
permanent regression coverage. If a finding is already fixed, verify that fix and
its tests instead of duplicating it.

## Remit and boundaries

Bridge is the private management plane for an Arch Linux AI/gaming workstation.
The Go API and embedded UI run outside K3s. The CLI uses the API. A separate root
helper executes allowlisted host operations, and an unprivileged worker executes
named builds. Preserve these boundaries and the existing operation journal,
configuration authority, authentication, resource limits and legacy-session lock.

The reference installer is
`/Users/uk-gr9yjx0l0y/Projects/ArchLinuxThreadripperAI`. Consult its contracts when
necessary. Do not change it unless a demonstrated compatibility requirement
makes a narrowly scoped change necessary; document both sides of that change.

- Inspect Git status and preserve unrelated work. Do not commit, stage or push.
- Read the relevant architecture, host-executor, operations, qualification,
  verification and implementation-matrix documents, source and tests.
- Stay with Go, the existing web assets, standard-library runtime and current
  build/test tools. Do not introduce another service, framework or database.
- Do not deploy, run the workstation installer, modify disks or boot settings,
  start installed services, change a live cluster or access credentials.
- Keep tests isolated, with temporary state and synthetic credentials. Do not
  download full model weights or run full ROCm/image builds for this task.
- Preserve safe refusal when external effects are unknown. Do not clear recovery
  flags, delete journals, loosen permissions globally or weaken tests to pass.
- Do not add billing, public ingress, telemetry, frameworks or performance tuning.
- Continue through routine reversible code changes. Ask only for a missing choice
  that materially changes behavior and cannot be resolved from the repository.

## 1. Published model permissions under the service umask — P1

Evidence:

- `internal/adapters/staging.go:156` creates parent directories with `0750`.
- `internal/adapters/staging.go:299` creates model files with `0640`.
- `internal/adapters/staging.go:369` explicitly changes only the final revision
  directory's mode before publication.
- `deployment/systemd/bridged.service:16` sets `UMask=0077`.

Under that umask, new model files are `0600` and new parent directories lose group
traversal. A successful stage can therefore publish weights that the designated
workload reader group cannot access. The existing
`TestStageVerifyAtomicAndCorrupt` fails its reader-mode assertion under this umask.

Required behavior:

- Keep partial downloads inaccessible to workload readers until verification and
  atomic publication complete.
- Explicitly establish and verify the intended group-readable file modes and
  group-traversable directory modes on the managed publication tree, including
  nested files, required receipts and newly created model-ID parents.
- Preserve the reviewed reader-group identity, setgid inheritance where required,
  symlink protections, safe path handling, hash verification and durability.
- Do not change the service umask or grant access to unrelated paths or users.
- Define how an already-published snapshot with incorrect modes is identified and
  repaired. Any repair must remain within a verified managed snapshot and must not
  become an unrestricted recursive chmod/chown operation.

Tests must cover `0077`, the normal test umask, nested paths, fresh/existing
parents, partial-download privacy, publication failure and repeat staging or
repair. Use a subprocess for umask tests so parallel tests cannot change each
other's process-wide umask. Mode assertions do not prove cross-UID Linux access;
separately qualify actual access if a suitable isolated Linux environment exists.

Existing reproduction from the repository root:

```sh
(
  umask 0077
  env GOTOOLCHAIN=local CGO_ENABLED=0 go test ./internal/adapters \
    -run '^TestStageVerifyAtomicAndCorrupt$' -count=1
)
```

## 2. Failed restore chains must remain recoverable — P1

Evidence:

- `internal/engine/engine.go:229` compares a request's single recovery ID with
  every unresolved operation.
- `internal/hostexec/executor.go:236` repeats that restriction independently.
- The helper clears the original record only after a successful restore.

If operation A requires recovery and restore B also requires recovery, both
records remain unresolved. A retry referencing either A or B is rejected because
it does not identify the other record. A focused regression probe reproduced
this deadlock in the engine.

Required behavior:

- Represent or derive the relationship between an operation and its recovery
  attempts. Permit a validated retry of the same unresolved recovery chain.
  Reuse persisted `RecoveryID` relationships where sufficient, and update all
  admission, execution and helper checks, not only the `Recover()` convenience path.
- Keep unrelated unresolved operations blocked. Do not fix this by accepting an
  arbitrary recovery ID, ignoring all but the newest record or dropping fences.
- Preserve root-helper authorization, target confirmation, operation identity,
  idempotency and the canonical session lock independently of the API process.
  Retain the original recovery snapshot and keep the lock through external
  effects, build-gate disposition and durable recovery settlement.
- Mark a chain resolved only after durable evidence establishes the recovery
  outcome. Preserve the original failed operation and every attempt as history;
  do not relabel the original mutation as successful.
- Handle existing on-disk records safely. Document any journal/schema migration
  and behavior when linkage is missing or ambiguous. Never require journal deletion.

Add engine and root-helper tests for A failing, B failing to restore A, and C
successfully restoring the chain. Repeat across process restarts. Cover duplicate
requests, competing retries, cancellation/unknown effects, unrelated recovery
records and persistence faults while recording the successful recovery. Verify
that ordinary mutations resume only after the relevant recovery fence is resolved.
Distinguish preflight refusal from uncertain dispatched effects in these tests.

## 3. Select recovery by operation and executor — P1

Evidence:

- `internal/engine/engine.go:328` selects `profile.restore` for every unresolved
  operation whose dispatch flag is set.
- Local staging and verification do not have corresponding root-helper records.
- `internal/hostexec/executor.go:216` rejects a restore ID that does not identify
  an unresolved record in its own journal.

A daemon crash during a dispatched local model operation can therefore select an
inapplicable GPU restore and leave management mutations blocked. A regression
probe confirmed that an unresolved dispatched `model.stage` operation generates
`profile.restore`. Inspect the equivalent build-worker path as well.

Required behavior:

- Use the existing adapter/executor boundaries to inspect and recover operations.
  Reserve host/profile restoration for operations whose effects require it.
- For model staging, inspect verified publication receipts, hashes and partial
  state. Reconcile, resume or safely retry according to observed evidence. Do not
  claim a snapshot is valid because a destination directory exists.
- Reconcile builds through the worker's own durable records and process/cgroup
  evidence. Do not route worker-only recovery IDs through the root helper.
  Missing, unreadable or populated cgroups are not proof of descendant termination.
- Preserve the distinction between no dispatch, confirmed completion, active
  execution and uncertain effects. Never redispatch work that may still be running.
- If automatic recovery is unsafe, retain the fence and provide a bounded,
  operation-specific owner procedure. A generic instruction to delete state is
  not an acceptable recovery path.

Test crashes before dispatch, during staging, after atomic publication but before
the API records completion, during a worker build, and after executor completion
but before API acknowledgement. Include malformed/missing receipts, unavailable
executors, source drift, restarts and repeated recovery. Exercise the real adapter paths with
fixtures as well as the engine state machine. Assert that local asset recovery
does not invoke host restoration or require K3s when no cluster effect occurred.
Test manually constructed recovery requests as well as generated plans. Neither
local nor worker reconciliation may clear unrelated GPU recovery fences or use
source-configuration state alone to dismiss uncertain external effects.

Coordinate this fix with section 2 so both use one consistent recovery model.

## 4. Initialize delegated build cgroup controllers — P1

Evidence:

- `internal/worker/linux.go:142` creates a job cgroup, then immediately writes
  `memory.max`, `memory.swap.max`, `pids.max` and `cpu.max`.
- `deployment/systemd/bridge-worker.service` uses
  `Delegate=cpu memory pids` and `DelegateSubgroup=supervisor`.
- No initialization enables the required controllers in the delegated parent's
  `cgroup.subtree_control`.

Systemd delegation makes controllers available; it does not itself enable them
for the worker's child cgroups. A fresh delegated subtree can consequently fail
with `worker cgroup resource limit unavailable`.

Check the current primary documentation and applicable systemd/kernel semantics:
[systemd cgroup delegation](https://github.com/systemd/systemd/blob/main/docs/CGROUP_DELEGATION.md#delegation).
The original finding was source/documentation-backed, not reproduced on Linux.

Required behavior:

- Validate the worker-owned delegated subtree and available cgroup v2 controllers.
  Enable the required controllers there before creating limited job cgroups.
  Read back controller state and effective child limits; do not infer enforcement
  from a successful write alone.
- Respect the no-internal-process constraint and the existing supervisor subgroup.
  Do not modify ancestor cgroups, unrelated services or host-wide controllers.
- Keep initialization idempotent and safe under the worker's concurrency model.
- Fail closed with an actionable error if delegation, controllers or required
  limit files are unavailable. Never silently run an unbounded or swapped build.
- Apply limits before starting build processes. Preserve containment, cancellation,
  descendant-termination checks and safe handling of incomplete initialization.

Add tests for fresh/already-initialized subtrees, missing controllers, denied
writes, incorrect process placement, failed setup and repeated jobs. Filesystem
mocks cannot prove kernel cgroup behavior. Provide a small opt-in Linux integration
test using only a clearly authorized, disposable delegated subtree and a trivial
payload. Verify effective limits and descendant termination without a real build.
If that environment is unavailable, label the runtime check NOT RUN and give the
owner exact qualification instructions; do not invoke sudo or alter installed
services to obtain a passing result.

## 5. Isolate live browser management sessions — P2

Evidence:

- `internal/api/server.go:228` issues a host-scoped session cookie with `Path=/`.
  Its Secure attribute depends on the configured external URL.
- `deployment/server.local.example.json` permits live management over
  `http://127.0.0.1:8743`.
- `internal/api/server.go:231` returns session/CSRF metadata to an authenticated
  session; a stolen session is not rendered harmless by the CSRF mechanism.

An automatic-cookie regression probe sent the management session to a second
HTTP service on another loopback port. This was a Go cookie-jar reproduction, not
a browser exploit test. Browsers also do not isolate cookies by port; HttpOnly,
SameSite and a different port do not establish that boundary. Consult
[RFC 6265 section 8.5](https://httpwg.org/specs/rfc6265.html#weak-confidentiality).

Required behavior:

- Make supported live browser deployments use a dedicated trusted HTTPS
  management identity, separate from untrusted local applications, with Secure,
  HttpOnly, appropriately SameSite and narrowly scoped cookies.
- Treat demo-only HTTP separately. Preserve safe CLI-only loopback access where
  supported, without exposing browser sessions through that configuration.
- Preserve role checks, CSRF defenses, Host/Origin validation, credential expiry,
  revocation, logout and approved proxy/TLS trust boundaries.
- Fail unsafe live browser configurations clearly. Do not simply set Secure on an
  HTTP example and leave login broken, or claim HTTPS adds port isolation. All
  services that can receive the same cookie must remain within its trust boundary.
- Update examples, CLI/UI guidance and migration instructions. Explain any cookie
  name/configuration change and how old sessions are invalidated or expired.
  Do not retrieve credentials, issue live certificates or expose the controller.

Add configuration and API regression tests plus a real browser test with isolated
test credentials and local test infrastructure. Verify login/logout/expiry/CSRF,
safe CLI access, refusal of unsafe live browser settings, and absence of the
management cookie at an unrelated loopback service. Do not use blanket browser
certificate-validation bypasses as evidence that deployment TLS is safe.

## Verification and completion

The review observed `make check`, the browser demo and workflow lint passing.
Those checks missed these defects. Three temporary engine/API probes and the
existing staging test under the service umask failed. The temporary probes are
not committed project tests; recreate permanent regression tests from the
scenarios above rather than depending on a review-time `/tmp` path.

1. Record the current baseline and reproduce relevant failures before fixing them.
2. Run focused tests without cached results, including the restrictive-umask test.
3. Run `make check` and `make browser` with the repository's pinned toolchain.
4. Run `make package` to check packaging and cross-builds. Inspect generated
   configuration and service examples. Run workflow lint if workflows change.
5. Update canonical schemas/generators before generated artifacts if contracts
   change. Test compatibility with persisted records and existing clients.
6. Update only relevant operational, security, recovery and verification documents.
   Correct any completion claims affected by these findings.
7. Review the final diff. Report unavailable tools and skipped Linux/physical
   checks separately from passing checks. Do not label fixtures as hardware tests.

Finish with a concise table containing each finding, changed paths, regression
tests, observed results and remaining qualification. Include exact migration,
recovery and baseline-restoration commands, with prerequisites and warnings for
owner-run steps. Explicitly state that no live system was changed. Preserve the
audit history and do not recommend reverting to unsafe permissions, cookies or
recovery behavior as a rollback strategy.

Stop after these five fixes and their necessary validation/documentation. Do not
claim readiness for live GPU operation or a performance improvement from these
local checks.
