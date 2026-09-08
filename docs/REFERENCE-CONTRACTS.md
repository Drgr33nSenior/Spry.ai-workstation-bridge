# Reference and live adapter contracts

Verified 2026-09-08. The reference repository is
`/Users/uk-gr9yjx0l0y/Projects/ArchLinuxThreadripperAI`. It had no `HEAD` during
inspection. Source files were untracked, and IDE files were staged. File hashes,
not a fabricated clean Git revision, identify the imported source. Bridge does
not install or execute that development checkout. Its live helper requires a
separately reviewed, root-owned installed runtime manifest.

| Requirement | Existing source/contract | Bridge implementation | Tests | Hardware qualification |
| --- | --- | --- | --- | --- |
| Selected models | `versions.lock`, `docs/MODELS.md`, SGLang Kustomize literals | `internal/catalog/models.json`, literal lock import and drift refusal | Immutable inventory and parser tests | Model/image/kernel paths remain unqualified |
| Stage models | Chat: documented revision-specific `hf download` / `hf cache verify`; RAG: ten SHA-256 files and `rag.sh` | Streaming exact snapshots, public HTTPS origin/DNS checks, budgets, per-file digests, atomic publication | Interrupted/corrupt downloads, budget/full, traversal/symlink, redirect and credential-forwarding refusals | Storage mapping and serving startup on target |
| Serving/resources | SGLang `Recreate`, zero replicas, qualified exact image/model, Guaranteed CPU/RAM/GPU resources | Source plan/apply plus restricted helper; source committed and live applied are separate results | API/domain/helper tests | K3s admission, actual CPU Manager, DRM and readiness |
| Sessions | `session.sh`, root-owned shared policy, build inhibition and recovery snapshot | Helper retains the shared legacy lock through durable transition; API inspect reconnects | Helper/session tests and simulated failure/recovery | Same-boot GPUs, complete `/proc`, pod termination, Sunshine/Steam paths |
| HIP/Vulkan builds | `rocm.sh`: fixed llama source, fresh hardware, ccache, memory-heavy budget | Typed separate worker, root manifest/toolchain gates, network/device isolation and cgroup limits | Request/budget tests; Linux namespace probe when available | Native SDK, build flags, output checks and runtime qualification |
| Sunshine image | `sunshine image-build` and `infrastructure/gaming/Dockerfile` already exist; original builder fetches apt dependencies | Fixed offline Containerfile, staged OCI base/package closure, isolated rootless Buildah, local OCI artifact | OCI integrity and fixed recipe tests | Offline dependency closure and rootless image build remain target prerequisites; display/input/encode remain unqualified |
| CPU policy | `resources plan` validates complete SMT topology; exports JSON Ansible variables | Fixed helper export returns two bounded textual artifacts; no kubelet mutation | Export bounds, hardware/topology tests | Owner-reviewed maintenance only |
| Client harnesses | `agents.sh` schema-1 native bundle and integrity contract | Deterministic native export; explicit client-local launch preserves refusal rules | Full native structure, metadata, digests and unsupported modes | Installed client version and endpoint/tool behavior |
| TheRock/kernel | TheRock is an incomplete build plan; kernel packaging/promotion is host administration | Explicit reasons; no kernel promotion or invented TheRock success | Recipe refusal tests | Separate owner workflow |

## Immutable upstream evidence

The Hugging Face revision API returned these exact identities and full snapshot
file metadata on 2026-09-08. Only small public metadata was fetched during
implementation. Model weights were not downloaded.

