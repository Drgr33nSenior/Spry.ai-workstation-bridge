# Testing and verification

This guide defines repeatable source checks and the boundary between fixture
tests and workstation qualification. Keep run logs, source hashes and test
results with the private build evidence or CI artifacts, not in this guide.
Use [QUALIFICATION.md](QUALIFICATION.md) for owner-run target acceptance.

## Prerequisites

- Use the exact Go version in `.go-version`. Release builds use
  `CGO_ENABLED=0`; race tests require a supported C compiler.
- Keep dependency and tool pins in `go.mod` and `go.sum`. Do not update them
  merely to obtain a passing check.
- Source discovery requires ripgrep. Browser tests require Node and an installed
  compatible Chrome; set `CHROME_BIN` if it is not at the platform default path.
- API/helper integration tests require temporary loopback listeners and Unix
  sockets. Use an environment that permits them; do not bypass peer checks,
  authentication or filesystem permissions.
- Installer checks require the tools listed by its Makefile and testing guide.
  Select a prepared Python environment with the required rendering dependencies.
  Do not use the developer's kubeconfig, private credentials or live cluster.

Telemetry deliberately rejects inherited `OTEL_*` overrides. Run tests in a
child environment without those overrides; do not weaken service policy:

```sh
/bin/bash --noprofile --norc -c '
  while IFS= read -r task_name; do
    case "$task_name" in OTEL_*) unset "$task_name";; esac
  done < <(compgen -e)
  exec "$@"
' bridge-check make check
```

This changes only the child environment and does not print variable values.

## Source gates

From the Bridge checkout:

```sh
make check
make browser
make package
(cd dist && shasum -a 256 -c SHA256SUMS)
git diff --check
```

`make check` runs the exact-toolchain, formatting, vet, Go tests, race,
generated-contract, OpenAPI, deployment-source, build, dependency verification,
vulnerability and applicable systemd gates. Run changed packages uncached
during development:

```sh
env GOTOOLCHAIN=local CGO_ENABLED=0 go test ./internal/PACKAGE -count=1
env GOTOOLCHAIN=local CGO_ENABLED=1 go test -race ./internal/PACKAGE -count=1
```

Replace `PACKAGE` with the affected package. A test skip is an exclusion, not
a pass. If a required tool is unavailable, report the missing check separately.
Do not suppress findings or add retry-until-green behavior.

Change canonical Go contract sources before regenerating `api/openapi.json`:

```sh
make generate
make generated openapi manifests
```

Inspect generated changes and all new files; `git diff` alone omits untracked
files. Run workflow lint when workflow source changes. Documentation audits are
advisory clarity checks, not security or standards certification.

## Browser tests

`make browser` drives the actual embedded UI using a test-only server with
isolated demo adapters and synthetic owner credentials. It exercises browser
authentication, exact plan confirmation, source/live outcome separation,
evidence export, recovery, refresh, narrow layouts, expiry, logout and CSRF.

The fixture uses an ephemeral hostname-bound certificate and narrow test-key
trust. It does not disable general TLS verification or alter global trust.
A separate loopback hostname checks cookie identity separation. HTTPS does not
isolate cookies by port; live management needs a dedicated trusted hostname.

Do not describe handler tests as browser validation. The fixture does not
establish installed TLS trust, complete accessibility, other-browser support
or actual GPU transitions.

## Selected installer compatibility

Keep the known-good catalog fixture separate from the current candidate:

```sh
bash scripts/test-installer-contract.sh /absolute/reviewed/installer
bash scripts/test-installer-contract.sh --candidate /absolute/reviewed/installer
```

The pinned command exports the retained fixture revision without checking out a
branch or running the installer. Candidate validation must additionally use a
fresh source export from the exact source selected for packaging:

```sh
# In the installer checkout; NEW_EXPORT must not exist.
bash infrastructure/packages/bootstrap/prepare-source.sh NEW_EXPORT
```

Extract that archive into a new private directory. Verify its archive checksum
and every entry in `SOURCE-MANIFEST.sha256`. From Bridge, pass the extracted
`project` directory, not an installed runtime directory:

```sh
env GOTOOLCHAIN=local CGO_ENABLED=1 \
  BRIDGE_INSTALLER_MEMORY_CANDIDATE=/absolute/extracted/project \
  BRIDGE_INSTALLER_PERFORMANCE_CANDIDATE=/absolute/extracted/project \
  go test -race ./internal/memory ./internal/performance \
  -run '^TestCandidateInstaller(Memory|Performance)Contract$' -count=1 -v
bash scripts/test-installer-contract.sh --candidate /absolute/extracted/project
```

Without the candidate variables, these pair tests skip. Ordinary Bridge CI
alone therefore cannot establish selected-pair compatibility. The installer
release builder requires the named tests against its selected source identity.

The performance test round-trips all supported bundle kinds through the actual
installer CLI and Go validators. It checks successful unqualified plans, unknown
and incomplete evidence, refusal artifacts, exact hashes and nonempty reasons.
The memory helper tests exercise production telemetry argument construction;
manually adding a planner option in a cross-repository test is not sufficient.

In the extracted installer tree, run the runtime-only closure test with the
prepared Python environment (PyYAML) and `kubectl` available:

```sh
env PYTHONDONTWRITEBYTECODE=1 HOME_LAB_PYTHON=python3 \
  python3 tests/workstation/test_telemetry.py \
  TelemetryTests.test_runtime_allowlist_closes_both_telemetry_profiles
```

This test materializes only `bridge-runtime.files`, renders and verifies both
telemetry profiles, and checks host-profile assets. It does not install them,
run collector images, contact K3s or measure collector memory.

Test the installed runtime closure separately: materialize only the files in
`infrastructure/packages/bootstrap/bridge-runtime.files`, then verify the
supported renderers and host assets from that tree. The broader source archive
cannot prove that every required runtime file will be installed.

Source hashes identify test inputs. They do not approve execution by the root
helper or replace administrator-reviewed installed artifact hashes.

## Packaging and Linux boundaries

Inspect each archive's binaries, architecture, configuration, service units,
API contract and operator guides. Compare packaged examples with their sources.
Packages must not contain credentials, model weights or private test results.
Cross-built Linux executables are not proof of Linux runtime behavior.

On a prepared compatible Linux environment, run `make systemd` and the
applicable Unix-peer, model-reader, namespace and delegated-cgroup tests.
Follow the disposable-test prerequisites in [QUALIFICATION.md](QUALIFICATION.md).
Do not install services, grant capabilities, alter ancestor cgroups or disable
sandbox controls to make a source check pass.

Kubernetes source rendering and fake-client tests do not establish admission,
RBAC, scheduling, device-plugin behavior or node visibility. Actual model
loading, memory sizing, native builds, queue cancellation, gaming handover and
performance require repeated target trials with current identities and retained
rollback evidence. Use [PERFORMANCE.md](PERFORMANCE.md) and
[MEMORY-BUDGETS.md](MEMORY-BUDGETS.md); no synthetic result establishes a speedup
or safe RAM reduction.
