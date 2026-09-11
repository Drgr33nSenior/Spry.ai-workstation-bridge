# Implementation matrix

Performance evidence and profile operations extend existing installer runtime
commands; see [PERFORMANCE.md](PERFORMANCE.md) for commands and explicit limits.

| Requirement | Existing contract | Added implementation | Source regression / target boundary |
|---|---|---|---|
| Compare/select | Serving sweep, startup, memory and numerical evidence | Installer versioned comparison/profile artifacts; Bridge owner plans and private exports | Identity/case/quality/gain/refusal tests; no measured speedup |
| Coding/tool quality | Existing language tools; worker accepts fixed build recipes | Ten deterministic tasks with hashes/accounting | No network or generated-code shell; live code execution unavailable without an approved evaluator |
| Loading/queue | Pinned SGLang image and explicit evidence | Exact-image-gated bounded candidates; defaults unchanged | Unknown capabilities refused; native overload/cancellation/priority qualification remains target-only |
| Warm status | Readiness plus explicit non-root warmup | Fresh identity comparison; current Bridge health separate from historical reports | Stale/restart/failure tests; root helper cannot launch the non-root harness |
| Inference cache | Managed Triton/Inductor namespaces | Inventory/prune plans and protected namespace references | Path/ownership/symlink/active/reserve tests; no deletion or quota claim |
| Metrics telemetry | Full profile and collector-health monitoring | Optional metrics composition, component+margin accounting, sealed memory evidence | Both profiles/image parsers; no measured RAM saving or automatic budget transfer |


Installer paths below are repository-relative unless prefixed `Bridge:`.
The expected workstation specification is not discovered hardware.

| Requirement | Existing source/contract | Bridge implementation | Tests | Hardware qualification |
|---|---|---|---|---|
| Model inventory | `versions.lock`, `docs/MODELS.md`, `apps/overlays/{single-gpu,dual-gpu}`; exact model revisions | `internal/catalog/models.json`, revision import, file/size/licence/provenance API/UI | Catalog lock drift and immutable metadata; API/CLI catalog | Weights/model kernels unqualified until target tests |
| Stage/verify | Chat snapshots documented with HF staging; RAG `SHA256SUMS` and `rag.sh` | Narrow streaming HF adapter, checked DNS/redirects, budgets, file hashes, atomic persistent publication | Interrupted/corrupt downloads, redirects, private IPs, traversal, symlinks, space bounds | Full selected weights NOT downloaded in development |
| Serving options/lifecycle | Pinned SGLang manifest; zero replicas; pending model/ROCm qualification | Typed plans/source updates; helper fixed UID/resourceVersion environment/resource replacement, stop/start/restart/model switch | API exact diff/drift, helper authority/resource gates, executable/browser apply | Readiness and performance NOT RUN — target hardware unavailable |
| Workload resources | `hardware.sh`, `resources.sh`, `infrastructure/ansible`, GPU device plugin | Boot/age-bound inventory; SMT/Guaranteed QoS/capacity validation; GPU counts; CPU maintenance export | 2/4 DIMMs, unknown topology, RAM/SMT limits, duplicate models/enumeration, physical-card refusal | Trained speed/channel operation, 4-DIMM retuning and kubelet policy owner qualification |
| Memory evidence/candidates | Reviewed installer collection and `serving-memory-plan`; owner-run non-root cold/warm observations | [Memory budgets](MEMORY-BUDGETS.md): owner-only sealed import, deterministic export, private artifacts, complete-template/model-file/launch/hardware/boot/node rechecks | Observed Demo browser: unknown/incomplete/refused/candidate states, explicit export review, retained unqualified summaries | No live resize or qualification; nonzero CPU-offload export unavailable until a separately reviewed collector can attest it |
| Optional memory adviser | Actual OpenAI Agents API function flow; no Responses API substitution | Disabled-default bounded outbound session; three fixed sanitized-evidence tools; owner-bound plan, no approval/apply or policy authority | Observed browser-only response fixture: escaped explanation and returned plan remains unapproved; paid API NOT RUN | No cloud result grants qualification; runtime hashes remain separately root-approved; privacy and spending limits in memory guide |
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
| Release packaging | No existing application build | Go1.27.1, pinned runtime/validation/security dependencies, local archives, source-only CI | `make check`, `make package`, `make browser` | Cross-builds do not qualify deployment |

Model publication, recovery chaining, worker cgroup setup and browser-session
isolation have explicit safety boundaries. They do not replace target
qualification: service-sandbox/cross-UID model access, kernel cgroup enforcement,
systemd helper visibility, K3s admission/RBAC, target TLS, AI/gaming handover,
ROCm/model performance, encoding, P2P/ECC, measured memory bandwidth, NVMe
layout and workstation safety remain owner qualification work.

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
