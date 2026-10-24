#!/usr/bin/env bash
# Deploy the static presyo binary to the VPS.
#
# Usage:
#   ./deploy.sh              # test, build, upload, atomically swap, verify
#   DRY_RUN=1 ./deploy.sh    # print every command without running anything
#   VPS_HOST=my-vps ./deploy.sh   # override the ssh config host (default: <VPS_HOST>)
#
# What it does:
#   1. runs `go test ./...` (fails the deploy on red tests),
#   2. builds a static linux/amd64 binary with the git version stamped in
#      (`-X main.version=<git describe>`),
#   3. aborts unless `ssh $VPS_HOST uname -m` reports x86_64,
#   4. copies the binary to $REMOTE_DIR/bin/presyo.new, then atomically
#      `mv`s it over $REMOTE_DIR/bin/presyo,
#   5. prints the remote `presyo version`, then the suggested crontab lines.
#
# This script NEVER touches crontab and NEVER runs as root.
set -euo pipefail

VPS_HOST="${VPS_HOST:-<VPS_HOST>}"
REMOTE_DIR="${REMOTE_DIR:-$HOME}"
REMOTE_BIN="${REMOTE_DIR}/bin/presyo"
DRY_RUN="${DRY_RUN:-0}"

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
run ssh "${VPS_HOST}" "${REMOTE_BIN} version"

cat <<'EOF'
deploy done. This script never touches crontab; install the schedule with
`crontab -e` on the VPS (TZ <SERVER_TZ> = UTC+9; DA posts evenings Manila
time; the old Python GitHub Action ran at 22:00 UTC = 07:00 server-TZ):

<CRON_LINE_REMOVED>

Optional second (evening) run:

<CRON_LINE_REMOVED>
EOF
