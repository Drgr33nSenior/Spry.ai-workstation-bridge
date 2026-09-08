# September 2026 remediation and migration

Review baseline: `264e09e`, inspected on 2026-09-08. The five fixes change
publication permissions, recovery admission/inspection, delegated cgroups and
browser sessions. They do not qualify GPUs or change installed systems.
See [VERIFICATION.md](VERIFICATION.md) for observed checks and exclusions.

## Upgrade prerequisites

Use the normal reviewed installation procedure in [OPERATIONS.md](OPERATIONS.md).
Inspect operations first. Stop API admission, let the helper finish its bounded
operation, then stop all writers before replacing binaries or backing up state.
Do not terminate a GPU handover to make an upgrade faster. Preserve the API,
helper and worker journals, original legacy session snapshot and root policy.
No installer change or adapter protocol version bump is required by these fixes.
Install the fixed controller, helper and worker together; do not mix recovery
implementations from this change and the vulnerable baseline.

Store schema remains 1. Existing `recovery_id` links identify a chain; no journal
rewrite or deletion is needed. A missing parent, cycle or cross-target link is a
refusal requiring owner investigation. Unlinked records remain separate fences.
Retention now preserves ancestors needed by retained recovery attempts.

The browser session format gains a generation binding. Pre-upgrade sessions are
not accepted, including when their old token is placed in the new cookie name.
Users must sign in again; CLI credentials are unchanged. Expired records use the
existing retention mechanism. Older binaries must not be used to bypass this
invalidation. If a credential or old session was exposed, use the stopped-service
local credential recovery procedure, not just a browser refresh.

## Private browser migration

`deployment/server.local.example.json` is now **CLI-only** in live mode:
`browser_sessions` is false. Its loopback HTTP bearer API remains available,
but browser pages/login and cookie authentication are refused. Omission of the
new setting also disables live browser sessions. Demo-only HTTP still works;
never enter a live management credential into a demo or another local service.

For browser management, review `deployment/server.vpn.example.json` and set:

```json
{
  "listen": "127.0.0.1:8743",
  "external_url": "https://bridge.internal.example:8743",
  "allowed_hosts": ["bridge.internal.example:8743"],
  "tls_cert_file": "/etc/bridge/tls/server.pem",
  "tls_key_file": "/etc/bridge/tls/server-key.pem",
  "browser_sessions": true
}
```

Merge these fields into the reviewed complete live configuration. Resolve the
dedicated hostname to loopback, or bind only the already reviewed VPN interface.
Certificates, name resolution and client trust are owner-provisioned outside
Bridge. The certificate SAN must identify that hostname. Do not use IP literals,
`localhost`, a wildcard bind, public ingress or an insecure client TLS option.
Use a scoped credential file and the existing CLI context `ca_file` when a
private CA is required. Restart the controller after a policy change.

