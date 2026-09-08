#!/usr/bin/env bash
# Build into disposable scratch; do not install the resulting package.
set -euo pipefail
if [[ $(id -u) == 0 || ! ${BRIDGE_VERSION:-} =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  printf '%s\n' 'An unprivileged user and stable vMAJOR.MINOR.PATCH version are required.' >&2
  exit 1
fi
make arch-source VERSION="$BRIDGE_VERSION"
cd dist/arch
sha256sum --check SHA256SUMS
export BUILDDIR
BUILDDIR=$(mktemp -d)
export PKGDEST="$PWD"
makepkg --verifysource
makepkg --cleanbuild --noconfirm
bridge_pkg="spry-ai-workstation-bridge-${BRIDGE_VERSION#v}-1-x86_64.pkg.tar.zst"
test -f "$bridge_pkg"
pacman -Qlp "$bridge_pkg"
bsdtar -tf "$bridge_pkg" > "$BUILDDIR/package-files.txt"
if grep -Eq '^(etc/|var/|\.INSTALL$|usr/lib/bridge/workstation-)' "$BUILDDIR/package-files.txt"; then
  printf '%s\n' 'Package contains forbidden active configuration, state, hooks or installer runtime.' >&2
  exit 1
fi
sha256sum PKGBUILD "spry-bridge-${BRIDGE_VERSION#v}-src.tar.gz" "$bridge_pkg" > SHA256SUMS
sha256sum --check SHA256SUMS
