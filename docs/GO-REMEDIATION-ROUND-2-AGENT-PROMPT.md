# Bridge remediation, round two: implementation-agent prompt

Work in `/Users/uk-gr9yjx0l0y/GolandProjects/Spry.ai-workstation-bridge`.
Verify the current location, Git status, revision and applicable repository
instructions. The review baseline is `b21c86d`, reviewed on 8 September 2026.
Line numbers below refer to that revision and may have moved.

Implement and validate the two fixes below. An audit, proposed plan or
documentation-only change does not complete this task. Reproduce the defects
where the environment permits, add permanent regression coverage, and continue
through implementation. If a finding is already fixed, verify it instead of
duplicating the change.

## Scope and safety

Preserve the Go API, CLI and embedded UI, the separate root helper and build
worker, and the existing configuration authority, durable journals and deployment
architecture. Read the relevant source, tests, `docs/REMEDIATION.md`,
`docs/OPERATIONS.md`, `docs/HOST-EXECUTOR.md` and `docs/VERIFICATION.md` first.

The previous review found the restore-chain, executor-specific recovery and
browser-session fixes sound within the tested boundaries. Cgroup initialization
is implemented and fixture-tested, but actual kernel enforcement is unqualified.
Keep those fixes and their tests. Do not reopen the earlier five-finding tranche
unless a demonstrated interaction requires a small correction.

- Preserve unrelated changes. Do not stage, commit or push.
- Do not introduce another service, privileged helper, framework, database,
  implementation language or unrelated dependency upgrade.
- Do not deploy, run the installer, modify installed services, access a live
  cluster, change disks/boot settings or retrieve credentials.
- Do not disable sandbox controls, grant capabilities, run the application as
  root, alter global certificate trust or start privileged containers to pass tests.
- Use isolated temporary fixtures, synthetic credentials and tiny model inputs.
  Do not download real model weights or run full workstation builds.
- Do not clear recovery flags or delete journals, partial downloads or snapshots
  as a shortcut. Preserve evidence and audit history.
- Ask only when a missing choice materially changes behavior and cannot be
  resolved from the repository. Continue unrelated work meanwhile.

## 1. Make model staging compatible with its service sandbox — P1

Evidence at the baseline:

- `internal/adapters/staging.go:51` defines published directory mode as setgid
  plus `0750`; line 55 defines private directory mode as setgid plus `0700`.
- `staging.go:569` explicitly applies the private mode before downloading;
  line 579 repeats it for the snapshot directory.
- Lines 411 and 442 explicitly apply the published mode during publication/repair.
- `deployment/systemd/bridged.service:33` sets `RestrictSUIDSGID=yes`.

Systemd filters chmod-family calls that request setgid, including requests that
only preserve an inherited bit. Thus the permission fix passes standalone umask
tests but conflicts with the installed service: fresh staging fails before the
first download and repeat-stage permission repair also fails.

