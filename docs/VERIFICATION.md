# Verification record

Verification date: **2026-09-08**. Development platform: macOS arm64, Go 1.27.1.
Initial implementation and verification path:
`/Users/uk-gr9yjx0l0y/Projects/Spry.ai-workstation-bridge`.
At the owner's request, the implementation was then moved into the attached
checkout at `/Users/uk-gr9yjx0l0y/GolandProjects/Spry.ai-workstation-bridge`.
All 99 application source and authored documentation files matched their
pre-move SHA-256 values before the checkout references in this guide and README
were updated. Both checkouts retained their Git and IDE metadata. The original
user prompt and existing guardrail file remain in the initial checkout.

Post-move verification from the attached checkout passed `make check`,
`make browser`, `make package`, archive checksum verification, documentation
audits and `git diff --check`. At that point, the Linux/systemd and target-hardware
exclusions below were unchanged. Installer checks were not repeated because relocation
did not change the installer. No Git staging, commit or push was performed.

These results establish an integrated fixture application and checked source
implementation paths. They do not qualify the physical workstation or an
installed Linux deployment. No installer, service installation, live cluster
change, large model download, workstation build recipe, reboot, publication or
Git staging/commit/push was performed. Tests used isolated temporary state,
generated credentials and ephemeral loopback listeners, not user credentials.

## Five-finding remediation from 264e09e

Verified in the attached GoLand checkout on 2026-09-08, Go 1.27.1/macOS arm64.
HEAD matched review baseline `264e09e`. The existing untracked
`docs/GO-REMEDIATION-AGENT-PROMPT.md` was preserved. An unrelated front-matter edit
to `.aiassistant/rules/workstation-guardrails.md` appeared during the task and was
left untouched. No files in the reference
installer were changed; its focused/required checks were therefore not repeated.

Before fixes, the supplied `0077` staging test failed with reader mode 0600.
Permanent engine probes failed for A/B restore-chain admission and for dispatched
model.stage/model.verify/build.start selecting profile.restore. The helper A/B/C
probe reproduced the independent one-ID fence. The legacy cookie-jar probe
reproduced cross-port disclosure. A further test reproduced an old session token
being accepted when rewrapped in the new cookie name; purpose binding fixed it.
The original cgroup defect remains source-backed, not reproduced on a delegated
Linux kernel subtree; new controller fixtures exercise fresh and failed setup.

| Finding | Implementation and permanent regression | Observed result | Remaining qualification |
|---|---|---|---|
| 1. Publication umask | `internal/adapters/staging*`: explicit reader modes, private partial parent, exact-tree/receipt/hash verification and bounded repeat-stage repair; subprocess 0022/0077, nested/existing parents, failure and repeat tests | PASS, including actual synthetic cross-UID Linux access under 0077 | Installed workload GID/ACL/PVC mapping and filesystem durability |
| 2. Restore chains | `internal/domain/recovery.go`, `engine`, `hostexec`, `store`: linked independent journals, A/B/C, restarts, duplicate/competing requests, unrelated/cyclic/target fences, persistence faults and history retention | PASS in Go fixtures and supported race tests | Actual legacy-session/GPU transition and installed helper visibility |
| 3. Executor-specific recovery | `internal/adapters/adapter.go`, `engine/recovery*`, worker status: execution hash, active/missing/verified/corrupt publication, source drift, worker journal, manual recovery refusals and no redispatch | PASS with real local adapters over tiny fixtures; no host restore for local/build IDs | Real worker cgroup lifetime and target executor/process evidence |
| 4. Delegated controllers | `internal/worker/cgroup*`, `linux.go`: parent/supervisor/controller validation, enablement/limit readback and no-launch failures | PASS fixture tests on macOS and isolated Linux; kernel opt-in test safely skipped | NOT RUN — authorized writable delegated subtree unavailable |
| 5. Browser identity | `internal/config`, `api`, `auth`, session purpose and local/VPN examples; build-tagged browser fixture | PASS API/config tests and real Chrome live-cookie/CSRF/logout/expiry/isolation flows | Installed private CA/DNS trust, other browsers and real live deployment |

Commands actually run:

```sh
env GOTOOLCHAIN=local CGO_ENABLED=0 go test ./internal/adapters ./internal/engine ./internal/hostexec ./internal/worker ./internal/api ./internal/auth ./internal/config ./internal/store ./internal/domain -count=1
(umask 0077; env GOTOOLCHAIN=local CGO_ENABLED=0 go test ./internal/adapters -run '^TestStageVerifyAtomicAndCorrupt$' -count=1)
make check
make browser
make package
```

