#!/usr/bin/env bash
# Read a pinned local Git tree, export configuration privately, test compatibility.
# Does not run an installer, launch a client, or approve runtime hashes.
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
revision=67a506090e8ecf696190e0be55f865e3ce054d0e
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
