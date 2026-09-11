# Verification record

## Telemetry and adviser remediation — 2026-09-11

Both working trees were clean at entry: Bridge
`a6e80a3beee27c44e1272beb22a83d6b3278e8be` and installer
`82ab8af265c2d2722e4a82d26da9bf3474351e32`. The checks below concern the
subsequent uncommitted source changes, not a new immutable release. No installed
service, live workload, credential, owner policy, Git index or Git history was
changed. Earlier verification sections remain historical evidence.

The final sealed installer source archive used for the race-enabled memory pair
test has SHA-256
`7d598dab0999ad080d41b0c4746b8a9c5862e716c5319bfe4e0d8541f4f74b9d`.
It is retained at installer
`test-results/telemetry-review.CkHF7h/sealed-reviewed/bootstrap-source.tar.gz`,
with its lock and an extracted copy. This identity is source-test evidence,
not an installed package or runtime approval.

| Issue | Correction and permanent regression |
|---|---|
| Environment-free session lacked required initial input | `internal/advisor/client.go` creates one session with fixed input and handles active/tool/completed-turn observations. `TestInitialSessionOutcomesStartExactlyOneTurn` and `TestCreateUncertaintyNeverRetriesOrReflectsProviderData` cover the contract and uncertain creation. No message resubmission or automatic create retry. |
| Absent usage became zero | Nullable usage with explicit provisional/source metadata. `TestUsageUnknownAndProvisionalSources`, `TestLaterUnknownUsageDoesNotReusePreviousSnapshot` and `TestUsageObservationLimitsAndUnknownUsageRemainBounded` cover missing, null, explicit zero, malformed and over-limit accounting. |
| Refusal alert missed cluster metric names | Installer `config/alerts.yaml` covers both pinned memory-limiter families and all three signals. `tests/fixtures/telemetry/alerts_test.yaml` executes with pinned Prometheus. |
| Host collector lacked central health reporting | Installer `host/config.alloy` self-scrapes bounded health metrics into a distinct job and direct Prometheus path. Heartbeat and forwarding-failure fixtures detect loss, including loss of the health path itself. |
| Summary copied the operation journal | `Store.TelemetrySnapshot` returns only counts and storage health. `TestTelemetrySnapshotCountsAndPersistenceFailure` checks persistence refusal; `TestTelemetrySummaryDoesNotCloneRetainedEvents` exercises the authenticated API with 500 retained operations and 128 events each. |
| Pair compatibility was optional in release assembly | The installer ISO/release builder now requires its sealed source, verifies the selected Bridge test exists, runs that test, and binds installer/source/recipe identities in retained `Bridge.builder.lock`. Source/archive/missing-test/failed-test/swapped-build regressions cover refusals. Bridge-only tag artifacts remain unpaired. |

Additional coverage retains sanitized failed-advisory accounting in the existing
bounded owner-only audit, uses explicit cloud memory units, and labels unknown
or provisional usage in the real browser. Tests cover authorization, audit
persistence faults and no automatic mutation. The generated OpenAPI documents
nullable usage and `X-Bridge-Advisory-ID`. Clients must preserve `usage: null`;
it is not a zero-token result. No store schema migration or installed runtime
hash update is required. `bridgectl audit` reads retained attempts through the
same owner authorization as the existing audit API.

The installer also has an executable synthetic OTLP scope/link-attribute test.
The pinned transform removes scope attributes and refuses linked spans; it does
not claim to sanitize arbitrary per-link fields. These are isolated Linux image
checks, not evidence of a leak in an installed system or a physical overload test.

### Observed checks and limits

Bridge uses the exact Go 1.27.1 toolchain. Test-child environments remove inherited
`OTEL_*` variables without displaying their values; service policy still rejects
those overrides. To repeat a command without changing the parent environment:

```sh
/bin/bash --noprofile --norc -c '
  while IFS= read -r name; do
    case "$name" in OTEL_*) unset "$name";; esac
  done < <(compgen -e)
  exec "$@"
' bridge-check make check
```

