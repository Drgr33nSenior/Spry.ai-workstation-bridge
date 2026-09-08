#!/usr/bin/env bash
set -euo pipefail

# Runs only synthetic identities and an in-memory model fixture. It does not
# mount the workstation, use the network, pull images, or start a service.
if [[ ${BRIDGE_STAGING_SANDBOX_RUN:-} != 1 ]]; then
  printf '%s\n' 'Set BRIDGE_STAGING_SANDBOX_RUN=1 to run this disposable Linux qualification.' >&2
  exit 2
fi
bridge_image=${BRIDGE_STAGING_SANDBOX_IMAGE:-golang@sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b}
bridge_docker=${DOCKER_BIN:-docker}
# Never follow a developer's remote Docker context or TLS credential references.
DOCKER_HOST=${BRIDGE_STAGING_SANDBOX_DOCKER_HOST:-unix:///var/run/docker.sock}
if [[ $DOCKER_HOST != unix:///* || ! -S ${DOCKER_HOST#unix://} ]]; then
  printf '%s\n' 'An explicitly reviewed local Docker Unix socket is required.' >&2
  exit 2
fi
export DOCKER_HOST
unset DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH
if [[ ! $bridge_image =~ @sha256:[a-f0-9]{64}$ ]]; then
  printf '%s\n' 'The pre-existing fixture image must be digest-pinned.' >&2
  exit 2
fi
bridge_temp=$(mktemp -d)
bridge_binary="$bridge_temp/staging-sandbox.test"
bridge_docker_config="$bridge_temp/docker-config"
mkdir "$bridge_docker_config"
export DOCKER_CONFIG="$bridge_docker_config"
bridge_arch=$($bridge_docker image inspect "$bridge_image" --format '{{.Architecture}}')
case "$bridge_arch" in
  arm64|amd64) ;;
  *) printf '%s\n' "unsupported container architecture: $bridge_arch" >&2; exit 2 ;;
esac
bridge_container=''
cleanup() {
  rm -f -- "$bridge_binary"
  rmdir "$bridge_docker_config" 2>/dev/null || true
  rmdir "$bridge_temp" 2>/dev/null || true
}
trap cleanup EXIT

GOTOOLCHAIN=local GOOS=linux GOARCH="$bridge_arch" CGO_ENABLED=0 go test -c -o "$bridge_binary" ./internal/adapters
bridge_container=$($bridge_docker create --pull=never --rm --read-only --network none --pids-limit 128 --memory 256m --cpus 2 --user 21341:21343 --cap-drop ALL --tmpfs /sandbox:rw,exec,nosuid,nodev,size=64m,uid=21341,gid=21343,mode=0755 --tmpfs /models:rw,nosuid,nodev,size=16m,uid=21341,gid=21342,mode=2750 --tmpfs /tmp:rw,exec,nosuid,nodev,size=16m "$bridge_image" sleep 20)
$bridge_docker start "$bridge_container" >/dev/null
$bridge_docker exec -i "$bridge_container" sh -ceu 'dd of=/sandbox/staging-sandbox.test bs=1048576 status=none; chmod 0755 /sandbox/staging-sandbox.test' < "$bridge_binary"
$bridge_docker exec "$bridge_container" sh -ceu '
  umask 0077
  exec env \
    BRIDGE_STAGING_SANDBOX=1 \
    BRIDGE_STAGING_SANDBOX_ROOT=/models \
    BRIDGE_STAGING_SANDBOX_READER_GID=21342 \
    /sandbox/staging-sandbox.test -test.run "^TestStagingRestrictSUIDSGID$" -test.count=1 -test.v
'
bridge_partial_paths=$($bridge_docker exec -i "$bridge_container" sh -seu <<'CONTAINER'
  paths=
  for partial in /models/.partial-*; do
    test -d "$partial"
    test -f "$partial/snapshot/nested/weights.safetensors"
    paths="${paths}${paths:+:}$partial/snapshot/nested/weights.safetensors"
  done
  test -n "$paths"
  printf "%s" "$paths"
CONTAINER
)
bridge_partial_receipt=$($bridge_docker exec -i "$bridge_container" sh -seu <<'CONTAINER'
  for partial in /models/.partial-*; do
    if test -f "$partial/snapshot/.bridge-receipt.json"; then
      printf "%s" "$partial/snapshot/.bridge-receipt.json"
      exit 0
    fi
  done
  exit 1
CONTAINER
)
$bridge_docker exec --user 21344:21342 "$bridge_container" env \
  BRIDGE_STAGING_SANDBOX_READER_PROBE=1 \
  BRIDGE_STAGING_SANDBOX_READER_GID=21342 \
  BRIDGE_STAGING_SANDBOX_PUBLISHED=/models/sandbox-model/cccccccccccccccccccccccccccccccccccccccc/nested/weights.safetensors \
  BRIDGE_STAGING_SANDBOX_PUBLISHED_RECEIPT=/models/sandbox-model/cccccccccccccccccccccccccccccccccccccccc/.bridge-receipt.json \
  BRIDGE_STAGING_SANDBOX_PARTIALS="$bridge_partial_paths" \
  BRIDGE_STAGING_SANDBOX_PARTIAL_RECEIPT="$bridge_partial_receipt" \
  /sandbox/staging-sandbox.test -test.run '^TestStagingRestrictSUIDSGIDReaderProbe$' -test.count=1 -test.v
printf '%s\n' 'PASS: synthetic writer passed RestrictSUIDSGID staging; synthetic reader read publication and was denied partial access.'
