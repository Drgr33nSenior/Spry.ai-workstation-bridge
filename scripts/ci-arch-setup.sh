#!/usr/bin/env bash
# Only for disposable CI containers. Never run this on the workstation.
set -euo pipefail
if [[ ! -f /.dockerenv || $(id -u) != 0 ]]; then
  printf '%s\n' 'Refusing setup outside a root-owned disposable Docker container.' >&2
  exit 1
fi
# Keep package signatures enabled and perform a full upgrade, not a partial one.
# The previous complete daily snapshot stays fixed until explicitly reviewed.
printf '%s\n' "Server = https://archive.archlinux.org/repos/2026/09/07/\$repo/os/\$arch" > /etc/pacman.d/mirrorlist
pacman-key --init
pacman-key --populate archlinux
pacman -Syyu --noconfirm --needed go git jq ripgrep bubblewrap
useradd --create-home --shell /usr/bin/bash bridge-ci
pacman -Q