| Command | Observed result |
|---|---|
| `GOTOOLCHAIN=local CGO_ENABLED=0 go test ./... -count=1` | PASS on macOS; Unix-socket tests executed, unlike the restricted review environment |
| Focused adviser/API/auth/config/store/telemetry/CLI tests, uncached | PASS; adviser race and vet checks also passed |
| `make check` | PASS: toolchain, fmt, vet, tests, race, generated, module tidy, OpenAPI, manifests, builds and security; govulncheck found no vulnerabilities |
| `make browser` | PASS: Chrome 152.0.7977.83 / Node 26.8.1, including unknown/provisional usage, memory plan review, existing TLS/session/CSRF/recovery flows |
| `make package` and `shasum -a 256 -c SHA256SUMS` in `dist` | PASS: Linux amd64 and macOS arm64/amd64 archives; service/configuration examples inspected |
| `bash scripts/test-installer-contract.sh INSTALLER` and `--candidate INSTALLER` | PASS; known-good `67a5060` retained separately from the selected installer |
| Actual candidate memory contract against a sealed, extracted installer source | PASS with tiny synthetic input, private output verification and the installer negative corpus; no RAM saving established |
| Installer telemetry, serving-memory and performance scripts with explicit prepared Python | PASS: 9 + 14 + 18 + 3 + 17 + 2 Python tests and related shell fixtures |
| Installer `make check` with the prepared child environment below | PASS with explicit skips: 55 test scripts, shell syntax/ShellCheck, YAML, Ansible syntax and local Kubernetes rendering; exclusions listed below |
| Installer Bridge-build/bundle/Docker-wrapper/release shell fixtures | PASS, including actual source sealing and swapped build evidence refusal; not actual package installation or ISO boot |
| Installer `bash tests/test_telemetry_stack.sh` | PASS: actual local Kustomize rendering and source policy assertions |
| Installer `bash infrastructure/observability/validate-images.sh desktop-linux` | PASS using cached pinned images; executable Prometheus rules, Alloy parsers, host log and cluster OTLP sanitization |
| Linux systemd gate, installed peer/cgroup behavior, K3s routes/RBAC and hardware tests | NOT RUN — this Mac is not a prepared workstation installation |
| Paid Agents API creation/completion/cancellation and real collector refusal/forwarding failures | NOT RUN — separate owner authorization and prepared target/account required |

The installer check skipped shfmt and bats because they are unavailable. It also
skipped the real ccache repeat-build test, generated CMake/Ninja pool fixture,
pinned GPU-operator chart rendering, streaming-image smoke test and Linux Wayland
process-group test because their tools or explicitly selected inputs were absent.
Real HIP IPC and Linux systemd checks did not run. Ansible syntax checks passed
with expected empty-inventory/host-pattern warnings; they contacted no hosts.
`make check-strict` was NOT RUN; the skipped gates prevent a strict qualification
claim. Workflow lint was not applicable: no workflow source changed.

The image checks used Linux ARM64 variants in a local Docker VM. Containers were
non-root, read-only and capability-dropped, with no real credentials, journals or
device mounts. Parser/log checks had no network; synthetic OTLP used an owned
internal network, without published ports or an external route. No image was
pulled. Logs remain in installer `test-results/telemetry-images.*`; the final
sanitization run is `telemetry-images.vDvrQ7`. Ten stopped experimental containers
were removed after exact identity/state/label checks; their recorded logs remain.
This does not qualify Arch AMD64 services or real collector pressure behavior.

### Initial failures and corrections

The initial adviser contract fixture failed with HTTP 400 when creation omitted
input. The telemetry summary allocation regression failed at 72 allocations for
empty history versus 5,564 for retained history; after the counter change both
were 46. These are local synthetic allocation results, not Threadripper latency
or memory-bandwidth claims. The explicit cloud-unit regression also failed before
the field-name correction. An initial new API fixture omitted Content-Type and
received the correct 400; adding its required JSON header made it exercise the
intended audit behavior.

During collector fixture development, read-only `/tmp` blocked promtool; a bounded
tmpfs resolved it. The absent-series expectation omitted Prometheus's derived
`job` label. The generated Alloy fixture needed multiline syntax and its test-only
debug sink's experimental stability flag. An attempted Unix HTTP listener was
unsupported; the final test uses the isolated internal network instead. No
production processor or sandbox was weakened to make these checks pass.
Final review also added explicit memory/CPU/process limits and `--pull never`
to both new OTLP test containers, bounded the wget request, and made the fixture
server remove itself on completion. The final validator rerun passed with those
restrictions and left no active test network or container.

