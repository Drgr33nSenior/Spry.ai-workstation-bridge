# Arch Linux checks and packaging

## CI boundary

Main pushes and pull requests run one parallel Ubuntu, macOS and Arch test matrix.
Only a newly created stable `vMAJOR.MINOR.PATCH` tag builds package artifacts.
After tag validation, all three environments can start together. Separate
cross-platform archive and Arch-package jobs each wait for the complete matrix
to pass, then can run independently. Main/PR runs skip both packaging jobs.
Neither workflow installs Bridge, starts its services or publishes a release.

`.github/workflows/ci.yml` owns the matrix and packaging jobs, shared by the
main/PR and tag entry workflows. Its `arch-container` YAML anchor supplies one
image/resource definition to the Arch test row and Arch package job. Ubuntu
and macOS rows use an empty container value and run on their native runners.
Arch-only setup stays conditional in the matrix. A fresh Arch package job
prepares its own isolated container; no filesystem state is shared between jobs.

The Arch image is
the 2026-09-07 `base-devel` build, verified against the official registry on
2026-09-08. `scripts/ci-arch-setup.sh` selects the complete 2026-09-07 Arch Linux
Archive snapshot. It initializes the image's package keyring, retains signature
checks and performs a full package upgrade. Run this setup script only in a
disposable CI Docker container; it changes that container's package database.
Do not run it on the workstation.

The Go setup action selects `.go-version` independently of the snapshot's Go
package. The PKGBUILD checks the actual compiler version before building.
The dated snapshot makes package inputs repeatable; it is not a claim that the
current rolling distribution has identical packages. Review both the image and
archive date periodically, retaining the test gates. Vulnerability checks use
the current vulnerability database, so their results can change.

The container uses the runner's kernel and does not boot systemd. A passing job
does not qualify kernel behavior, service sandboxing, GPU access, handover,
worker cgroups, K3s or model performance. Follow [QUALIFICATION.md](QUALIFICATION.md)
on the actual workstation. A blocked container sandbox check must remain a
reported limitation, not a reason to disable the sandbox.

The shared matrix changes GitHub's displayed check names. If the owner has
configured named required checks, review branch protection after the first CI
run. These source changes do not modify repository settings.

## Generate and build a local package

Prerequisites: a reviewed Bridge source tree, Go matching `.go-version`, and
an Arch build environment with `base-devel` and the dependencies listed in the
recipe. Do not run `makepkg` as root. The repository does not install prerequisites
on the development machine.

```sh
make arch-source VERSION=v1.0.0
cd dist/arch
sha256sum --check SHA256SUMS
less PKGBUILD
makepkg --verifysource
makepkg --cleanbuild
pacman -Qlp spry-ai-workstation-bridge-1.0.0-1-x86_64.pkg.tar.zst
```

`make arch-source` creates a deterministic source snapshot and a concrete
`PKGBUILD` with its SHA-256 digest. It includes allowlisted application source,
tests, workflows, documentation and deployment examples, not Git/IDE metadata,
credentials, runtime state or existing binaries. Local generation snapshots the
current files, including uncommitted edits; it does not claim a commit identity.
In CI, checkout selects the tagged source. The version label alone is not proof
of provenance. Generation does not create a Git tag or publish anything.

The recipe downloads the checksummed Go modules during preparation, verifies
them, builds the four static binaries and runs the unit/integration suite before
packaging. CI runs race, contract and vulnerability checks separately beforehand.
Build failure does not install a partial package. Fix the input or environment,
then build again in a fresh scratch directory; do not skip failed checks.

The tagged Actions run provides a ready-built `.pkg.tar.zst`, source archive,
`PKGBUILD` and checksums in `spry-bridge-arch-v1.0.0`. Retention is seven days.
Checksums detect corruption; they are not independent package signatures. Verify
the repository, tag and successful workflow before accepting an artifact.

## Installation boundaries

For the personal ArchLinuxThreadripper ISO integration, the package also owns
`/usr/share/doc/spry-ai-workstation-bridge/source.sha256`. This identifies the
exact source archive consumed by the recipe. Keep that archive, PKGBUILD and
`.BUILDINFO` with the candidate; a version string or Git HEAD alone does not
identify a locally modified build. A new explicit output directory is supported:
`go run ./cmd/bridge-arch-package --version v0.0.0 --output /absolute/new/source`.

The installer independently checks dependencies against its selected snapshot
and packages its runtime/reference closure. Bridge's Go requirement is build-only.
Do not replace the installer's frozen snapshot with the CI snapshot. Run
`bash scripts/test-installer-contract.sh --candidate /absolute/installer/export`
alongside the historical pinned test. The actual package's `bridge-hostd
--check-reference PATH --check-native BUNDLE` checks catalog/native compatibility
without starting services or authorizing executable hashes. `--manifest PATH`
remains an offline inspection, not a policy approval.

The package installs binaries under `/usr/lib/bridge`, a `bridgectl` link under
`/usr/bin`, systemd units and the canonical sysusers/tmpfiles definitions.
Examples remain under `/usr/share/spry-ai-workstation-bridge/examples`; they are
not copied to `/etc/bridge`. Documentation is under
`/usr/share/doc/spry-ai-workstation-bridge`.

There is no Bridge install hook, service enable/start command or policy generator.
Arch's system hooks can create the declared service accounts/directories and
reload unit definitions when the owner installs the package. Review the package
before installation. Resolve the allocated UIDs afterward and use those values
in the independently reviewed server, helper and worker policies. Package
dependency satisfaction does not qualify a live tool or grant helper authority.

The installer runtime/reference bundle, kubeconfig, credentials, models and
state remain separately owned. Follow [OPERATIONS.md](OPERATIONS.md) to provision
them and authorize service startup. Before an upgrade, use its stop, backup and
recovery procedure; pacman installation is not an application-state migration.
Package removal must not be used to delete retained model or recovery state.

No project licence has been selected. Do not submit this package to the AUR or
distribute it commercially until the owner decides the terms and the recipe and
payload include the corresponding licence metadata/text. No terms are inferred.

## Primary references

Verified 2026-09-08:

- [Official Arch container sources](https://github.com/archlinux/archlinux-docker)
  and [Arch archive snapshot](https://archive.archlinux.org/repos/2026/09/07/).
- [PKGBUILD manual](https://man.archlinux.org/man/PKGBUILD.5.en) and
  [makepkg manual](https://man.archlinux.org/man/makepkg.8.en).
- [GitHub reusable workflows](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows).
- [Matrix jobs](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/run-job-variations)
  and [YAML anchors](https://docs.github.com/en/actions/reference/workflows-and-actions/reusing-workflow-configurations#yaml-anchors-and-aliases).

The checked-in digest, archive date, action SHAs and Go version are the pins.
Rolling manual pages explain mechanisms; they are not immutable version pins.
