# Implementation matrix

The reference is `/Users/uk-gr9yjx0l0y/Projects/ArchLinuxThreadripperAI`, inspected
2026-09-08. It has no HEAD; its existing untracked source and staged IDE metadata
were preserved. Paths below are relative to that repository unless prefixed
with `Bridge:`. The expected workstation specification is not discovered hardware.

| Requirement | Existing source/contract | Bridge implementation | Tests | Hardware qualification |
|---|---|---|---|---|
| Model inventory | `versions.lock`, `docs/MODELS.md`, `apps/overlays/{single-gpu,dual-gpu}`; exact model revisions | `internal/catalog/models.json`, revision import, file/size/licence/provenance API/UI | Catalog lock drift and immutable metadata; API/CLI catalog | Weights/model kernels unqualified until target tests |
| Stage/verify | Chat snapshots documented with HF staging; RAG `SHA256SUMS` and `rag.sh` | Narrow streaming HF adapter, checked DNS/redirects, budgets, file hashes, atomic persistent publication | Interrupted/corrupt downloads, redirects, private IPs, traversal, symlinks, space bounds | Full selected weights NOT downloaded in development |
| Serving options/lifecycle | Pinned SGLang manifest; zero replicas; pending model/ROCm qualification | Typed plans/source updates; helper fixed UID/resourceVersion environment/resource replacement, stop/start/restart/model switch | API exact diff/drift, helper authority/resource gates, executable/browser apply | Readiness and performance NOT RUN — target hardware unavailable |
| Workload resources | `hardware.sh`, `resources.sh`, `infrastructure/ansible`, GPU device plugin | Boot/age-bound inventory; SMT/Guaranteed QoS/capacity validation; GPU counts; CPU maintenance export | 2/4 DIMMs, unknown topology, RAM/SMT limits, duplicate models/enumeration, physical-card refusal | Trained speed/channel operation, 4-DIMM retuning and kubelet policy owner qualification |
| Profiles and recovery | `session.sh` continuous FD9 flock, saved snapshots, DRM and unmanaged Pod gates | `internal/hostexec`, typed profile plans, independent root journal, inherited canonical lock | Legacy/helper contention, persistence, failed starts/visibility/refusals; demo browser crash recovery | Actual AI/gaming handover, encoding and DRM release NOT RUN |
| Direct CLI/API serialization | Legacy caller-selected session directory was an integration gap | Narrow `session.sh` canonical root policy and inherited verified FD, durable state/build-gate handling | Added `tests/test_session.sh`; installer focused and required target | Install both compatible adapters together; do not mix old runtime |
| Named builds | `build.sh`, `rocm.sh` locked llama HIP/Vulkan; `sunshine.sh` existing image recipe | Separate Go worker, immutable staged inputs/toolchain, bwrap/cgroup containment, offline Sunshine input adapter | Named recipe refusal, budgets/provenance/cancellation contracts; Linux build compile | Actual sandbox denials, descendants, toolchain/GPU/encoding validation target-only |
| Existing unsupported build surface | TheRock source planning is not a complete build recipe; kernel clean-chroot is owner host administration | Explicit TheRock refusal; kernel install/promotion unavailable through Bridge | Recipe enumeration and refusal tests | Never imply a built/promoted/qualified framework |
| Persistent caches | Existing ccache/shared model/shader storage and NAS ownership | Cache usage/budgets and bounded cleanup preview; future-job budgets constrained by worker root ceilings | Cache scans, bounds, no broad deletion endpoints | Filesystem quotas and actual consumption require installed target checks |
| Client configuration | `agents.sh`, `AGENT-HARNESSES.md`; Qwen then DSH then Hermes; bundle integrity | Native non-secret bundles, UI downloads, client-local verified configure/launch | Bundle integrity, secrets, unsupported DSH CLI/Hermes limits, actual CLI exports | Client installation/model tooling compatibility owner qualification |
| Management application | No prior Bridge application | `cmd/bridged`, `cmd/bridgectl`, embedded local web pages | Actual executable integration; actual Chrome login/edit/apply/profile/recovery/reconnect/mobile/logout | macOS CLI runtime tested; Linux deployment runtime separate |
| Authentication/authorization | New Bridge boundary | Offline UID-authorized bootstrap/recovery, hashed random credentials, roles, cookie sessions/CSRF/Host/TLS | Roles, expiry/revoke/logout, origin/Host/body limits, exclusive lock, unsafe network configs | Cross-UID Linux OS boundary and installed TLS/private access qualification |
| Privileged fencing/provenance | Installed runtime/session gates | Root policy, peer UID, typed hashed IDs, installed artifact hashes, exact qualification, root recovery | API tamper/source drift, lock ownership, recovery-required restart; independent root policy tests | Real Linux process/DRM visibility cannot be inferred from CI |
| Durable operations | New Bridge boundary | Single-writer atomic store, intent before effects, source/live separation, idempotency, queue, cancellation, explicit restore/reconcile | Faults before/after source rename, storage poison, queue recovery fence, restart inspect/no redispatch | No exactly-once external-effect claim |
| Private deployment/RBAC | Existing K3s 1.35.7+k3s1 and namespace abstractions | Outside-K3s services, local sockets, explicit HTTPS/VPN policy, narrow RBAC | Source contract/manifests and cross-builds | Admission, RBAC and systemd behavior NOT RUN on actual workstation |
| Release packaging | No existing application build | Go1.27.1, standard-library runtime, pinned validation/security tools, local archives, source-only CI | `make check`, `make package`, `make browser` | Cross-builds do not qualify deployment |

Supported source capabilities have live implementation paths. Missing installed
runtime hashes, reviewed source inputs, offline image input closure, dedicated
identity, sandbox prerequisites or qualification remain explicit integration
prerequisites. A missing K3s dependency does not produce a fixture success.
Global request/output caps, deterministic physical-card placement and automatic
AI/gaming GPU coexistence are refused where the selected deployment contract
does not enforce them. Model quality/context are never reduced to pass a plan.

Target qualification, current two-DIMM bandwidth, ROCm kernels, P2P, ECC, encoding,
NVMe layout and workstation safety remain **NOT RUN — target hardware unavailable**.
Follow [QUALIFICATION.md](QUALIFICATION.md); CI cannot establish those properties.