All commands above passed. `make check` included formatting, vet, unit/integration
tests, supported race checks, canonical generation/OpenAPI/manifests, local
builds, module verification and `govulncheck` (`No vulnerabilities found`).
The controller/CLI executable integration tests passed. Workflow files, Go
dependencies, systemd definitions and PKGBUILD template were unchanged, so
workflow lint and native makepkg were not repeated. Source manifest validation
checked the new CLI-only/local and browser-enabled/VPN examples.
Archive checksum verification and file-list inspection passed. The existing
documentation packager also includes the preserved remediation prompt Markdown;
these are local artifacts, not published releases. Review that input before any
separate owner-authorized distribution. The test-only browser binary is not in
the release archives.

`make browser` now builds `cmd/bridge-browser-fixture` only with its test build tag.
It uses real live API/cookie policy over explicitly isolated Demo adapters;
production `bridged` cannot select this combination. It writes synthetic owner
credentials into temporary protected files. It does not exercise CLI bootstrap;
the executable integration suite covers that separately. Chrome 152.0.7977.82
and Node 26.8.1 passed login, exact draft/plan/apply, session transition, crash and
recovery/reconnect, mobile layout, CSRF rejection, logout and server-side expiry.
Only the fixture handles a local signal to expire synthetic sessions. TLS uses
a per-run hostname-bound certificate; the test client validates its CA/SAN and
Chrome permits only that ephemeral SPKI, not a blanket certificate bypass. A
second HTTPS loopback service at an unrelated hostname received no management
cookie. Cookie-jar tests also cover plaintext same-host/different-port exclusion.
This supersedes the earlier HTTPS-browser exclusion below, not target TLS
qualification or the rule that HTTPS cookies share a hostname across ports.

Linux evidence used the public Go 1.27.1 bookworm image, repository digest
`sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b`.
Worker fixture tests ran without network, with read-only source, dropped
capabilities and bounded resources. Cross-UID staging ran with no host mounts,
network, added capabilities or published ports, under `umask 0077`, with a tiny
synthetic model and numeric reader UID/GID. The reader could open nested published
files and received EACCES on private partial content. An initial image-ID lookup
syntax was refused before container startup; the corrected immutable repository
digest run passed. No production build, full model download, credential read,
installer/service operation, cluster change or Git staging/commit/push occurred.

The delegated-cgroup kernel test, bubblewrap enforcement in that image, installed
systemd behavior, K3s admission/RBAC and GPU/hardware checks are **NOT RUN**.
The image lacked bubblewrap and no disposable writable cgroup subtree was
authorized. Cross-builds are not these runtime tests. Follow
[REMEDIATION.md](REMEDIATION.md) for exact migration, repeat-stage repair,
recovery, baseline reproduction and opt-in Linux qualification commands.

## Unified matrix follow-up

Verified 2026-09-08 from the owner's clean `221e124` baseline. Both entry
workflows now call `.github/workflows/ci.yml`. It runs one parallel Ubuntu,
macOS and Arch test matrix, then two independent tag-only packaging jobs.
Each packaging job requires the entire matrix to succeed. The previous
Arch-specific reusable workflow was replaced; image/tool pins and packaging
scripts were not changed.

| Command/check | Observed result |
|---|---|
| `make check` on macOS arm64 | PASS: formatting, vet, unit/integration tests, race, generated API, OpenAPI, manifests, local builds and vulnerability checks; existing platform exclusions retained |
| `go test ./internal/integration -run 'Test(SourceCheck\|TagBuild\|UnifiedCheck)' -count=1` | PASS: matrix rows, native/container selection, shared Arch pin, complete-matrix dependencies, independent packaging and new-tag-only artifact gates |
| Actual matrix check script with fixture `make`, `runuser` and `chown` commands | PASS for all three platform values, both success and failure exit codes, and a workspace path containing spaces; no privileged command was executed |
| `actionlint .github/workflows/check.yml .github/workflows/build.yml .github/workflows/ci.yml` | PASS with actionlint 1.7.12, including matrix expressions and the YAML container alias |
| `make arch-source VERSION=v1.0.0` and checksum verification from `dist/arch` | PASS; source archive contains the two entry workflows and `ci.yml`, not the removed `arch.yml` |
| `git diff --check` and documentation audits | PASS; new `ci.yml` inspected directly as well as tracked diffs |

