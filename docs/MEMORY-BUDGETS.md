# Evidence-based SGLang host-memory candidates

Bridge imports sealed owner evidence and exports an **unqualified** memory
candidate. It does not collect a live experiment, resize a Pod, restart serving,
edit defaults, qualify a model or approve its own recommendation.

The installer baseline requests and limits 38 GiB of host RAM. Its 16 GiB shm
ceiling is inside that limit, not another 16 GiB allocation. Neither is measured
consumption. `.80` is a GPU-memory fraction. Two GPUs do not pool host RAM or
form one transparent VRAM allocation. Discover memory and topology again after
a DIMM change; do not infer bandwidth or a smaller safe budget from averages.

## Authorities and compatible inputs

The installer owns collection, workload validation and deterministic candidate
generation. Bridge owns authorization, source revisions, approval, operation
records, capacity checks and artifact export. The optional agent explains only
sanitized evidence and can create an export plan for the owner to review.

The new helper actions are `memory.evidence.import` and `memory.plan.export`.
Both use the existing root journal and continuous legacy session lock. A failed
or interrupted read-only operation does not require GPU restoration. Private
files remain under `/var/lib/bridge-hostd/memory/OPERATION_ID`; the API journal
contains only sanitized summaries and artifact metadata. This is an additive
contract-v1/store-schema-1 change. Older helpers refuse the new actions; there
is no fallback. Upgrade the reviewed controller/helper together.

`memory_sources` is an optional administrator-owned host-policy map:

```json
{
  "memory_sources": {
    "ram-baseline-20260911": {
      "path": "/var/lib/bridge-memory-inbox/ram-baseline-20260911",
      "sha256": "SHA256_OF_REVIEWED_MANIFEST_JSON"
    }
  }
}
```

This fragment is not a complete policy or a valid digest. An authorized owner
must review the sealed source and provision its root-owned, owner-private tree.
All ancestors must be protected; every bundle inode must be private. The API
accepts the map key and exact digest, never its path. Up to 20 named sources are
permitted. Each file is at most 256 MiB; a bundle is at most 1 GiB. Retained
imports, failed partial copies and outputs share a four-GiB admission ceiling
and a free-space reserve. No cleanup or deletion API is provided.

The installed runtime must contain the exact reviewed `workstationctl`,
`versions.lock`, its existing closure, and `serving_memory.py`, `measurement.py`,
`performance.sh`, `serving.py`, `model_kernels.py`. Verify the package's source
and build manifests separately. Development-checkout hashes and reference
imports are not runtime approval. Never update installed approved hashes merely
to run changed code. The helper suppresses Python bytecode creation; its existing
systemd restrictions, UID boundaries and timeout stay unchanged.

## Owner-run collection and sealing

Prerequisites: authorized non-production target maintenance, the existing
non-root workstation user, readable Pod cgroup v2 counters, current hardware and
resource plan, dedicated scoped Kubernetes identity, independently qualified
baseline, and unchanged tokenizer-produced workload. Follow the installer's
`docs/PERFORMANCE-VALIDATION.md` and `docs/MODEL-KERNELS.md` first.

For each of two cold and two warm **fresh Pod UIDs**, run the current installer
commands. Replace uppercase placeholders with reviewed local values. Output
directories must be new. Do not use root for these observation commands:

```sh
workstationctl --config WORKSTATION_CONFIG rocm serving-startup EXACT_POD cold NEW_STARTUP_DIR --memory
workstationctl --config WORKSTATION_CONFIG rocm serving-evidence EXACT_POD NEW_EVIDENCE_DIR
workstationctl --config WORKSTATION_CONFIG rocm benchmark-serving WORKLOAD_JSON NEW_EVIDENCE_DIR NEW_SERVING_DIR
```

Use `warm` for the warm observations. Cache labels are owner declarations.
Preserve cache-preparation evidence; do not delete shared caches to manufacture
coldness. The startup allowance is 2100 seconds and serving can take longer.
The current root-helper timeout is capped at 1800 seconds. The API permits a
longer timeout (the examples use 3600 seconds), but that does not extend the
helper's allowance. The helper therefore does **not** run collection or truncate
it into an apparently successful sample. No timeout policy was changed here.

Prepare a new private bundle (`umask 0077` before creating/copying its files):

```text
bundle/
  deployment.json          # exact baseline Deployment, not only resources
  workload.json            # same tokenizer-produced workload for every run
  resource-plan.json      # current installer plan from observed hardware
  observations/
    cold-01/startup/{startup.json,memory.jsonl}
    cold-01/serving/{result.json,host-telemetry.jsonl}
    cold-01/serving/after/{runtime.json,pod.json}
    warm-01/...            # identical layout, distinct fresh Pod
    cold-02/...
    warm-02/...
```

Copy only those selected files; retain the full original collection separately.
Missing observation files and failed records can be sealed/imported as incomplete
evidence. They cannot justify a candidate. Do not synthesize success fields.

