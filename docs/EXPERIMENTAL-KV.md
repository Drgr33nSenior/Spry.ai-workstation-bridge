# Experimental UltraQuant-derived RDNA4 candidate

The optional candidate is source-only and **unqualified**. It is absent from
default profiles and baseline runtime pins. No measured speedup, gfx1201 build,
GPU numerical result or end-to-end serving qualification is available yet.
Bridge remains a CGO-disabled control plane, not an inference proxy or kernel
host. The build worker retains its no-GPU sandbox.

## Implementation boundary

The durable experiment is the sibling directory
`/Users/uk-gr9yjx0l0y/GolandProjects/Spry.ai-ultraquant-rdna4`.
Its [README](../../Spry.ai-ultraquant-rdna4/README.md) and
[codec specification](../../Spry.ai-ultraquant-rdna4/docs/CODEC.md) describe
the implementation, attribution, source identities, exact limits and build/test
procedure. `patches/source.json` binds the actual source tree and generated
`patches/sglang-ultraquant.patch`; the retained upstream revision is
`0b3bb0cbe31873994c9f989fddfe2f87ca839fdd`.

The runtime owns packing, post-RoPE Q/K rotation, compressed device allocation,
append/reuse and fused GPU attention. Its candidate is FP16 software decoding
plus gfx1201 WMMA, not the paper's native CDNA4 scaled-MFMA path. No author kernel
code was reused and no bit-exact reproduction is claimed. See
[UltraQuant section 5 / appendix A](https://arxiv.org/html/2606.20474v3) and
[AMD's RDNA4 WMMA guide](https://gpuopen.com/learn/using_matrix_core_amd_rdna4/).

Initial scope is unquantized `LlamaForCausalLM`, single GPU/TP1, causal full
attention, FP16, page size 1 and bounded shapes. The selected Qwen3.5/3.8
workstation models have recurrent layers and are explicitly refused. The
dual-GPU FP8-weight model is also outside scope. Existing llama.cpp builds are
unrelated and do not establish SGLang support.

## Evidence and selection

The installer remains the canonical evidence producer and comparison authority.
Its SGLang observer reads a private receipt emitted by the patched runtime only
after real compressed append and attention execution. It binds the live worker
and launcher identities and mapped library. It does not import an owner's
descriptor as proof that GPU execution occurred.

The optional `experimental_kv` descriptor in observed runtime JSON binds the
source revision/tree, patch, binary, compiler, dependencies, codec and effective
configuration hashes. It separates requested mode, observed mode and
`qualification: unqualified`. It always requires restart. Missing or malformed
candidate identity is refused; ordinary profiles without it retain existing
behaviour. An unbuilt source descriptor cannot establish observed execution.

Bridge validates this descriptor within the existing sealed comparison and
profile-selection workflows. It adds no API kind, arbitrary path, environment
map, command or GPU endpoint. Owner selection remains a configuration **export**,
not deployment, restart or qualification. Existing locks, GPU-holder visibility,
operation journals, cancellation, recovery snapshots and failure states remain
unchanged. Retained evidence stays private.

Changing runtime source, patch, binary, codec or effective settings invalidates
candidate identity. Existing model, tokenizer, software, device, workload and
producer-condition bindings still apply. A fresh observation is needed for
status; old results must not be relabelled. Follow [PERFORMANCE.md](PERFORMANCE.md)
for sealing, private import, explicit owner selection and retained results.

## Separately authorized qualification procedure

Source tests and compile-only work do not authorize GPU runs. Obtain separate
owner authorization for the exact device, runtime build, model, workload and
time window. Preserve the unchanged baseline image/model locks, recovery
snapshots and independent recovery access. Do not run the installer or deploy
from a performance selection export.

1. With prepared tools only, verify source/archive hashes, compile for gfx1201,
   inspect generated WMMA code, then run the experiment's explicitly gated
   synthetic GPU tests. Record actual compiler/dependency/driver revisions,
   library hash and every numerical failure. Stop the GPU branch on failure.
2. Use the same owner-selected, supported unquantized Llama model revision and
   matched workload for unchanged baseline and candidate. Start fresh sessions
   between representations. Keep baseline full-precision KV and, only where
   independently supported by that exact runtime/device, an existing quantized
   KV baseline. A dtype name is not support evidence. Never substitute a Qwen
   model that the candidate refuses.
3. Use the established owner-run serving/kernel harnesses. Prevent AI/gaming
   transitions during their controlled measurement window; do not weaken the
   existing lock permissions or give the build worker GPU access. Collect live
   source/process evidence for each run. For the separately reviewed KV-only
   numerical comparison, use the canonical `workstationctl rocm kernel-quality`
   operation with `--experimental-kv` and owner-reviewed tolerances. Ordinary
   comparison remains strict; the new mode is not a general image-change waiver.
4. Repeat labelled cold-start, warm/reused, short-context and cache-pressured
   multi-turn cases. Include initial prefill, decode, continuation prefill,
   shared-prefix hits, eviction/reuse and repeated prompt processing. Record
   startup, time to first token, inter-token latency, throughput, tail samples,
   failures and prompt/token counts. Do not discard slower candidate runs.
5. Measure actual allocation and peak temporary memory **per GPU**, plus host
   RSS and reserved memory. Keep no-swap and host-reserve requirements. Packed
   byte arithmetic excludes activations, workspaces and allocator overhead;
   it is not measured free capacity. GPU power is not wall power.
6. Check numerical error, long-context retrieval and representative coding
   quality on matched deterministic cases and seeds. Use the existing approved
   evaluator; do not replay real agent tool actions or execute generated code
   outside it. Include incomplete and refused cases. Missing evaluation is not
   a quality pass, and sampled numerical similarity is not qualification.
7. Exercise cancellation during prefill and decode, restart, cache discard and
   recovery to baseline. Observe process ownership, GPU holders, journals and
   durable failure states. A poisoned native session requires restart; no
   ordinary-KV fallback or mixed-format cache is permitted.
8. Seal the original producer evidence, compare with declared KV-cache changes,
   and review all differences and practical/noise/regression thresholds. Only
   then consider an explicit selection export. Independently qualified
   promotion remains a separate existing workflow and authority boundary.

Rollback is a stopped-session switch to the unchanged baseline followed by
fresh ordinary cache allocation. Retain candidate source, failed evidence and
recovery artifacts. There is no persistent cache migration or Bridge database
migration, and no automatic deletion or deployment operation.

## Remaining milestones

CPU codec/oracle and runtime admission/metadata tests can run without Torch or a
GPU. HIP compilation and instruction inspection need the missing prepared ROCm
tools. Actual gfx1201 numerical execution, native dependency compatibility,
end-to-end Llama scheduling/serving, cancellation/recovery and owner quality/
performance qualification remain NOT RUN. Hybrid Qwen cache integration and
multi-GPU support are separate runtime work, not hidden configuration switches.
