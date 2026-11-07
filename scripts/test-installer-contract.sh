#!/usr/bin/env bash
# Read a pinned local Git tree, export configuration privately, test compatibility.
# Does not run an installer, launch a client, or approve runtime hashes.
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
revision=0d7655fed8d7764e75063fd84c2f67fe2ab520fe
if [[ $# == 2 && $1 == --candidate ]]; then
  candidate=$(cd -- "$2" && pwd -P)
  work=$(mktemp -d)
  trap 'rm -rf -- "$work"' EXIT
  bash "$candidate/bin/workstationctl" agent configure "$work/bundle"
  cp "$candidate/versions.lock" "$work/versions.lock"
  cd "$root"
  BRIDGE_INSTALLER_CANDIDATE="$work" GOTOOLCHAIN=local go test -count=1 -v -run 'Test(Pinned|Candidate)InstallerContract$' ./internal/catalog
  exit
fi
[[ $# == 1 ]] || { printf 'usage: %s /path/to/installer-checkout\n' "$0" >&2; exit 1; }
[[ $(git -C "$1" rev-parse "$revision^{commit}") == "$revision" ]]
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
mkdir "$work/source"
git -C "$1" archive "$revision" bin lib templates versions.lock | tar -xf - -C "$work/source"
"$work/source/bin/workstationctl" agent configure "$work/bundle"
cp "$work/source/versions.lock" "$work/versions.lock"
printf '%s\n' "$revision" >"$work/revision"
cd "$root"
BRIDGE_INSTALLER_FIXTURE="$work" GOTOOLCHAIN=local go test -count=1 -v -run '^TestPinnedInstallerContract$' ./internal/catalog
