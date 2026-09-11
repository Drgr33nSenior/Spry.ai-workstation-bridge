# Spry.ai Workstation Bridge

A private Go management application for one Arch Linux workstation. The embedded
web UI and Go CLI use the same authenticated, versioned API. The controller runs
outside K3s. GPU changes pass through a restricted host executor; builds run in a
separate unprivileged worker.

The fixture demo runs on macOS or Linux without root, Kubernetes, GPUs, private
endpoints or the installer. Live adapters use the reviewed installer contracts.
Workload qualification remains an independent owner-controlled prerequisite.
This repository does not install services or expose public management ingress.

See the [architecture diagrams](docs/STACK.md) for the private management plane,
the K3s platform and the AI, RAG and gaming workload paths.

Live browser management requires explicit `browser_sessions: true` and a
dedicated trusted HTTPS hostname. The live loopback HTTP example is CLI-only;
demo HTTP below is separate. See the [remediation and migration guide](docs/REMEDIATION.md)
for session invalidation, model permission repair and recovery-chain behavior.

Optional [private telemetry](docs/TELEMETRY.md) exports bounded OpenTelemetry
metrics and traces to an owner-configured collector. Export is disabled by
default. An owner-only summary reports local status and selected Prometheus
measurements without exposing arbitrary queries, credentials or raw audit data.

The owner-only [memory-budget workflow](docs/MEMORY-BUDGETS.md) imports sealed
evidence collected by the non-root workstation owner and exports unqualified
SGLang memory candidates. Private artifacts stay separate from sanitized views.
An optional OpenAI Agents API adviser is disabled by default; it can explain
selected evidence and request a plan, but cannot approve or apply one. Neither
workflow automatically changes running memory limits or grants qualification.

## Run the demo

Use Go **1.27.1**, as recorded in `.go-version`. No production Node.js runtime,
SQLite driver or CGO is required. The module identity comes from the existing
repository remote: `github.com/Drgr33nSenior/Spry.ai-workstation-bridge`.

```sh
make build
demo_dir="$(pwd -P)/.demo"
./bin/bridged --init-demo "$demo_dir"
./bin/bridgectl admin bootstrap --config "$demo_dir/bridge.json" --output "$demo_dir/owner.token"
./bin/bridged --config "$demo_dir/bridge.json"
```

Open **http://127.0.0.1:8743**. Enter the generated credential from the owner-only
file using a local editor. The browser exchanges it for a short-lived HttpOnly
session and clears the credential field. The credential is never printed by the
CLI. Stop the server before offline administration. An existing demo directory
is refused; use its existing configuration or choose a new directory.

In another terminal, set `demo_dir` to the same absolute directory:

```sh
demo_dir="$(pwd -P)/.demo"
./bin/bridgectl --context "$demo_dir/context.json" status
./bin/bridgectl --context "$demo_dir/context.json" models
./bin/bridgectl --context "$demo_dir/context.json" config
./bin/bridgectl --context "$demo_dir/context.json" operations
```

Use the UI to edit serving options, preview exact changes, confirm
`demo-workstation`, apply and inspect the resulting operation. Pages also cover
resources, AI/gaming/maintenance, builds, managed cache budgets, client profiles
and recovery. Every fixture view is labelled DEMO; it never falls back to live
adapters or qualifies hardware. Demo model staging uses tiny receipts, not model
weights. All live actions require a different administrator-owned configuration
and state directory.

The [CLI and UI guide](docs/CLI-AND-UI.md) contains tested model-to-plan/apply
examples, role-scoped credentials, local Qwen Code/DSH/Hermes configuration,
AI/gaming transitions, failure recovery and stable exit codes.

## Development and checks

Open this repository in GoLand as a Go module and select the Go SDK in
`.go-version`. Existing IDE metadata is preserved; no project SDK files are
rewritten. Create a Go Build configuration for `./cmd/bridged`, with program
arguments `--config /absolute/path/to/.demo/bridge.json` and this repository as
the working directory. Run the explicit initialization and bootstrap commands
above first. A second Go Build configuration can run `./cmd/bridgectl` against
the generated context. Do not put credentials into GoLand arguments or shared
run configurations.

```sh
make check       # formatting, vet, tests, race, contract, manifests, builds, vulnerabilities
make browser     # actual browser flows; needs installed Chrome and Node
make package     # Linux amd64 server/helper/worker/CLI; macOS arm64/amd64 CLI
```

`make package` writes local archives and checksums under `dist/`. It neither
installs nor publishes them. Race checks require a working local C compiler;
release binaries use `CGO_ENABLED=0`. Cross-compilation is not runtime validation.
Browser evidence records Chrome 152.0.7977.82 and Node 26.8.1; no npm packages,
CDNs, external fonts or telemetry services are needed by the application.

The generated [OpenAPI 3.1.1 contract](api/openapi.json) comes from Go wire types
and the route register in `internal/contract`. Run `make generate` after changing
the contract; `make generated openapi` checks reproducibility and validates the
specification using a pinned tool. Runtime OpenTelemetry dependencies, development
tools and their checksums are pinned in `go.mod`/`go.sum`. The telemetry guide
records the SDK identity, licence, configuration limits and verification boundary.

## CI and versioned build artifacts

| Event | Checks | Packaged artifact |
|---|---|---|
| Push to `main` | Unit/integration tests, race, formatting, vet, contract/manifests and vulnerability checks on Ubuntu, macOS and Arch userspace | None |
| Pull request targeting `main` | Same checks | None |
| Push a new stable version tag, such as `v1.0.0` | Validate the tag, then run the same checks | Build and upload only after Ubuntu, macOS and Arch checks pass |

