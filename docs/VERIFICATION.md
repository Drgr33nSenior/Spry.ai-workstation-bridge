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
audits and `git diff --check`. The Linux/systemd and target-hardware exclusions
below remain unchanged. Installer checks were not repeated because relocation
did not change the installer. No Git staging, commit or push was performed.

These results establish an integrated fixture application and checked source
implementation paths. They do not qualify the physical workstation or an
installed Linux deployment. No installer, service installation, live cluster
change, large model download, workstation build recipe, reboot, publication or
Git staging/commit/push was performed. Tests used isolated temporary state,
generated credentials and ephemeral loopback listeners, not user credentials.

## Final Bridge commands

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