Export the current Bridge configuration and obtain its `revision`. Hash the
exact root-published hardware report used by the helper, and read its matching
`boot-id.txt` through authorized local access. These are evidence identities, not
credentials. Then run the client-local command:

```sh
bridgectl memory-seal --directory /ABSOLUTE/PRIVATE/bundle \
  --source-revision SOURCE_REVISION --hardware-sha256 HARDWARE_SHA256 --boot-id BOOT_UUID
```

This creates `manifest.json` exclusively, with per-file hashes and observation
IDs. It does not contact Bridge, OpenAI or Kubernetes. A failed seal retains its
files for inspection; do not overwrite its manifest. Use a new bundle for a
corrected input set and retain the original evidence. Review the manifest and
provision a protected copy through existing offline owner administration before
adding its map entry to host policy and restarting the helper normally.

## Import, plan, confirm and export

Create an owner-private draft with the exact current target/revision:

```json
{
  "action": "memory.evidence.import",
  "target": "REVIEWED_TARGET",
  "source_revision": "SOURCE_REVISION",
  "memory": {
    "evidence_id": "ram-baseline-20260911",
    "evidence_sha256": "SHA256_OF_REVIEWED_MANIFEST_JSON",
    "other_mib": 8192
  }
}
```

8192 MiB is an example, not a discovered budget. Count other Pods, telemetry,
WebUI/RAG, VMs and builds without double-counting host reserves. Bridge refuses
an other-workload allowance below its observed other-Pod requests. Missing
topology, CPU policy, cluster accounting or changed hardware prevents export.

```sh
bridgectl --context PRIVATE_CONTEXT memory-preview --file import-draft.json
bridgectl --context PRIVATE_CONTEXT plan --file import-draft.json
bridgectl --context PRIVATE_CONTEXT apply --plan PLAN_ID --target REVIEWED_TARGET --idempotency-key memory-import-001
bridgectl --context PRIVATE_CONTEXT --deadline 30m wait IMPORT_OPERATION_ID
bridgectl --context PRIVATE_CONTEXT memory
```

For candidate generation, create a second draft: set `action` to
`memory.plan.export` and `memory.evidence_id` to the successful **import operation
ID**. Keep its manifest digest, current source revision and reviewed other budget.
Repeat preview, plan, explicit target confirmation and wait with a new idempotency
key. Apply here authorizes generation/export only, never a resource change.
An export preflight reports `ready-for-plan`, with no computed candidate. Only
successful deterministic generation produces `plan-only-unqualified` artifacts.

The helper rechecks current baseline Deployment name/namespace/UID and complete
Pod template, qualified ConfigMaps, exact observed model-file hashes, effective
managed launch/settings, observation node, current boot/hardware,
resource accounting, sealed file hashes and installed tool provenance. The
planner requires complete matching serving cases, no restarted/duplicate Pods,
readable matching cgroups, pressure-free no-swap samples and final-sample coverage.
The default uses lifetime peak plus the full shm growth envelope, then at least
2 GiB/25% headroom rounded up to 256 MiB. This is conservative, not optimal.

Model-file comparison follows the existing helper qualification contract:
`.bridge-receipt.json` is non-authoritative and omitted from the qualification
file map. The collector includes its hash in the sealed runtime evidence; that
receipt never substitutes for the exact independently qualified weight hashes.

The selected collector does not record the CPU-offload launch option. Candidate
export with nonzero CPU offload is therefore unavailable; desired environment
values cannot fill that evidence gap. A separately reviewed collector contract
is required before supporting that case. Imported failed or stale data remains
available without a live cluster check, but it cannot establish a candidate.

The helper verifies the exact three-file private output tree and syncs each file
and its directory before recording successful generation. Storage failure leaves
the output retained and unexportable, not qualified or automatically retried.

Download **all three** files while baseline preconditions still match, before
testing a candidate. They contain the baseline Pod spec and may include secrets:

```sh
bridgectl --context PRIVATE_CONTEXT artifact EXPORT_OPERATION_ID --name plan.json --output /PRIVATE/CANDIDATE/plan.json
bridgectl --context PRIVATE_CONTEXT artifact EXPORT_OPERATION_ID --name patch.json --output /PRIVATE/CANDIDATE/patch.json
bridgectl --context PRIVATE_CONTEXT artifact EXPORT_OPERATION_ID --name rollback.json --output /PRIVATE/CANDIDATE/rollback.json
```

Each export rechecks identities and hashes. Do not send these files, raw runtime
records, environments, token arrays, traces or logs to a cloud agent. The Resources
page and `memory-summary.json` are separate sanitized views. Prometheus remains
advisory; it is not durable sizing evidence.

If the API disconnects, inspect the original operation instead of resubmitting:

```sh
bridgectl --context PRIVATE_CONTEXT memory-inspect EXPORT_OPERATION_ID
bridgectl --context PRIVATE_CONTEXT operations EXPORT_OPERATION_ID
```

Active/unknown helpers remain unavailable; successful independent completion can
be recorded later without redispatch. A failed planner retains inputs/partial
outputs, never promotes them, and cannot clear an unrelated GPU recovery fence.
If source or hardware changes, collect a new evidence set and create new plans.

