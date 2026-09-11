# Evidence-based performance experiments

Bridge exposes private owner-reviewed analysis and selection exports. The
installer owns comparison, coding evaluation, loading/queue candidates and cache
planning. These actions do not deploy, restart inference, run generated code,
delete caches or qualify hardware. Retain the previous known-good workload and
independent recovery access.

## Capabilities and limits

| Work | Behaviour |
|---|---|
| Comparison | Versioned JSON/readable reports preserve declared differences, case labels, failed runs, unknown measurements, quality gates and practical/noise/regression thresholds. Different models are not equivalent-quality GPU scaling. |
| Selection | Separate owner action exports an eligible configuration and previous selection. It does not update `managed-source.json`. Changed relevant identities make its qualification stale. |
| Coding/tool quality | Ten versioned deterministic tasks; corpus/template/generation hashes and separate retries, latency and tokens. Structured/tool/refusal assertions run offline. Generated-code compilation is unavailable: the current named build worker is not an arbitrary-code evaluator. |
| Loading/queue | Bounded candidates require exact selected-image capability/source evidence. Unknown options and unsupported checkpoint layouts are refused. Defaults remain unchanged. Client interactive priority remains unsupported; no gateway or replay is added. |
| Warmth | Current inventory reports scoped Kubernetes health/loading separately from representative warmth. Installer `serving-warm-status` checks fresh Pod/process/model/runtime/device evidence against explicit warmup results. Historical reports cannot establish current readiness. |
| Inference cache | Managed Triton/Inductor inventory and exact prune plans protect active/last-known-good namespaces, check ownership and reject escapes/symlinks. No deletion endpoint or prune executor. |
| Telemetry | Optional installer metrics profile; full remains default. Component limits plus margin are planning allowances, not RSS or savings. Existing conservative reserve is retained. |

The root helper cannot invoke the installer's non-root warmup harness through
its current typed protocol. Bridge does not start extra warmup or infer it from
HTTP readiness. The existing non-root owner-run harness needs an explicitly
controlled no-transition window: it is not newly serialized with the root-only
session lock. Do not overlap it with AI/gaming changes. Automatic Bridge warmup
needs a separately reviewed non-root execution/lock protocol. Lock permissions
and existing Kubernetes probes were not weakened.

## Prepare a sealed bundle

Use the installer's `PERFORMANCE-PROFILES.md`, `MODEL-KERNELS.md`,
`PERFORMANCE-VALIDATION.md` and `TELEMETRY.md` for input/spec schemas. Collection
remains owner-run. Exclude prompts, credentials, raw environments and full logs.
Use `umask 0077` before creating a new private tree. `spec.json` selects only
typed relative inputs. Seal with the current Bridge **configuration SHA-256**,
not a Git commit or source-archive hash:

```sh
workstationctl performance seal PRIVATE_BUNDLE comparison EXACT_TARGET CONFIG_SHA256
sha256sum PRIVATE_BUNDLE/manifest.json
workstationctl performance inspect PRIVATE_BUNDLE MANIFEST_SHA256
```

On macOS use `shasum -a 256`. Inputs are limited to 256 files, 64 MiB per file
and 256 MiB total. All inodes must be private, regular and symlink-free. After
review, an authorized administrator provisions a root-owned private copy and
adds its exact ID/path/digest to the existing helper policy. This fragment is
not a complete policy or a valid digest:

```json
{
  "performance_sources": {
    "comparison-20260911": {
      "path": "/var/lib/bridge-performance-inbox/comparison-20260911",
      "sha256": "SHA256_OF_REVIEWED_MANIFEST_JSON"
    }
  }
}
```

All ancestors must be protected. Up to twenty sources are permitted. The API
accepts IDs/hashes, never these paths. Installed tool approvals must separately
cover the new analysis modules, coding corpus and existing runtime closure.
Reference import or source-test hashes cannot grant execution authority.
Policy reload still uses the established reviewed stopped-service procedure.
The example policy approves no sources.

Retained inputs/results use `/var/lib/bridge-hostd/performance/OPERATION_ID`.
They share a four-GiB admission ceiling and free-space reserve. Partial copies
and failed reports are retained; there is no automatic cleanup. Detailed
artifacts are private; bounded sanitized summaries accompany owner-only
operations. Completed report generation may describe a refused/incomplete
experiment: inspect the report status, not only operation completion.

Cache work additionally requires administrator `INFERENCE_CACHE_ROOT`; empty
defaults refuse inventory. `INFERENCE_CACHE_FREE_RESERVE_MIB` defaults to 20480.
Clients cannot override either. Registry ownership/references need review;
atime is not reliable managed-use evidence. Local-path PVC requests are not
enforced filesystem quotas.

## CLI and UI

Keep credentials in the existing private context file, never flags. Create a
private typed draft:

```json
{
  "action": "performance.export",
  "target": "EXACT_TARGET",
  "source_revision": "CURRENT_CONFIGURATION_SHA256",
  "performance": {
    "evidence_id": "comparison-20260911",
    "evidence_sha256": "SHA256_OF_REVIEWED_MANIFEST_JSON",
    "kind": "comparison"
  }
}
```