Both workflows call `.github/workflows/ci.yml`, which owns one parallel test
matrix for Ubuntu, macOS and Arch. Each row runs the same checks. On valid new
tags, separate archive and Arch-package jobs each wait for the entire matrix
to succeed; neither packaging job waits for the other. Main/PR runs skip both
packaging jobs. A failed row does not cancel the other rows' diagnostics.

Only the Arch matrix row uses the official digest-pinned `base-devel` container
on an Ubuntu amd64 runner; Ubuntu and macOS run natively. The Arch package job
reuses that container definition. Arch packages come from the dated 2026-09-07 archive;
Go comes from the exact `.go-version` through the pinned setup action. Tests and
package builds run as an unprivileged container user. This checks Arch userspace,
not the Arch kernel, installed systemd services, K3s or workstation hardware.
Update the image and archive snapshot together through a reviewed change.

The accepted tag format is `vMAJOR.MINOR.PATCH`, using non-negative integers
without leading zeroes. Examples: `v0.1.0`, `v1.0.0`, `v12.3.4`. Tags such as
`v1.0`, `v01.0.0`, `v1.0.0-rc.1` and `v1.0.0+build.1` do not produce artifacts.
The pipeline deliberately accepts only the normal version form from
[SemVer 2.0.0](https://semver.org/spec/v2.0.0.html), with a `v` tag prefix.
Other tag names do not start the build workflow. Updates/deletions of existing
tags do not build; use a new version for changed source. A rerun of an original
creation event can retry a failed build without moving its tag.

After the reviewed source and workflows are committed and pushed, create and
push one unused version tag. These are owner commands, not actions performed
during implementation:

```sh
git tag -a v1.0.0 -m "Bridge v1.0.0"
git push origin v1.0.0
```

Open **Actions → versioned build → the successful run → Artifacts**. Download
`spry-bridge-v1.0.0`, which contains the Linux amd64 archive, both macOS CLI
archives and `SHA256SUMS`. Artifacts expire after seven days and use the account's
Actions storage allowance. Download a needed build before it expires; this is
not permanent GitHub Releases storage. The workflow does not create releases,
publish packages, deploy services or request repository secrets.

The same tagged run also produces `spry-bridge-arch-v1.0.0`: a pacman package,
the generated `PKGBUILD`, its versioned source archive and `SHA256SUMS`. The
canonical recipe is [packaging/arch/PKGBUILD.in](packaging/arch/PKGBUILD.in).
It uses the generated archive's real checksum, with no skipped integrity check.
See [Arch packaging](docs/ARCH-PACKAGING.md) for local builds and installation
boundaries. The licence decision remains pending; this is not an AUR submission.

The upload uses the SHA-pinned official
[actions/upload-artifact v7.0.1](https://github.com/actions/upload-artifact/tree/043fb46d1a93c77aae656e7c1c64a875d1fc6a0a)
action, verified on 2026-09-08. It runs only in CI and adds no application runtime
dependency. `internal/integration/workflow_test.go` checks the workflow structure
and executes the actual tag-validation script with accepted and rejected inputs.
Full workflow syntax validation was also run with `actionlint` **1.7.12**:

```sh
actionlint .github/workflows/check.yml .github/workflows/build.yml .github/workflows/ci.yml
```

Release tags identify application builds. They do not automatically change the
OpenAPI format version, API contract version or helper protocol version. Keep
`api/openapi.json` generated from `internal/contract`; CI checks that it agrees
with its canonical source.

If branch protection requires individual check names, review those settings
after the first run: the shared matrix changes the displayed job paths. This
repository change does not update GitHub branch protection or bypass its gates.

## Live operation

Start with [installation and recovery](docs/OPERATIONS.md), the
[configuration and trust decisions](docs/ARCHITECTURE.md),
[host executor contract](docs/HOST-EXECUTOR.md),
[reference contracts](docs/REFERENCE-CONTRACTS.md) and
[implementation matrix](docs/IMPLEMENTATION-MATRIX.md). The
[qualification checklist](docs/QUALIFICATION.md) specifies target commands and
failure recovery. [Verification evidence](docs/VERIFICATION.md) distinguishes
observed local checks from unperformed physical qualification.

`deployment/server.local.example.json` is local-only. The VPN example binds a
single documentation address and requires HTTPS plus a certificate matching
the management hostname. Replace it with a reviewed private interface or VPN
address, configure private name resolution separately and retain authentication.
There is no wildcard listener, source-IP administrator trust, TLS verification
bypass, public ingress manifest or inference proxy.

The source installer remains at
`/Users/uk-gr9yjx0l0y/Projects/ArchLinuxThreadripperAI`. The Bridge implementation
now resides in the attached GoLand checkout at
`/Users/uk-gr9yjx0l0y/GolandProjects/Spry.ai-workstation-bridge`.
Its existing Git and IDE metadata were preserved. The installer changes are limited to the session
lock compatibility and focused tests described in the host executor guide.

No project licence was present. **Owner licence decision pending.** No
distribution or commercial terms have been selected. Billing, customer tenancy,
public agent execution, marketplace functions, HA, inference proxying and public
game-streaming transport are outside this release. A commercial service would
need its own security assessment, support and upgrade policy, customer isolation,
legal/licensing decisions and sustained target qualification.