## Candidate testing and rollback

Promotion is deliberately outside these two export actions. Use the existing
owner-approved maintenance and qualification process with the canonical session
lock, independent recovery access, retained baseline and scoped identity. Review
the candidate's whole-spec `test` before any apply. Do not mark a generated file
qualified to bypass the trial/qualification procedure.

Only after that maintenance authorization, with the canonical lock held by the
existing maintenance procedure, the candidate patch command is:

```sh
kubectl --kubeconfig OWNER_SCOPED_KUBECONFIG --context REVIEWED_CONTEXT \
  --namespace REVIEWED_NAMESPACE patch deployment REVIEWED_SGLANG_DEPLOYMENT \
  --type=json --patch-file /PRIVATE/CANDIDATE/patch.json
```

This changes a Deployment and may replace Pods. It is not a Bridge command or
authorization to activate an unqualified workload outside the controlled trial.
Do not run it during ordinary serving or bypass its baseline test.

At the smaller limit, repeat cold/warm startup, sustained memory, numerical and
coding correctness, latency and throughput checks. For memory-only numerical
comparison, after collecting both real result files:

```sh
workstationctl rocm kernel-quality BASELINE_RESULT CANDIDATE_RESULT NEW_QUALITY_JSON --atol 0.001 --rtol 0.001 --memory-only
```

These tolerances are an example requiring owner review. OOM, PSI, incomplete or
cancelled runs, correctness loss or latency/throughput regression prevent a
promotion recommendation. Regenerate installer compilation-worker resource plans
after a successful resize; old build allowances are not automatically retuned.

During separately authorized maintenance, the retained rollback can be passed to
the owner's scoped Kubernetes command, while the existing maintenance procedure
holds the canonical lock and controls stopped workloads:

```sh
kubectl --kubeconfig OWNER_SCOPED_KUBECONFIG --context REVIEWED_CONTEXT \
  --namespace REVIEWED_NAMESPACE patch deployment REVIEWED_SGLANG_DEPLOYMENT \
  --type=json --patch-file /PRIVATE/CANDIDATE/rollback.json
```

This command is **not** authorization or a complete handover procedure. Its
whole-spec test must pass; do not remove it after drift. Restore source and prior
qualification through the existing explicit maintenance process, then use Bridge's
qualified session recovery. Never delete journals or restart AI onto an occupied
GPU. Downloading artifacts does not execute this command.

## Optional Agents API advisory

This uses the actual [Agents API](https://developers.openai.com/api/docs/guides/agents-api/overview),
not the Agents SDK or Responses API. Its documented
[function flow](https://developers.openai.com/api/docs/guides/agents-api/tools/functions)
supports application-handled `required_actions` and submitted tool-result events.
Official Go examples use `openai-go/v3`; this narrow adapter uses typed standard-
library REST instead of adding an SDK dependency. Contract checked 2026-09-11.

Only a live administrator can enable `advisor` in server policy. It needs an
explicit model, `api_key_file` (a provisioned owner-only systemd credential file),
`timeout_seconds` 5..20, `max_tool_calls` 3..8, `max_sessions_per_hour` 1..60, and
`project_budget_acknowledged: true`. It is disabled by default and forbidden in
demo mode. Policy changes require a normal reviewed controller restart; this
implementation task creates no credential and changes no installed policy.

For CLI advisory use, put only `evidence_id` (the successful import operation),
`evidence_sha256` and `other_mib` in a private `memory-request.json`, then run:

```sh
bridgectl --context PRIVATE_CONTEXT memory-advice --file memory-request.json
```

An unavailable provider returns an explicit error. The same evidence remains
usable through `memory-preview` and the ordinary plan/export commands.

The application creates an idle `environment:none` session, then submits fixed
input. Only `read_memory_evidence`, `explain_memory_capacity` and
`request_memory_plan` exist, each with empty arguments and at most one execution.
The last tool creates a plan under the requesting owner's actor identity; it
cannot apply or approve it. Tool calls re-enter deterministic Bridge validation.
Provider output is untrusted explanation text. The UI reviews the locally
returned plan, not a plan ID or purported approval invented in model text.

No public callback, MCP server, PromQL, shell, Kubernetes credential or API
management credential is exposed. Outbound HTTPS uses only `api.openai.com`,
normal certificate verification and no redirect/environment proxy. Requests,
response bytes, history, tool calls, concurrency and duration are bounded.
Hourly admission is process-local and resets at restart.

**Unsupported:** the documented session-create contract has no per-session hard
token/dollar ceiling. Local bounds, reported usage checks and best-effort
cancellation do not provide one. The owner must separately provision the
provider's project spending control before acknowledging it; Bridge neither
queries nor changes organization administration. Cancellation acceptance is not
proof remote computation stopped. Sessions have provider retention; review its
data controls. No key, paid request, real account capability or spending control
was tested. Deterministic import/planning/CLI use requires no OpenAI account.