| Model | Exact revision | Full snapshot bytes | Reviewed representation |
| --- | --- | ---: | --- |
| [Qwen3.8-27B-FP8](https://huggingface.co/Qwen/Qwen3.8-27B-FP8/tree/017b9c7af6b5689d5dd426a76e0bc077eb5ca20a) | `017b9c7af6b5689d5dd426a76e0bc077eb5ca20a` | 30,890,049,597 | Official serialized blockwise FP8, two GPUs |
| [Qwen3.5-9B](https://huggingface.co/Qwen/Qwen3.5-9B/tree/c202236235762e1c871ad0ccb60c8ee5ba337b9a) | `c202236235762e1c871ad0ccb60c8ee5ba337b9a` | 19,329,393,661 | BF16, one GPU |
| [Qwen3-Embedding-0.6B](https://huggingface.co/Qwen/Qwen3-Embedding-0.6B/tree/97b0c614be4d77ee51c0cef4e5f07c00f9eb65b3) | `97b0c614be4d77ee51c0cef4e5f07c00f9eb65b3` | 1,207,489,041 | CPU embedding candidate |

All three upstream cards declare Apache-2.0. The chat snapshots contain
`LICENSE`; the embedding snapshot carries its license declaration in `README.md`
and API metadata, without a separate `LICENSE` file. Retain the supplied files
and obtain the applicable terms before distributing weights. This does not
choose a licence for Bridge itself.

`models.json` records each file's size and upstream LFS SHA-256 or Git blob
SHA-1. Non-LFS verification includes the Git `blob <size>\0` header. Staging
also computes SHA-256 for every resulting file and records it in the receipt.
The existing RAG LFS hashes match the imported revision; its ten-file contract
is a subset of the full 12-file snapshot. No pickle or remote model code is run.

The serving image remains
`docker.io/rocm/sgl-dev:v0.5.15.post1-ubuntu24.04-py3.14-rocm10.0.0@sha256:51f63a2d201ca944a43b8f1a18ac7da73ed9f6743c45bb82bbc40b44e6cf18d2`.
Its Triton attention setting, disabled AITER/MLA switches and selected
`qwen3` / `qwen3_coder` parsers remain part of the reference contract.
The helper permits only independently qualified exact configurations.
Upstream engine source compatibility does not qualify the R9700 kernel path.

The native client schemas are bound to
[Qwen Code 0.23.0](https://github.com/QwenLM/qwen-code/tree/98a9c964158697dd5631d15a62174684ff7bbb53),
[DSH](https://github.com/deepseek-ai/deepseek-harness/tree/c389f96bf3a9b6807cb71ed6bdad5849be0df6d8), and
[Hermes](https://github.com/NousResearch/hermes-agent/tree/13fb5e1eceba51fc45a48b5d95a357e144d42689).
GitHub's commit API confirmed all three exact commits on the verification date.
DSH supports only the pinned ACP profile. Hermes's output-token cap remains
provider-owned. Qwen's existing system settings policy is never overridden.
Bundles are secret-free configuration; credential values come from the local
client's supported secret input. No harness automatically inherits RAG access.

## Storage and source ownership

The installed `versions.lock` and source manifests remain authoritative for
selected artifacts. `internal/catalog/models.json` is a reviewed derivative;
Bridge refuses a selected lock mismatch. Updating a model requires a reviewed
source change, refreshed immutable metadata and focused tests, not an API URL.

The explicitly migrated serving, workload resource and managed-cache settings
live in `/var/lib/bridge/managed-source.json`. A live helper checks that source
revision against the desired typed configuration and its own root qualification.
Unmigrated installer settings, storage layout, package locks, NAS backup and
retention remain under their existing ownership. Generated manifests and CPU
maintenance plans are exports, not additional editable authorities.

`model_root` must be the approved host path mapped into the existing `llm-models`
PVC. Bridge does not copy into a guessed K3s local-path directory. Set this path
from the actual PV/mount evidence before staging. The service reads models from
`/models/<model>/<revision>` with `HF_HUB_OFFLINE=1` and a read-only mount.
The API never downloads into a container layer.

Before staging, the owner configures that exact existing mount as
`bridge:<reviewed workload reader group>`, mode `2750`. Resolve the group from
the qualified workload's actual `fsGroup`; do not guess UID/GID 1000 or add the
worker to the API's credential group. The API inherits this GID for model
directories and files. Partial snapshots remain owner-only (`0700`) while
downloading. Publication permits group traversal (`0750`), and files retain
group read (`0640`). A GID inheritance mismatch refuses publication. The
packaged tmpfiles definition intentionally does not invent the model reader
group. Verify the mounted root with `stat -c '%a %U %G %g' /srv/ai/bridge-models`
and test reading a small generated staged fixture from the actual workload
identity before any large download. API credentials remain under their separate
owner-only state directory.

Staging retains `.partial-*` directories after failure. They consume the same
budget as published snapshots. Every file is streamed, bounded by its reviewed
size and verified before an atomic directory rename. The model root and every
managed relative path reject symlinks or special files. A receipt records
verification time, not continuing runtime qualification. An explicit verify
rehashes all files and rejects extras. Cleanup output is a preview; an owner
reviews incomplete transfers and the existing storage/backup policy before
removing anything. No model-delete API exists.

Reviewed download hosts are `huggingface.co`, `cdn-lfs.huggingface.co`,
`cdn-lfs.hf.co`, `cdn-lfs-us-1.hf.co` and `cas-bridge.xethub.hf.co`.
Redirects require HTTPS and this exact host set. DNS is checked and the dialer
uses the validated public address directly. Private, loopback, link-local,
metadata and reserved destinations are refused. The client uses no ambient
HTTP proxy, cookie jar or credential header. Signed upstream redirect query
strings are transient and are not recorded. A new CDN hostname requires a
reviewed adapter change; failure never becomes a fixture fallback.

## Dedicated worker preparation

The worker is separate from the API and root helper. Its socket authorizes the
configured API UID, while its process runs as the distinct `bridge-worker` UID.
The policy example deliberately contains invalid UIDs and missing digest
evidence. It cannot be started unchanged. The installer is not invoked by
worker tests or by packaging.

On an owner-controlled target, inspect the existing tools before preparing
policy. The reviewed optional isolation tools are
[bubblewrap v0.12.0](https://github.com/containers/bubblewrap/releases/tag/v0.12.0)
and [Buildah v1.45.0](https://github.com/containers/buildah/releases/tag/v1.45.0).
These release identities were resolved on 2026-09-08. Record executable hashes
from the installed target and check compatibility before any source job:

```sh
/usr/bin/bwrap --version
/usr/bin/buildah --version
/usr/bin/cmake --version
/usr/bin/ninja --version
/usr/bin/ccache --version
sha256sum /usr/bin/bwrap /usr/bin/cmake /usr/bin/ninja /usr/bin/ccache /usr/bin/cc /usr/bin/c++ /usr/bin/glslc
sha256sum /opt/rocm/bin/amdclang++ /usr/bin/buildah
findmnt --target /srv/ai/bridge-build-scratch
findmnt --target /var/cache/bridge-worker
systemctl show bridge-worker.service -p Delegate -p ControlGroup -p MemoryMax
```

Omit an optional tool from the last command when its corresponding recipe will
remain unavailable. Do not install missing tools or weaken namespace policy as
part of a Bridge test. The worker checks fixed executable hashes before each
job. Compiler binaries may resolve through root-owned installed symlinks; source
trees may not contain symlinks. No environment map or executable path comes from
the API.

Prepare the exact
[llama.cpp commit](https://github.com/ggml-org/llama.cpp/tree/427291b5b34cd914a31b3fd3b61a68f6184f4b9f)
as an immutable source export. The tree at that commit has no symlinks according
to the complete GitHub tree response. These commands generate local review
artifacts and do not install or compile the source:

```sh
git -C /absolute/reviewed/llama.cpp rev-parse HEAD
git -C /absolute/reviewed/llama.cpp status --porcelain=v1 --untracked-files=all
build_inputs=$(mktemp -d)
git -C /absolute/reviewed/llama.cpp archive 427291b5b34cd914a31b3fd3b61a68f6184f4b9f | tar -xf - -C "$build_inputs"
go run ./cmd/bridge-hostd --manifest "$build_inputs"
```

Save and review the generated file-hash map with the source export. Installation
of that export at `/usr/lib/bridge/llama-source`, the root-owned manifest, actual
UID allocation and policy digest values is a separate owner action. The helper's
`hardware.refresh` can publish sanitized same-boot evidence at
`/var/lib/spry-bridge-evidence/hardware.json` and `boot-id.txt`. This evidence
directory contains no helper credentials or privileged recovery state.

The scratch and compiler-cache roots must be dedicated, owner-provisioned
filesystems whose total capacity is within the requested limits. The worker
refuses a broad shared filesystem as proof of a smaller budget. Existing shared
cache/storage ownership is not changed automatically. Lowering a requested hard
filesystem budget can therefore require an owner storage change before the next
job; it does not resize or delete a live filesystem.

The worker accepts only `llama-hip`, `llama-vulkan` and the configured
`sunshine-image` recipe. Each request binds actor, operation ID, source revision
and requested budgets. Root policy sets independent ceilings. Heavy compilation
uses the existing 16 GiB host reserve, 8 GiB link reserve and 4 GiB/job rule;
the requested job count is a ceiling. The worker also applies cgroup memory,
zero-swap, CPU and process-count limits at process creation. It checks the
legacy build-inhibit marker before each phase. This inhibits new compilation;
already-running or unrelated builds are not claimed to be suspended.

Bubblewrap gives builds read-only source/toolchain mounts, private `/proc`, a
minimal `/dev`, isolated network and controlled environment. API state,
management credentials, helper sockets and unrelated host paths are absent.
The worker's private process view is deliberate; the root GPU helper retains
the complete host view. Cancellation kills the job cgroup and verifies that no
descendant remains before reporting cancellation. Uncertain outcomes remain
recovery-required. On worker restart, pending records are not retried. If the
recorded cgroup is absent or empty, the uncommitted build becomes failed; its
artifacts remain unqualified. Otherwise, new builds remain blocked until a
status check proves that no descendants remain. A persistence failure keeps a
separate fence until the worker restarts with readable durable records.

Successful jobs retain outputs, a bounded local `build.log`, CMake output where
applicable, and `provenance.json` with source/toolchain hashes, effective budgets,
fixed arguments and output hashes. Completion never installs packages, publishes
images, promotes a kernel or qualifies a workload.

## Offline Sunshine context

The original image recipe is supported source functionality. Its networked apt
resolution is an integration prerequisite, not proof that an offline image can
be built from a Dockerfile alone. The separate optional worker context requires:

- `base.oci`: an OCI archive preserving the exact Steam base manifest
  `sha256:f6bd0f5d88c6a5160fe765f61af9b33702b01cde45717b89f9ff390f104882dd`.
- `sunshine.deb`: release `2026.906.222525`, SHA-256
  `c87f226920ad83055a898be1f0c7540307593e92d8b0baf1f076909758db8ca0`.
- `apt/lists` and `apt/archives`: the complete owner-reviewed, signed Debian
  metadata and package closure for the original recipe, including both Mesa
  architectures at `26.1.2-1~bpo13+1`.
- The reviewed reference `bin`, `lib`, `templates`, `versions.lock` and
  `infrastructure/gaming` files, covered by a separate root-owned hash manifest.

On a separately authorized input-staging machine, an existing Skopeo can export
the base with `skopeo copy --preserve-digests
docker://docker.io/josh5/steam-headless@sha256:f6bd0f5d88c6a5160fe765f61af9b33702b01cde45717b89f9ff390f104882dd
oci-archive:/absolute/new-context/base.oci`. Resolve/download the complete apt
closure using the same base and the original signed repository policy. Do not
disable apt authentication or substitute a newer package to complete staging.
Hash the entire prepared context with `bridge-hostd --manifest`, review it,
and install it read-only before setting the three `sunshine_*` worker fields.

The embedded Containerfile uses `apt-get --no-download` and preserves all fixed
package versions. Buildah imports only that OCI archive, builds with
`--pull=never --network=none --isolation=chroot --storage-driver=vfs`, and writes
`sunshine.oci` locally. It receives the installed container signature policy
read-only. If that policy rejects the archive, the build fails; Bridge does not
weaken the policy. No Docker socket, root-equivalent group, FUSE device, registry
credential or image publication is involved.

## Qualification and recovery observations

Run the fixture checks without private inputs:

```sh
go test ./internal/catalog ./internal/adapters ./internal/worker
go test -race ./internal/catalog ./internal/adapters ./internal/worker
```

On compatible Linux, `go test -run TestLinuxContainment -v ./internal/worker`
tests the production namespace argument set against generated private-file,
network, writable-source and device-access probes. It reports a skip if
bubblewrap or unprivileged namespaces are unavailable. It does not weaken host
policy or claim cgroup runtime validation from source tests. Verify the packaged
units with the installed target's `systemd-analyze verify`, including
`DelegateSubgroup` support, and inspect actual cgroup v2 delegation before a
bounded named test job. Stop and preserve the operation, cgroup and scratch
artifacts if any descendant or outcome is uncertain.

NOT RUN — target hardware unavailable: native HIP/Vulkan builds, real model
downloads, apt dependency closure, rootless Sunshine image construction, cgroup
resource/cancellation behavior, systemd service execution, K3s RBAC/admission,
GPU handover, encoding and performance. Follow `docs/HOST-EXECUTOR.md` and the
reference `docs/HOME-LAB.md`, `docs/AI-PERFORMANCE.md`, `docs/MODELS.md` and
`docs/SUNSHINE.md` on the owner-controlled target. Retain zero replicas until
their exact target/image/model checks pass. Source tests make no claim about
VRAM pooling, memory bandwidth, P2P, ECC, model quality or workstation safety.
