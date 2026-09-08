# Arch Linux checks and packaging

## CI boundary

Main pushes and pull requests run Ubuntu, macOS and Arch userspace checks.
Only a newly created stable `vMAJOR.MINOR.PATCH` tag builds package artifacts.
The tag workflow waits for Ubuntu and macOS checks, then runs Arch checks and
`makepkg`. Cross-platform archives also depend on the Arch job succeeding.
Neither workflow installs Bridge, starts its services or publishes a release.

`.github/workflows/arch.yml` owns the official Arch image digest. The image is
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

The checked-in digest, archive date, action SHAs and Go version are the pins.
Rolling manual pages explain mechanisms; they are not immutable version pins.
