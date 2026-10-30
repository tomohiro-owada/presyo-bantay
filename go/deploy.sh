#!/usr/bin/env bash
# Deploy the static presyo binary to the VPS.
#
# Usage:
#   ./deploy.sh              # test, build, upload, atomically swap, verify
#   DRY_RUN=1 ./deploy.sh    # print every command without running anything
#   VPS_HOST=<ssh-host> ./deploy.sh   # REQUIRED: ssh config host/alias (never hard-coded)
#
# Target config: VPS_HOST (ssh host/alias, required except with DRY_RUN=1)
# and REMOTE_DIR (default: the remote home directory) come from the
# environment or from an untracked `go/.deploy.env` (git-ignored) holding
# plain `VPS_HOST=...` / `REMOTE_DIR=...` lines. No host, user, IP or path of
# any server is stored in this repository.
#
# What it does:
#   1. runs `go test ./...` (fails the deploy on red tests),
#   2. builds a static linux/amd64 binary with the git version stamped in
#      (`-X main.version=<git describe>`),
#   3. aborts unless `ssh $VPS_HOST uname -m` reports x86_64,
#   4. copies the binary to $REMOTE_DIR/bin/presyo.new, then atomically
#      `mv`s it over $REMOTE_DIR/bin/presyo,
#   5. copies scripts/daily.sh and scripts/watch.sh to $REMOTE_DIR/bin/
#      (chmod 755),
#   6. prints the remote `presyo version`.
#
# This script NEVER touches crontab and NEVER runs as root.
set -euo pipefail

DEPLOY_ENV="$(dirname "$0")/.deploy.env"
if [ -f "$DEPLOY_ENV" ]; then
  # shellcheck disable=SC1090
  . "$DEPLOY_ENV"
fi
DRY_RUN="${DRY_RUN:-0}"
if [ -z "${VPS_HOST:-}" ]; then
  if [ "${DRY_RUN}" = "1" ]; then
    VPS_HOST="<vps-host>"
  else
    echo "error: VPS_HOST is not set (ssh host/alias); export it or put it in go/.deploy.env" >&2
    exit 1
  fi
fi
REMOTE_DIR="${REMOTE_DIR:-.}"
REMOTE_BIN="${REMOTE_DIR}/bin/presyo"

if [ "$(id -u)" = "0" ]; then
  echo "error: do not run deploy.sh as root" >&2
  exit 1
fi

cd "$(dirname "$0")"

VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"

# run prints the command, then executes it unless DRY_RUN=1.
run() {
  printf '+ %s\n' "$*"
  if [ "${DRY_RUN}" != "1" ]; then
    "$@"
  fi
}

run go test ./...

run env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -X main.version=${VERSION}" \
  -o bin/presyo ./cmd/presyo

if [ "${DRY_RUN}" = "1" ]; then
  echo "+ ssh ${VPS_HOST} uname -m  # expect x86_64, abort otherwise"
else
  ARCH="$(ssh "${VPS_HOST}" uname -m)"
  echo "+ ssh ${VPS_HOST} uname -m  -> ${ARCH}"
  if [ "${ARCH}" != "x86_64" ]; then
    echo "error: VPS arch is ${ARCH}, want x86_64" >&2
    exit 1
  fi
fi

run ssh "${VPS_HOST}" "mkdir -p ${REMOTE_DIR}/bin ${REMOTE_DIR}/logs"
run scp bin/presyo "${VPS_HOST}:${REMOTE_BIN}.new"
run ssh "${VPS_HOST}" "mv ${REMOTE_BIN}.new ${REMOTE_BIN}"
run scp scripts/daily.sh "${VPS_HOST}:${REMOTE_DIR}/bin/daily.sh"
run ssh "${VPS_HOST}" "chmod 755 ${REMOTE_DIR}/bin/daily.sh"
run scp scripts/watch.sh "${VPS_HOST}:${REMOTE_DIR}/bin/watch.sh"
run ssh "${VPS_HOST}" "chmod 755 ${REMOTE_DIR}/bin/watch.sh"
run scp scripts/weekly.sh "${VPS_HOST}:${REMOTE_DIR}/bin/weekly.sh"
run ssh "${VPS_HOST}" "chmod 755 ${REMOTE_DIR}/bin/weekly.sh"
run scp scripts/utilities.sh "${VPS_HOST}:${REMOTE_DIR}/bin/utilities.sh"
run ssh "${VPS_HOST}" "chmod 755 ${REMOTE_DIR}/bin/utilities.sh"
run ssh "${VPS_HOST}" "${REMOTE_BIN} version"

echo "deploy done. This script never touches crontab (see your private ops notes for the schedule)."