The script fixtures validate command selection and failure propagation, not
GitHub scheduling, runner provisioning or Docker startup. No remote workflow,
native Arch package build, systemd/hardware test, installation or publication
was run for this refactor. The previous Arch sandbox/disk-space limitations
below remain unresolved and were not retried. Application/browser code, the
PKGBUILD, dependencies and installer were unchanged. No Git state was mutated.

Required-check display paths change with the reusable matrix. The owner must
review any named branch-protection checks after its first GitHub run; repository
settings were not read or modified. README and the packaging guide now separate
parallel checks, dependent packaging and target qualification explicitly.

## Earlier Arch CI and package follow-up

Verified 2026-09-08 in the attached GoLand checkout. This follow-up began from
the owner's clean `624d3d9` commit. It changes CI, source packaging, focused tests
and documentation only. The API contract, application runtime, installer and
existing deployment units were not changed. No Git staging, commit, tag, push,
remote workflow dispatch or package/service installation was performed.

At this stage, main pushes and PRs included a reusable Arch userspace check. Stable-tag
artifacts require Ubuntu, macOS and Arch success. The tag-only Arch step builds
a source-based pacman package and uploads its source, concrete PKGBUILD and
checksums with seven-day retention. See [ARCH-PACKAGING.md](ARCH-PACKAGING.md).

| Command/check | Observed result |
|---|---|
| `make check` on macOS arm64 | PASS after generator/workflow changes: formatting, vet, unit/integration, race, generation, OpenAPI, manifests, four builds and vulnerabilities; systemd remains excluded |
| `make package` and `shasum -a 256 -c SHA256SUMS` from `dist` | PASS; all three cross-platform archives |
| `make arch-source VERSION=v1.0.0`, `bash -n dist/arch/PKGBUILD`, checksum verification from `dist/arch` | PASS; deterministic source and concrete recipe with real source hash |
| `go test ./cmd/bridge-arch-package` | PASS; source inventory, modes, checksum/determinism, symlink refusal, invalid versions and malformed templates |
| Workflow integration tests, including race on macOS | PASS; exact tag gating, all check dependencies, read-only permissions, pinned actions/image, unprivileged Arch execution, no main artifacts or service installation |
| `actionlint .github/workflows/check.yml .github/workflows/build.yml .github/workflows/arch.yml` | PASS with actionlint 1.7.12 |
| `shellcheck scripts/ci-arch-setup.sh scripts/ci-arch-package.sh` and `bash -n` | PASS |
| `go test -v ./...` as an unprivileged user inside the pinned Arch amd64 image | PASS under Docker Desktop amd64 emulation; Linux Unix-peer and worker restart fixtures ran; bubblewrap containment skipped because its package was unavailable |
| `go vet ./...` inside the same Arch container | PASS |
| `makepkg --verifysource` inside Arch | PASS; generated archive accepted by makepkg's SHA-256 verification |
| Final `cmd/bridge-arch-package` test executable cross-built on macOS and run inside Arch | PASS, including actual `package()` execution with tiny fixture binaries in temporary directories; exact payload, permissions and CLI symlink checked. This is not a complete makepkg build |
| `bash scripts/ci-arch-setup.sh` inside the disposable Arch container | BLOCKED: pacman failed with `error restricting syscalls via seccomp: 22` under local amd64 emulation; sandbox/signature controls were not disabled |
| `makepkg --cleanbuild --noconfirm` inside Arch | BLOCKED, exit 8: missing `jq`; dependency preparation above failed. No `--nodeps`, `--syncdeps` or install bypass was used |
| `CGO_ENABLED=1 go test -race ./...` inside Arch | FAILED: local Docker filesystem ran out of space during compilation/tests; not a race-test pass. No unrelated Docker data was pruned |
| `git diff --check` and technical-writing audits | PASS; new untracked source files also inspected directly |

The container had no host source/home/device/socket mounts, published ports or
privileged mode. Only allowlisted source files and a Bridge test executable were
copied into it. Go 1.27.1
was downloaded from the official HTTPS distribution and checked against its
published SHA-256 before execution. An initial request without redirect following
returned a redirect page and failed the checksum check; the corrected HTTPS-only
redirect request passed. A checksum command initially used the repository root
instead of `dist/arch`; it reported a missing checksum file and passed from the
documented directory.
The disposable container and its scratch state were removed after verification;
the downloaded image cache and unrelated Docker data were left untouched.