```sh
bridgectl --context OWNER_CONTEXT performance-preview --file PRIVATE_DRAFT
bridgectl --context OWNER_CONTEXT plan --file PRIVATE_DRAFT
bridgectl --context OWNER_CONTEXT apply --plan PLAN_ID --target EXACT_TARGET --idempotency-key UNIQUE_KEY
bridgectl --context OWNER_CONTEXT operations OPERATION_ID
bridgectl --context OWNER_CONTEXT performance-inspect OPERATION_ID
bridgectl --context OWNER_CONTEXT artifact OPERATION_ID --name comparison/report.txt --output NEW_PRIVATE_REPORT
```

Use the exact artifact name listed by the operation. The local download checks
its recorded size/hash and creates a new private file; it does not overwrite an
earlier report. Other bundle kinds expose their own typed artifacts.

The Performance page uses the same owner authorization, previews, exact target
confirmation and journal. Viewer/operator identities cannot inspect the reports.
Selection uses kind `profile-selection` and action `performance.profile.select`;
the sealed spec binds the comparison, candidate and previous selection.
`performance-select --file PRIVATE_DRAFT` creates a plan, not approval. Apply
still checks actor, expiry, target, drift and idempotency. Selection only exports
a configuration; qualified maintenance promotion remains separate.

Kind `profile-status` evaluates a retained selection against a newly sealed
observed identity. Missing/old observations yield unknown; model, runtime,
software, tokenizer, workload, launch or hardware changes yield stale. Status
is as of the observation, never an automatic qualification or background probe.

After disconnect/restart, `performance-inspect` queries the original helper. It
never redispatches or settles unrelated GPU fences. Preserve both journals and
retained inputs. Failed read-only analysis does not invent uncertain GPU effects.

## Compatibility and target checks

Contract v1/store schema 1 gain additive fields/actions; old helpers refuse them
without fallback. The selected installer release builder now requires both
named memory and performance tests against its exact source export. An unpaired
Bridge tag archive is not pair evidence. The retained known-good catalog fixture
is unchanged. No release pin or installed approved hash was changed here.

Before using a candidate on hardware:

1. Preserve the qualified baseline, rollback artifacts and independent recovery
   access. Rediscover boot, DIMMs, usable memory and device identity.
2. Collect repeated owner-labelled cold/warm fresh-Pod startup and complete
   serving/memory evidence. Never flush global caches or accept truncation.
3. Run numerical and coding-quality checks; unavailable code execution is not
   a pass. Verify loader flags in the exact image; bound threads across TP ranks.
4. Measure matched workload cases repeatedly, including failures, tail sample
   counts, startup, peak RAM/VRAM, pressure and device power (not wall power).
5. Separately test native overload/cancellation and AI/gaming interruption/resume.
   Do not replay real agent/tool actions. Recheck warmth after every new process.
6. Test both telemetry profiles and loss of collector-health delivery. Review
   actual overhead before regenerating resource/build budgets; allowance is not
   measured consumption.

Fixtures establish no speedup, safe RAM reduction, sandbox/cgroup enforcement,
GPU handover, encoding, cache quota or deployment readiness.

These commands are for the authorized non-root owner on the target, not this
development task. Replace placeholders with reviewed inputs, select a
no-transition collection window, and use a new output path each time:

```sh
workstationctl --config WORKSTATION_CONFIG rocm serving-startup EXACT_POD cold NEW_STARTUP --memory
workstationctl --config WORKSTATION_CONFIG rocm serving-evidence EXACT_POD NEW_EVIDENCE
workstationctl --config WORKSTATION_CONFIG rocm benchmark-serving WORKLOAD NEW_EVIDENCE NEW_SERVING
workstationctl --config WORKSTATION_CONFIG rocm kernel-evidence EXACT_POD NEW_KERNEL_EVIDENCE
workstationctl --config WORKSTATION_CONFIG rocm serving-loading-plan DEPLOYMENT NEW_KERNEL_EVIDENCE NEW_LOADING_PLAN --threads 1,2 --reserve-mib REVIEWED_RESERVE --per-thread-mib REVIEWED_THREAD_BUDGET
workstationctl --config WORKSTATION_CONFIG rocm serving-queue-plan DEPLOYMENT NEW_KERNEL_EVIDENCE NEW_QUEUE_PLAN --maximum-queued 8
workstationctl --config WORKSTATION_CONFIG rocm serving-warm-status EXACT_POD RETAINED_WARMUP_RESULT NEW_STATUS_JSON
workstationctl --config WORKSTATION_CONFIG rocm kernel-quality BASELINE_RESULT CANDIDATE_RESULT NEW_QUALITY_JSON --atol REVIEWED_ATOL --rtol REVIEWED_RTOL
workstationctl --config WORKSTATION_CONFIG telemetry render NEW_TELEMETRY_RENDER apps/overlays/dual-gpu
workstationctl telemetry plan CURRENT_RESOURCE_PLAN NEW_TELEMETRY_RENDER NEW_CAPACITY_PLAN
```

Repeat startup with `warm` on fresh Pods under the existing runbook. Do not apply
generated patches as part of these commands. Memory-only changes require the
existing repeated smaller-limit trials and retained rollback precondition.
Gaming/resume still uses the separately approved profile plan/apply and recovery
workflow in [QUALIFICATION.md](QUALIFICATION.md); recollect warmth after returning
to AI on a new process.