Live cookies use `__Host-bridge_session_v2`: Secure, HttpOnly, SameSite=Strict,
host-only and Path=/. The `__Host-` prefix requires Path=/ and no Domain attribute.
Demo cookies use `bridge_demo_session_v2`. The old `bridge_session` cookie is
ignored and expired on logout. Cookies are **not isolated by port**. Every HTTPS
service at the management hostname is in the same cookie trust boundary; do not
host untrusted applications there, even on another port. A plaintext unrelated
loopback service must not receive the live Secure cookie. This follows the
[RFC 6265 weak-confidentiality boundary](https://httpwg.org/specs/rfc6265.html#weak-confidentiality),
checked 2026-09-08. HTTPS is not a claim of port isolation.

## Published model permissions

The service keeps `UMask=0077`. A private setgid mode-0700 partial container
contains the verified snapshot until atomic publication. Published model-ID,
revision and manifest-listed nested directories use 2750; model files and the
receipt use 0640. All retain the reviewed model-root group. No arbitrary recursive
chmod/chown, group change, symlink traversal or broad path permission change is
exposed. The root helper still independently verifies qualification.

To identify an older incorrectly permissioned snapshot, submit `model.verify`
for its selected model ID. To repair, submit a reviewed `model.stage` plan for
the same ID/revision. Repeated staging validates the complete receipt, exact tree
and hashes before repairing only those managed paths; it does not redownload an
already valid snapshot. Invalid receipts, changed files, unexpected files or
wrong groups refuse repair. Preserve them for owner investigation.

The isolated Linux cross-UID check passed under `0077` with synthetic identities and tiny
files: the reader group opened nested published files and received EACCES on
private partials. Repeat on an authorized disposable Linux environment only,
as root so the test can start synthetic UIDs (never against a real model root):

```sh
(umask 0077; BRIDGE_STAGING_CROSS_UID=1 env GOTOOLCHAIN=local CGO_ENABLED=0 go test ./internal/adapters -run '^TestStagingCrossUIDReaderQualification$' -count=1 -v)
```

Installed filesystem ACLs, workload group mapping and PVC access still require
target qualification; mode bits alone do not establish those boundaries.

Use the current target and revision from `bridgectl config` in a local draft:

```json
{
  "action": "model.stage",
  "target": "REVIEWED_TARGET",
  "source_revision": "REVISION_FROM_CONFIG",
  "model": "SELECTED_ID_FROM_MODELS"
}
```

```sh
bridgectl --context /absolute/private/context.json models
bridgectl --context /absolute/private/context.json config
bridgectl --context /absolute/private/context.json plan --file model-repair-draft.json
bridgectl --context /absolute/private/context.json apply --plan PLAN_ID --target REVIEWED_TARGET --idempotency-key model-permission-repair-1
bridgectl --context /absolute/private/context.json --deadline 30m wait OPERATION_ID
```

Review the plan before apply; hashing large files can take time. If an earlier
stage is uncertain, first use its operation-specific recovery below. Retained
private partials count against storage budgets and are not automatically deleted.
If a published receipt is missing or malformed, restore only that exact snapshot
from an owner-verified backup with matching manifest/hashes, through stopped-writer
host maintenance. Do not fabricate a receipt or widen permissions to clear a fence.

## Recovery and baseline session restoration

All IDs below are returned by the API; they are not filenames or executor choices.
Set the context to the reviewed management endpoint and confirm the exact target.
For A requiring recovery and B failing to restore A, inspect both records:

```sh
bridgectl --context /absolute/private/context.json operations A_OPERATION_ID
bridgectl --context /absolute/private/context.json operations B_OPERATION_ID
bridgectl --context /absolute/private/context.json recover B_OPERATION_ID
bridgectl --context /absolute/private/context.json apply --plan C_PLAN_ID --target REVIEWED_TARGET --idempotency-key recovery-chain-C
bridgectl --context /absolute/private/context.json --deadline 5m wait C_OPERATION_ID
bridgectl --context /absolute/private/context.json operations
```

C can reference A or B in the same unresolved chain. A duplicate submission uses
the same plan/key; a new attempt needs a new plan/key. Competing retries and
unrelated unresolved operations are blocked. The helper independently validates
its own links under root policy. It holds the existing session lock through
external effects, build-gate disposition, C's durable result and settlement.
Only successful restoration resolves the chain; A and B remain failed history.
If settlement persistence fails after C is durable, the helper restarts by
finishing journal settlement under the same legacy lock, not by repeating the
host action. Status remains `recovery-settlement-pending` until that settlement
is durable. Restore storage availability and restart the helper through the
owner's reviewed service procedure before inspecting C again; do not submit
another restoration merely to clear a persistence fault.

For local staging/verification, recovery inspects the original catalog revision,
publication receipt, hashes and partial/publication state without K3s. Proven
publication or proven interrupted non-publication can settle the local outcome.
An active local task, unreadable evidence or source drift cannot. Worker recovery
uses the worker journal/status, never a root-helper restore ID. A confirmed
terminal worker result can settle the API record without repeating compilation.
A readable exact cgroup reporting `populated 0` can establish that an uncommitted
job failed, not that its artifacts succeeded or became qualified.

When `recover` finds durable completion it records that result and returns a
refresh-required conflict; inspect operations again. Otherwise it returns a
validated `operation.reconcile` or qualified `profile.restore` plan, or a bounded
refusal. Plans and manually submitted drafts use the same checks. Before-dispatch
source failures require the source to match one side of the interrupted update.
Source state alone never proves a dispatched host/build/model outcome.

For an unavailable worker, the owner must restore access to its original journal
and exact configured cgroup hierarchy. Inspect only the named job's journal,
`cgroup.events`, `cgroup.procs`, worker service state and bounded build log.
Missing, unreadable or populated cgroups remain uncertain. Do not create an empty
replacement cgroup, trust a recycled PID, delete journals or start another build
to manufacture termination evidence. If the original evidence cannot be recovered,
keep that operation fenced pending owner investigation; this release has no
force-clear endpoint.

Restoring the saved AI/gaming baseline is the qualified `profile.restore` above,
not restoring an old API database. Resolve occupied devices, visibility, boot or
qualification failures first. The original session snapshot remains authoritative.
Do not downgrade to unsafe cookies, modes or recovery admission as a rollback.

## Disposable Linux cgroup qualification

**NOT RUN — an authorized writable delegated kernel subtree was unavailable.**
Source fixtures do not prove kernel enforcement. The implementation checks the
worker-owned cgroup v2 root, empty parent `cgroup.procs`, the current worker in
`supervisor`, available cpu/memory/pids controllers, enablement readback and each
child limit before process launch. It modifies no ancestors. Missing delegation
is a refusal; it never starts an unbounded build.

Mechanisms checked 2026-09-08: [systemd delegation at ce04f8a](https://github.com/systemd/systemd/blob/ce04f8a331a54ea6c15d602b56791dbfa4785b70/docs/CGROUP_DELEGATION.md#delegation)
and [kernel cgroup v2 semantics at 28924df](https://github.com/torvalds/linux/blob/28924df2a08f440c73991b83028032c901de2ae4/Documentation/admin-guide/cgroup-v2.rst).
These source revisions are evidence, not target release pins. Check
the installed target kernel/systemd manuals before the following owner-run test.

Prerequisites: create a disposable delegated subtree through the owner's normal
systemd process, named `bridge-cgroup-test-*`, with `Delegate=cpu memory pids`,
`DelegateSubgroup=supervisor`, no unrelated processes, and the test shell in that
supervisor subgroup. Do not reuse the installed worker subtree. From that shell:

```sh
systemd --version
uname -r
cat /proc/self/cgroup
BRIDGE_CGROUP_TEST_ALLOW=disposable-delegated-subtree BRIDGE_CGROUP_TEST_ROOT=/sys/fs/cgroup/OWNER_DELEGATION/bridge-cgroup-test-001 env GOTOOLCHAIN=local CGO_ENABLED=0 go test ./internal/worker -run '^TestLinuxDelegatedCgroupIntegration$' -count=1 -v
```

Replace only the root path with that exact authorized disposable subtree. The
test enables controllers there, reads back 64 MiB RAM/no swap/256 PIDs/one CPU,
starts a trivial shell plus sleep descendant, kills only its child cgroup and
checks descendant termination. It removes only that empty test child. On failure,
preserve observations and let the owner dispose of the dedicated test unit;
never loosen host controllers, sandbox policy or service permissions.

## Reproduce the review baseline without restoring unsafe code live

This source-only command extracts a separate temporary copy. It does not change
the working tree, index or journals, and must not be installed:

```sh
baseline_dir="$(mktemp -d /tmp/bridge-review-baseline.XXXXXX)"
git archive 264e09e | tar -x -C "$baseline_dir"
(cd "$baseline_dir" && umask 0077 && env GOTOOLCHAIN=local CGO_ENABLED=0 go test ./internal/adapters -run '^TestStageVerifyAtomicAndCorrupt$' -count=1)
```

Expected baseline result: FAIL because the published file is 0600. The same test
passes in the fixed tree. Keep backup restoration separate: with all writers
stopped, validate a matching backup in an isolated directory and compare executor
evidence with actual host state before any owner-controlled restoration. An old
database cannot undo newer external effects. Never delete the audit trail.