The complete Arch CI job, native amd64 `makepkg` build, actual pacman install/
upgrade/remove behavior and GitHub artifact upload are **NOT RUN**. The workflow
uses native amd64 on GitHub, but that is configured behavior, not an observed
remote result. The local checks above do not remove those qualification gaps.
Browser tests were not repeated because this follow-up changes no UI/runtime
code. The initial browser evidence and physical target exclusions remain below.

## Initial application verification commands

| Command | Observed result |
|---|---|
| `make check` | PASS, exit 0 after final Go and RBAC changes; platform exclusions below |
| `make fmt` through `check` | PASS; no unformatted Go files |
| `go vet ./...` | PASS |
| `go test ./...` | PASS, including the actual daemon/CLI executable integration |
| `CGO_ENABLED=1 go test -race ./...` | PASS for packages supported on macOS |
| `go run ./cmd/bridge-apigen --check` | PASS; generated contract agrees with Go types and route register |
| `go mod tidy -diff` | PASS; no module/checksum drift |
| `go tool validate api/openapi.json` | PASS; pinned OpenAPI validator |
| `go run ./cmd/bridge-validate` | PASS; strict example JSON/YAML, named RBAC and distinct service sandbox source checks |
| `make build` through `check` | PASS; four local binaries |
| `go mod verify` | PASS; all modules verified |
| `go tool govulncheck ./...` | PASS; `No vulnerabilities found.` at verification time |
| `node --check scripts/browser-test.mjs` | PASS |
| `make browser` | PASS; actual Chrome 152.0.7977.82, Node v26.8.1 |
| `make package` | PASS; Linux amd64 server/helper/worker/CLI and macOS arm64/amd64 CLI archives |
| `file dist/linux-amd64/{bridged,bridge-hostd,bridge-worker,bridgectl} dist/darwin-{arm64,amd64}/bridgectl` | Expected statically linked Linux ELF and macOS Mach-O architectures |
| `shasum -a 256 -c SHA256SUMS` from `dist` | PASS for all three local archives |
| `tar -tzf dist/spry-bridge-linux-amd64.tar.gz` | Reviewed binaries, contract, deployment sources and documentation; no credentials, weights or installed runtime tree |
| Repeated `go run ./cmd/bridge-package` with unchanged inputs | Identical archive SHA-256 values; documentation was included in the final refresh |
| `git diff --check` in both repositories | PASS; limited to tracked/staged diff evidence, not a substitute for untracked-file review |
| `ai-guardrails docs audit --path FILE` for authored guides | PASS; no findings |

Focused tests, race tests, vet and Linux amd64 worker/helper cross-builds were
also run during implementation. Linux helper and worker test binaries were
cross-compiled, not executed. The source-only CI workflow was authored but was
not dispatched remotely. Release binaries use `CGO_ENABLED=0`; race testing uses
the available local C compiler. Tool versions and module checksums are pinned.

## Browser evidence and corrected failures

The browser test launches the real daemon and CLI, bootstraps a generated local
owner credential, and drives installed Chrome through its debugging protocol.
It passed credential exchange, HttpOnly session establishment, the isolated demo
label, unsupported-limit validation, exact serving change preview and target
confirmation, apply, separate source/live outcome display, gaming transition,
page refresh/session reconnect, daemon crash, durable recovery, explicit restore,
390-pixel layout, and logout revocation. All management pages were rendered.

One intermediate final browser run timed out at the explicit-restore assertion.
The fixture stopped the daemon when an operation became `running`, which can
precede external dispatch. The application correctly selected source-only
reconciliation for that boundary. The fixture now requires the durable
`dispatched` marker before the crash; the explicit-restore assertion was retained
and the complete browser run passed. Source-only recovery has focused engine
tests; this is not a claim that its browser flow was separately exercised.

Earlier focused runs exposed temporary-path canonicalization and formatting
issues, which were corrected and rechecked. An intermediate manifest invocation
with an unsupported `-root` flag returned usage status 2; the documented no-arg
invocation passed. No failure was suppressed or converted into qualification.

Resource/build/cache/harness pages have browser navigation/render coverage, not
complete browser form-submission/download coverage. Their contracts also have
API, CLI, adapter and bundle tests. Safari/Firefox, HTTPS browser behavior and a
full keyboard/screen-reader accessibility audit were not performed.

## Acceptance evidence boundaries