Verify the relevant current primary source and target semantics:
[systemd seccomp implementation](https://github.com/systemd/systemd/blob/main/src/shared/seccomp-util.c),
particularly `seccomp_restrict_sxid()` and `seccomp_restrict_suid_sgid()`.
The review established this conflict from source; it did not execute the Linux
service sandbox. Do not describe it as an observed target failure.

Required behavior:

1. Keep `UMask=0077`, `RestrictSUIDSGID=yes`, the unprivileged service identity and
   existing privilege boundaries. Resolve the directory design against these
   constraints rather than disabling the restriction.
2. Preserve the administrator-approved reader GID on every required inode.
   Workloads must be able to traverse published directories and read verified
   model files/receipts without receiving write access.
3. Do not assume every published directory must retain setgid. Distinguish GID
   inheritance during creation from reader access after publication. Trace new
   parents, nested directories, later revisions and existing snapshots before
   choosing the implementation. Avoid adding another permission mechanism unless
   a demonstrated need justifies its provisioning and migration costs.
4. Keep incomplete data unreachable through a private ancestor throughout
   download, permission preparation and failure handling, until atomic publication.
   Do not expose partial content while preparing final reader permissions.
5. Retain exact-tree, receipt, hash, group and path-safety verification. Repeated
   staging must not redownload an already valid snapshot, overwrite a corrupt
   snapshot or alter unrelated paths.
6. Handle snapshots produced before and after the first remediation. Explain
   which existing modes can be accepted or safely repaired. If safe automatic
   repair is impossible, refuse with a bounded owner-run offline procedure; do
   not require impossible live chmod calls or broad recursive chmod/chown.
7. Check the full service contract for other interactions introduced by this
   fix. Do not limit validation to one permission assertion.

Permanent tests must cover:

- Fresh model parent, fresh revision and nested manifest-listed files under
  `0077`, with the intended unprivileged writer and distinct reader identities
  where an isolated Linux environment permits them.
- Existing published directories from the previous `2750` design and older
  restrictive modes; valid repeat staging, a second revision and permitted repair.
- Private partial data during transfer and after failure, including failure after
  final permissions are prepared but before the publication rename.
- Wrong reader GID, corrupt/missing receipt, unexpected entries and existing path
  safety cases. Unsafe repair must refuse without overwriting the evidence.
- Behavior under the actual service sandbox or demonstrably equivalent
  chmod/setgid syscall restrictions. A root-run downloader followed by a reader
  probe, an umask-only test or a mocked chmod error is not sufficient evidence.

Any Linux runtime test must be opt-in and restricted to an explicitly authorized,
disposable environment. It must not alter the installed Bridge unit, model root
or host-wide controls. A test-only child process may apply restrictive syscall
filters to itself where supported. Document any differences from the packaged
unit; do not present a partial filter as full systemd qualification.

If no suitable Linux runtime is available, finish the portable implementation
and tests, provide a bounded owner-run sandbox qualification command, and mark
that runtime check **NOT RUN**. Do not claim deployment readiness.

## 2. Make browser-security configuration tests fail for the right reason — P2

Evidence:

- `internal/config/config_test.go:26` adds
  `TestLiveBrowserSessionsRequireDedicatedHTTPSIdentity`.
- Each negative case starts from incomplete demo fixture `good()` at line 58,
  changes selected fields to live mode, then accepts any validation error.
- Without the browser-policy checks, macOS still rejects live mode at
  `internal/config/config.go:267`. Linux instead rejects missing live adapter paths.

These tests can therefore remain green after the intended security validation is
removed. This is a regression-coverage defect, not evidence of a current live
authentication bypass. Preserve the working dedicated-HTTPS, CLI-only HTTP and
session-invalidation behavior.

Required behavior:

1. Establish a complete valid baseline before testing one invalid property at a
   time. Assert the intended browser-policy error, not merely a non-nil error.
2. Keep platform restrictions intact. If full live validation cannot succeed on
   macOS, test a small platform-independent policy function and retain separate
   Linux full-configuration coverage. Do not bypass Linux checks in production.
3. Cover refusal of live browser sessions over plaintext loopback, HTTPS IP
   literals, localhost and single-label management names. Test required TLS
   configuration without letting unrelated fixture errors satisfy the assertion.
4. Cover acceptance of a complete dedicated-DNS HTTPS browser configuration,
   CLI-only live loopback configuration and isolated demo browser configuration
   at the appropriate policy/integration layers.
5. Demonstrate test sensitivity: temporarily remove or invert the relevant policy
   check in an isolated test copy/overlay and show that the focused tests fail.
   Restore that test-only mutation before final checks. Do not commit weakened
   validation, alter legitimate expected results or add a mutation framework.

Keep the existing API and real-browser regression tests. Preserve Secure,
HttpOnly, host-only cookies, CSRF/Origin/Host validation, expiry, revocation,
pre-upgrade session invalidation and the separation of live and demo identities.
HTTPS does not isolate cookies by port. Do not weaken certificate verification
or use actual credentials in the tests.

## 3. Check the observed workflow-test timeout without masking failures

The first review run of `make check` failed in the race stage:

```text
TestUnifiedCheckMatrixGatesIndependentTagPackages
ubuntu checks did not propagate exit 0: signal: killed
```

Its shell fixture uses a two-second context deadline in
`internal/integration/workflow_test.go:350`. The isolated retry and complete rerun
passed. This is an intermittent validation warning, not a confirmed race report
or evidence that the workflow itself is broken.

Run a small, bounded set of uncached race-enabled repetitions. Inspect timeout
versus command failure if it recurs. If a focused test-harness correction is
justified, preserve bounded execution and all assertions for success, failure
propagation and packaging gates. Do not add retry-until-green logic, unlimited
timeouts, test skips or weaker workflow conditions. If it does not recur, report
that fact and the original warning; do not invent a cause.

## Verification and completion

Use the pinned toolchain and existing commands. Run targeted new tests uncached,
then run the required checks without unnecessary concurrent build load:

```sh
env GOTOOLCHAIN=local CGO_ENABLED=0 go test ./internal/adapters ./internal/config ./internal/api ./internal/auth -count=1
(
  umask 0077
  env GOTOOLCHAIN=local CGO_ENABLED=0 go test ./internal/adapters \
    -run '^TestStageVerifyAtomicAndCorrupt$' -count=1
)
env GOTOOLCHAIN=local CGO_ENABLED=1 go test -race ./internal/integration \
  -run '^TestUnifiedCheckMatrixGatesIndependentTagPackages$' -count=3 -v
make check
make browser
make package
```

Also run the newly added sandbox/configuration tests by their actual names.
Verify archive checksums and review packaged service/configuration examples.
Run workflow lint if workflow source changes. Update canonical schemas or
generators before generated output if a contract change is necessary.

Update `docs/REMEDIATION.md`, `docs/VERIFICATION.md` and the relevant setup or
qualification instructions to describe the actual permissions design, migration,
sandbox test and remaining exclusions. Preserve historical results, but make it
clear that prior umask/cross-UID tests did not establish service-sandbox compatibility.

Finish with:

- Each issue, its root cause, changed paths and regression tests.
- Commands actually run, observed results and any initial failures/retries.
- Evidence that the browser-policy tests detect removal of the relevant checks.
- Exact bounded migration/repair and sandbox qualification commands for the owner.
- Explicitly separated portable, Linux sandbox and target-hardware evidence.
- Confirmation that no live system, credentials or Git history was changed.

Review the final diff and preserve unrelated work. Stop after these fixes and
their necessary tests/documentation. Do not claim GPU performance, cgroup
enforcement or live deployment readiness from portable tests and cross-builds.