Release-gate review caught temporary comparison files inside the source tree,
missing test files in the sealed source closure, and unbound Bridge source/recipe
identity in a gate lock. Those checks now use external comparison files, include
only the needed source fixtures, and reject swapped build evidence. Tests were
expanded rather than treating mocked orchestration as archive execution.

The first installer wrapper attempt lacked `HOME_LAB_PYTHON` and blocked/skipped
its fixtures. Explicit Python 3.11.6 ran the telemetry/memory/performance tests,
but the full source check then failed importing `markupsafe` for Jinja rendering.
An existing prepared Python 3.11.1 environment contains pinned PyYAML 6.0.3,
Jinja2 3.1.6 and MarkupSafe 3.0.3. Its targeted rendering check passed with
`PYTHONNOUSERSITE=1`. A subsequent ISO fixture still used bare `python3` and failed
to import `yaml`; the prepared environment must also precede the inherited PATH
in the test child. No dependency was installed or upgraded.
The next complete run exposed an incomplete release-coordinator fixture: its
mock source-preparation command created a lock but not the newly required source
archive. The fixture now creates a synthetic archive placeholder for its mocked
wrapper; production archive verification remains real and separately tested.
The exact prepared-environment release fixture passed after that correction.
The subsequent complete installer `make check` exited 0 with the exclusions above.
Final archive checksums passed. The first manual archive comparison used a
nonexistent `server.lan.example.json` path; discovery identified the actual
`server.vpn.example.json`. Comparing every packaged JSON/service/socket example
against its source then passed. This was an inspection-command error, not a
missing package input.

For a prepared environment, select both the explicit fixture interpreter and
the bare `python3` used by subprocesses. These overrides affect only the child:

```sh
INSTALLER=/absolute/reviewed/installer
PREPARED_PYTHON=/absolute/prepared/environment/bin/python
env PATH="$(dirname "$PREPARED_PYTHON"):$PATH" PYTHONNOUSERSITE=1 \
  HOME_LAB_PYTHON="$PREPARED_PYTHON" make -C "$INSTALLER" check
```