| Area | Local evidence | Remaining qualification |
|---|---|---|
| Bootstrap, login, expiry and revocation | Offline UID policy/store lock tests, API sessions/roles and real CLI/browser exchange | Installed cross-account filesystem/peer boundary and private HTTPS |
| Authorization and request defenses | Role, Host, Origin, CSRF, body/rate limits, malformed input and redaction tests | Independent deployed security assessment |
| Plans, source drift and recovery | Exact diff, idempotency conflict, source CAS, fault-injected persistence, pre-dispatch reconciliation and actual executable apply | Real external deployment effects |
| Hardware/resource policy | Two/four DIMMs, unknown topology, insufficient RAM, SMT constraints, stable identities and physical-card refusal fixtures | Actual discovery, trained memory settings and scheduler/device-plugin behavior |
| Session safety | Legacy/helper shared lock, authority, qualification and visibility refusal fixtures | Physical DRM holders, handover, readiness and encoding |
| Disconnect/cancel/restart | Durable intent, no redispatch, queue fencing, uncertain cancellation, helper journals and browser crash/restore | Linux descendant/cgroup behavior and physical effect boundaries |
| K3s dependency failures | Unavailable/error contracts, UID/qualification/boot and source gate fixtures | Real outage, expired credentials, admission and RBAC enforcement |
| Model staging | Bounded streaming, corruption/interruption, destination/redirect/credential, symlink/traversal and space-failure fixtures | Full selected downloads and actual serving filesystem permissions |
| Build containment | Named recipes, hashes, budgets, offline OCI integrity and cancellation contracts; Linux test compilation | Linux namespace denials, quota/cgroup enforcement and surviving children |
| Developer clients | Native bundle integrity, non-secret exports and unsupported-mode refusals | Owner-installed harness execution against qualified inference |
| Management UI | Actual Chrome flows described above | Other browsers and complete accessibility audit |
| Packaging | All requested architectures cross-built, archives inspected and checksummed | Installed Linux services and target manual compatibility |
| Independent helper authority | Tampered payload/source, root qualification/journal and continuous-lock tests | Installed root-owned artifact and OS peer enforcement |
| Demo/live isolation | Separate mode/state, startup-only selection and explicit failure/no-fallback tests | Installed host configuration review |

The source validator explicitly prints that Kubernetes admission, scheduling,
RBAC enforcement and target systemd behavior require qualification. It does not
invoke Kubernetes. `make check` printed:
`NOT RUN — compatible Linux systemd unavailable`.
The Linux-only Unix-peer and worker containment/restart tests are excluded by
platform build tags on macOS. No namespace restriction was weakened to run them.

## Installer integration checks

Reference path: `/Users/uk-gr9yjx0l0y/Projects/ArchLinuxThreadripperAI`.
The integration edits are limited to:

- `lib/workstation/session.sh`: root-owned canonical policy, continuous inherited
  lock, dedicated kubeconfig binding, durable state and template qualification;
- `tests/test_session.sh`: focused compatibility and failure fixtures;
- `docs/BRIDGE-CONTRACT.md`: adapter contract v1 and owner installation boundary.

`bash tests/test_session.sh` and the installer's complete `make check` both
passed after the last template-binding change. The required target checked shell
syntax, ShellCheck, YAML, 39 shell test files and Kustomize renders.

The installer reported these unavailable checks: `shfmt`, `bats`, a working
`ansible-playbook`, and Linux-only `systemd-analyze`. Optional fixtures also
skipped unavailable ccache/C-compiler execution, CMake/Ninja, supplied GPU chart
archive, configured Jinja2/PyYAML interpreter, Bash 4 USB-signal fixture and a
prebuilt gaming image. These skips are not passing runtime qualification.

Neither repository had a HEAD baseline. Existing staged/untracked IDE metadata,
the original implementation prompt and unrelated installer source were retained.
Final inventory includes new untracked application files; `git diff` alone does
not show them. Source and changed contracts were inspected directly. No complete
pre-edit installer snapshot was saved, so no reconstructed baseline is claimed.

## Target status

**NOT RUN — target hardware unavailable:** actual model/ROCm performance, GPU
handover, encoding, P2P, ECC stability, memory bandwidth, trained DIMM operation,
NVMe layout and workstation safety. Also unperformed: installed systemd helper
visibility, Linux worker containment/cgroups, K3s admission/RBAC and complete
large-artifact staging. Cross-builds and fixture success establish none of these.

Follow [QUALIFICATION.md](QUALIFICATION.md) for exact owner commands,
prerequisites, expected observations and recovery steps. Preserve existing gates
until those results are recorded. Missing qualification is not an API option.