Follow [live adviser acceptance](MEMORY-BUDGETS.md#separate-live-adviser-acceptance)
and the selected installer's telemetry acceptance procedure only after separate
owner authorization. Keep deterministic planning and independent recovery access.
Do not infer a safe memory limit, GPU performance, cgroup enforcement or deployment
readiness from these source, browser, image-parser or cross-build results.

## Memory-budget integration — 2026-09-11

This section records source and isolated-fixture evidence, not workstation RAM
savings or installed-service qualification. The earlier results below remain
historical. No installer file, live workload, owner policy, credential, deployment
or Git history was changed for this integration. Existing telemetry and ISO work
was preserved. The memory integration adds no Go dependency; the existing dirty
OpenTelemetry module changes remain in place.

The inspected pair was Bridge `90312ed3c81a6dae5f6b84a072d67f670ac970c8` and
installer `843a52be656699f261285d052e3e42fbac54b9e1`, both with uncommitted changes.
These commits alone do not identify the candidate memory implementation. Exact
source SHA-256 values inspected in this checkout were:

| Source | SHA-256 |
|---|---|
| Bridge `.go-version` (1.27.1) | `a8844fbc8c3eb51c26ed00aec4cd5a5a9d647f849c6ead07bcc83ed24c4a7718` |
| Bridge `go.mod` | `e4dfa3af517e4cb5015c9acfecc99518c789dbd1792828ef55d05c18b1a6bfd0` |
| Bridge `go.sum` | `7a039797ebd8cad6db85d40ca6c7c38c0f9e902c7eb98e9caf1183679e2ea3da` |
| Bridge generated `api/openapi.json` | `cfc298c3fc6a2b0f6981640c23e94d1403030dfb0e932d8d16707e62003b9fe2` |
| Installer `infrastructure/packages/bootstrap/source.files` | `e315b81d0397c23b346ae7f4ec9d0f4b673ba3bb2b282e84021c53ae64f583dc` |
| Installer `infrastructure/packages/bootstrap/bridge-runtime.files` | `0a9f0e4745125bbd25b80421e7d2eb30506f4c01b8135fe96c7e1535b5bd4a9b` |
| Installer `infrastructure/packages/bootstrap/PKGBUILD` | `231d463baa562a51448bc7cc9e4fe757d243290c8af1eca5aff475e5c7654893` |
| Installer `bin/workstationctl` | `6d8ef5c975a94392361e6da9c83288a663bf6adf4525228598c645fa0ad8d504` |
| Installer `versions.lock` | `f2a39a52bca6133e57a28c1a2081f71e1ecf2736569b759814ef8676ecf97541` |
| Installer `lib/workstation/serving_memory.py` | `bfd564ee63ae113676fc194d4330feeb410e4f82c5ed248b7765e6dc716fae96` |
| Installer `lib/workstation/measurement.py` | `fad19694032f8672db16bf55944fd6d0f571c93b44bb3092dfa89a9cc537c301` |
| Installer `lib/workstation/performance.sh` | `8a7fc3e5d81729d4df9a9c5d5b9575a473a1e8ef8c961ff09e0f67f39ca9c3e4` |
| Installer `lib/workstation/serving.py` | `6708002e2a9f41bdff9ce94d932254b9b0c7b96cd10feeeaca53ee8b8eec4daa` |
| Installer `lib/workstation/model_kernels.py` | `50ea3b11722306ea1a81323bf3f88d5357c6d898f99fca8f1f66d8e68e29ecad` |

The installer package recipe checks its source archive digest, source manifest
and `BUILD-IDENTITY`. No new installer package or installed build identity was
generated or approved here. The candidate test checks the actual runtime-file
closure and reports tool hashes. Runtime authorization still requires the
separate root-owned installed manifest; the table is not a replacement for it.

### Memory regression coverage

- `TestCandidateInstallerMemoryContract` invokes the actual candidate planner
  through `workstationctl`, using its own tiny cold/warm fixtures. It validates
  the full Go output contract, patch/rollback hashes and conservative result;
  altered runtime identity, capacity, averages, Pods, phases and shm refuse.
  It also runs the candidate's `test_serving_memory.py` negative corpus for
  duplicate/restarted Pods, foreign cgroups, final telemetry, pressure, shared
  memory and CPU/model changes. No model weights or real benchmarks run.
- `TestMemoryRealIntakeRetainsFailedEvidenceWithoutCluster` executes the real
  helper intake/copy/journal path with fixture ownership/provenance hooks. It
  retains failed private evidence, rejects drift and makes no cluster probe.
  Other helper tests bind full templates, node identities, qualified model-file
  hashes, effective launch settings, source authority and read-only restart state.
- `TestPrivateOutputDurabilityAndExactTree` and
  `TestPrivateArtifactEscapedWireBound` cover private exact-tree publication,
  bounded wire encoding, sync/refusal and evidence retention. Filesystem tests
  do not simulate physical power loss or prove installed UID enforcement.
- Engine tests cover source drift, expiry, idempotency, no automatic apply,
  unavailable generic inventory and later independent helper completion without
  redispatch. `TestCLIAndDaemon` now exercises memory commands, owner-only reads,
  import/export, private summary download and inspection after daemon restart.
- Agents API tests use a mocked transport: fixed function allowlists, hostile
  arguments/output, secret sentinels, turn identity, failed tools, API outages,
  redirects, count/time/byte bounds and cancellation distinct from completion.
  The API rechecks owner authentication for each tool and strips arbitrary text
  before any cloud-bound serialization.
- Real Chromium tests cover Resources unknown/incomplete/refused/candidate
  states, live-style `ready-for-plan` UI semantics, explicit review without
  approval, retained summaries and escaped advisory text. The live-style
  preflight/provider responses are browser-only fixtures, not Linux or paid-API
  qualification. Existing TLS/cookie, recovery, mobile, expiry and logout tests
  remain enabled.

### Repeat source checks

Observed on macOS arm64 with the exact Go 1.27.1 toolchain:

| Check run | Outcome |
|---|---|
| Focused uncached domain/API/adapter/helper/memory/engine/advisor/config/CLI tests | PASS |
| `go test ./... -count=1` with actual candidate installer path | PASS; includes the candidate's Python memory regression corpus |
| Uncached `TestCLIAndDaemon` | PASS; actual executable memory workflow and restart inspection |
| Pinned and candidate `scripts/test-installer-contract.sh` invocations | PASS; known-good fixture retained separately |
| Candidate memory contract with `CGO_ENABLED=1 go test -race` and `-count=1` | PASS; actual planner fixtures, not GPU execution |
| `make check` | PASS: exact toolchain, fmt, vet, tests, race, generated/OpenAPI/manifests, build, module verification and govulncheck; no vulnerabilities found |
| `make browser` | PASS; Chrome 152.0.7977.83 / Node v26.8.1; memory states, preflight-only review, and existing security/recovery flows |
| `make package`, archive checksums and package inspection | PASS; Linux amd64 server/helper/worker/CLI and macOS arm64/amd64 CLI; private configuration examples and memory guide included |
| Documentation audits and final diff checks | PASS; advisory writing checks only, not a security certification |
| Linux/systemd gate inside `make check` | NOT RUN — compatible Linux systemd unavailable; target admission/RBAC also explicitly unqualified |

From Bridge, set the candidate path to the separately reviewed source checkout.
The command does not install or approve it:

```sh
BRIDGE_INSTALLER_MEMORY_CANDIDATE=/absolute/reviewed/installer \
  GOTOOLCHAIN=local CGO_ENABLED=0 go test ./... -count=1
env GOTOOLCHAIN=local CGO_ENABLED=0 go test ./internal/integration -run '^TestCLIAndDaemon$' -count=1 -v
bash scripts/test-installer-contract.sh /absolute/reviewed/installer
bash scripts/test-installer-contract.sh --candidate /absolute/reviewed/installer
make check
make browser
make package
(cd dist && shasum -a 256 -c SHA256SUMS)
```

The first catalog command preserves the known-good
`67a506090e8ecf696190e0be55f865e3ce054d0e` fixture. The candidate commands do not
replace it. Without `BRIDGE_INSTALLER_MEMORY_CANDIDATE`, the candidate memory
test explicitly skips; a normal Go pass alone does not imply pair validation.

Initial fixture attempts failed on a relative candidate path and macOS `/var`
symlink aliases. Tests now resolve their disposable paths and use an explicit
candidate path. A private-output fixture also needed explicit 0700 setup. An
initial new browser fixture had invalid top-level `await`; its async wrapper
was corrected. These were test setup failures, not target observations; no
production path, permission or browser security checks were weakened.

### Memory qualification still required

**NOT RUN — target hardware unavailable:** actual non-root cold/warm collection,
smaller-limit startup/steady memory, numerical/coding correctness, throughput,
latency, DIMM rediscovery and safe rollback. Follow
[MEMORY-BUDGETS.md](MEMORY-BUDGETS.md) for exact collection, import/export,
candidate-test and preconditioned rollback commands, together with the existing
[target qualification procedure](QUALIFICATION.md). Retain independent SSH and
recovery access. A synthetic 33280-MiB candidate is not an observed saving.

**NOT RUN — prepared Linux runtime unavailable:** installed helper service,
root/reader access, cgroup enforcement, Kubernetes RBAC/admission and systemd
behavior. Cross-built Linux binaries are not runtime evidence. The source gate
reports systemd unavailable on this macOS host; no service, namespace or host
security control was changed to obtain a pass.

**NOT RUN — real Agents API/account/spending controls:** no key or paid request
was used. The optional connection uses the actual documented Agents API, not
Responses or the Agents SDK. Its documented session contract has no per-session
hard spending ceiling; project spending controls require separate owner setup.
Nonzero CPU-offload evidence export is explicitly unavailable with the selected
collector. Backup/NAS restore, ISO/UEFI, gaming input/capture and simultaneous
AI/gaming acceptance remain separate deliverables.

## Initial implementation — 2026-09-08

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

## Round-two remediation from b21c86d

Baseline `b21c86deb125e64d249ba33a5db7f52656a18c8b` and repository location were
verified on 2026-09-08. The initially untracked
`docs/GO-REMEDIATION-ROUND-2-AGENT-PROMPT.md` was preserved. The installer,
workflows, systemd units, API/schema, dependency pins and journals were not changed.

The browser-policy coverage defect was reproduced before the fix: removing the
old inline browser-policy block in a temporary Go source overlay left
`TestLiveBrowserSessionsRequireDedicatedHTTPSIdentity` passing. After the fix,
removing the production call to `validateBrowserSessionPolicy` in an isolated
overlay makes all six negative cases of
`TestLiveBrowserSessionsRejectUnsafeManagementIdentity` fail with the wrong-error
assertion. The unrelated macOS/Linux/TLS errors no longer satisfy these cases.
The repository's production policy was never removed; the overlay was not used
for the normal verification commands. To repeat this sensitivity check against
the fixed checkout, with a new disposable temporary directory:

```sh
bridge_mutation=$(mktemp -d)
cp internal/config/config.go "$bridge_mutation/config.go"
perl -0pi -e 's@\n\tif e := validateBrowserSessionPolicy\(c, u\); e != nil \{\n\t\treturn e\n\t\}@\n@' "$bridge_mutation/config.go"
bridge_config_source="$PWD/internal/config/config.go"
printf '{"Replace":{"%s":"%s"}}\n' "$bridge_config_source" "$bridge_mutation/config.go" > "$bridge_mutation/overlay.json"
env GOTOOLCHAIN=local CGO_ENABLED=0 go test -overlay "$bridge_mutation/overlay.json" ./internal/config -run '^TestLiveBrowserSessionsRejectUnsafeManagementIdentity$' -count=1 -v
```

Expected: exit 1 and six failed browser-policy assertions. This is an intentional
mutation check, not a passing application test. Run normal checks without
`-overlay`; no repository restoration or weaker validation is needed.

The requested bounded workflow run passed **3/3** uncached race-enabled
repetitions, with individual durations 2.85s, 2.76s and 2.88s (package 9.885s):

```sh
env GOTOOLCHAIN=local CGO_ENABLED=1 go test -race ./internal/integration -run '^TestUnifiedCheckMatrixGatesIndependentTagPackages$' -count=3 -v
```

The earlier review's two-second shell-fixture deadline warning did not recur.
Its cause remains unknown. No timeout, assertion, workflow condition or retry
behavior was changed, and no failure was reclassified as a race-test pass.

An initial portable staging implementation required child setgid inheritance;
focused macOS tests failed because that platform inherits GIDs without necessarily
copying the setgid bit. The implementation now checks the required GID on each
inode and keeps private ancestors 0700. The focused tests then passed. Linux
inheritance and syscall restrictions are separate evidence, recorded below.

### Round-two observed results

The baseline source was also built in an isolated source archive with the new
Linux sandbox test. Under the actual filter, legacy repeat staging failed with
`chmodat sandbox-model: operation not permitted`. The permanent test now uses
a separate `legacy-sandbox-model` ID to also cover fresh parent creation. This
reproduces the setgid syscall conflict,
not an installed systemd service failure. The corrected implementation passes
with the restriction active; it does not disable the service sandbox.

| Check actually run | Observed result |
|---|---|
| `env GOTOOLCHAIN=local CGO_ENABLED=0 go test ./internal/adapters ./internal/config ./internal/api ./internal/auth -count=1` | PASS |
| Supplied `umask 0077` + uncached `TestStageVerifyAtomicAndCorrupt` command | PASS |
| Uncached `TestStagingPublishedPermissionsUnderServiceUmask`, `TestPublicationFailureRetainsVerifiedPrivateSnapshot`, `TestStagingRepairRefusesUnverifiedSnapshot` | PASS; 0022/0077 subprocesses, fresh/existing parents, nested files, second revision, legacy/restrictive repair and unverified-tree refusal |
| Uncached `TestLiveBrowserSessionsRejectUnsafeManagementIdentity` | PASS on macOS and Linux; complete positive policy baseline and exact negative errors |
| Linux `TestCompleteLiveBrowserConfigurationOnLinux`, `TestCompleteLiveCLILoopbackConfigurationOnLinux` | PASS; full configuration validation, not live adapter startup |
| `BRIDGE_STAGING_SANDBOX_RUN=1 bash scripts/test-staging-restrict-sxid-linux.sh` | PASS; `TestStagingRestrictSUIDSGID` and `TestStagingRestrictSUIDSGIDReaderProbe` on Linux arm64, kernel `6.12.76-linuxkit` |
| `GOOS=linux GOARCH=amd64` and `arm64` adapter test cross-compilation with `GOTOOLCHAIN=local CGO_ENABLED=0` | PASS; amd64 syscall execution remains NOT RUN |
| `bash -n scripts/test-staging-restrict-sxid-linux.sh` and ShellCheck | PASS; no lint suppression |
| `make check` | PASS: formatting, vet, tests, supported race checks, generation/OpenAPI/manifests, builds, module integrity and govulncheck (`No vulnerabilities found`) |
| `make browser` | PASS: Chrome 152.0.7977.82 / Node v26.8.1, existing API cookie policy over the isolated demo adapter, certificate pinning, identity separation, CSRF/expiry/logout, configuration/apply and failure recovery |
| `make package` and `shasum -a 256 -c SHA256SUMS` from `dist` | PASS; all three archives, static Linux amd64 server/helper/worker/CLI and macOS arm64/amd64 CLI |
| `tar -tzf` / `tar -xOf` package inspection | PASS; packaged service retains UMask 0077, RestrictSUIDSGID and the writable-path allowlist; examples retain CLI-only HTTP and dedicated HTTPS browser policy |
| Documentation audits and `git diff --check` | PASS; preserved user prompt and new untracked source also inspected directly |

The Linux tests used a pre-existing digest-pinned image, an empty Docker
credential directory, no network or host mounts, a read-only container, no
capabilities, bounded tmpfs/memory/PIDs and synthetic unprivileged writer/reader
identities. The writer was not a member of the reader group. A process-wide
TSYNC seccomp filter rejected set-ID chmod/fchmod controls and allowed ordinary
0750/0640 modes. Both publication and its receipt were readable but not writable
by the reader. All retained partial model files and the prepared partial receipt
returned EACCES. See [exact owner command and limitations](REMEDIATION.md#staging-sandbox-qualification).

Initial Linux attempts failed because the Docker writable layer was full;
unrelated Docker state was not pruned. Bounded container-only tmpfs provided the
test filesystem without loosening security controls. One intermediate reader
probe selected the same revision deliberately damaged by the wrong-GID test;
it correctly failed. The probe now uses a separately verified second revision,
while the damaged fixture remains evidence. Initial amd64 test compilation
found that Go's syscall package lacks `SYS_SECCOMP` on that architecture; the
test-only arch files now use the verified native syscall numbers. ShellCheck
initially warned about container-shell variables in quoted strings; quoted
heredocs resolved the warning without suppressing it. All affected checks were
rerun after those fixture corrections.

After recording results, `go run ./cmd/bridge-package` refreshed only the archives
to include the final documentation, followed by checksum verification. The
existing packager includes the preserved remediation prompt Markdown; no
artifacts were published. The sandbox runner's temporary binary is removed and
its container expires; a bounded baseline source/test archive remained in local
temporary storage when cleanup was refused. It contains no credentials and is
not application recovery state. No cleanup bypass was attempted.

**NOT RUN:** complete installed systemd sandbox and helper visibility; target
amd64 syscall-filter runtime; filesystem/PVC/ACL/reader-group deployment mapping;
durable-media crash qualification; real cgroup enforcement; GPU/model/ROCm or
workstation-performance qualification. `make check` explicitly reported that
compatible Linux systemd and Kubernetes admission/scheduling/RBAC enforcement
require target qualification. Workflow source and the installer were unchanged,
so workflow lint, installer checks and native Arch package installation were
not repeated. No live system, credentials, Git history or journal was changed.

## Five-finding remediation from 264e09e (historical)

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
| 1. Publication umask | `internal/adapters/staging*`: explicit reader modes, private partial parent, exact-tree/receipt/hash verification and bounded repeat-stage repair; subprocess 0022/0077, nested/existing parents, failure and repeat tests | PASS, including synthetic cross-UID Linux access under 0077; root-run staging did not test RestrictSUIDSGID | Superseded permission design: round two fixes the service-sandbox conflict. Installed workload GID/ACL/PVC mapping and filesystem durability remain unqualified |
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
