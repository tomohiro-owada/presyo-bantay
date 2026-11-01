#!/usr/bin/env bash
# Hourly Grab fare Facebook post tick (GRABCARD_SPEC.md, "Hourly auto post").
#
# Runs on the ingest host (non-root) from cron at :10, :15 and :20 between
# 05:00 and 21:59 Manila. Each tick is one `presyo grabcard -auto` pass:
#   :10 card with the current hour's maps (exit 4 = maps not ready yet)
#   :15 retry
#   :20 maps if present, else the fare-only card; after that the hour is skipped
# It posts at most once per hour (state entry region "grab"), never posts
# stale data, and is OFF unless PRESYO_GRAB_FB=1 (set it in $ENV_FILE).
# The kill switch $PRESYO_HOME/.config/presyo/fbpost.disabled stops it too.
# Exit 4 ("nothing fresh yet") is normal and is turned into rc 0.
#
# Layout (all overridable via env for tests):
#   PRESYO_HOME=$HOME  BIN=$PRESYO_HOME/bin/presyo
#   ENV_FILE=$PRESYO_HOME/.config/presyo/grab.env
#   LOG=$PRESYO_HOME/logs/grabpost.log  LOCK=$PRESYO_HOME/.grabpost.lock
#
# Never uses sudo and never writes outside $PRESYO_HOME.
set -euo pipefail

PRESYO_HOME="${PRESYO_HOME:-$HOME}"
BIN="${BIN:-$PRESYO_HOME/bin/presyo}"
ENV_FILE="${ENV_FILE:-$PRESYO_HOME/.config/presyo/grab.env}"
LOG="${LOG:-$PRESYO_HOME/logs/grabpost.log}"
LOCK="${LOCK:-$PRESYO_HOME/.grabpost.lock}"

for p in "$BIN" "$ENV_FILE" "$LOG" "$LOCK"; do
  case "$p" in
    "$PRESYO_HOME"/*) ;;
    *)
      echo "error: path $p is outside PRESYO_HOME=$PRESYO_HOME" >&2
      exit 1
      ;;
  esac
done

mkdir -p "$PRESYO_HOME/logs" "$(dirname "$LOCK")"

exec 9>"$LOCK"
if ! flock -n 9; then
  echo "grabpost.sh: already running, exiting"
  exit 0
fi

if [ -f "$LOG" ]; then
  size="$(wc -c <"$LOG" 2>/dev/null || echo 0)"
  if [ "${size:-0}" -gt 2097152 ]; then
    mv -f "$LOG" "${LOG}.1"
  fi
fi

# shellcheck disable=SC2094
exec >>"$LOG" 2>&1

ts() { date '+%Y-%m-%d %H:%M:%S %Z'; }
echo "=== grabpost.sh start $(ts) ==="

# PRESYO_GRAB_FB (the gate), PRESYO_GRAB_POST_WINDOW (optional) and the API
# settings live in the env file; the secrets in it are never echoed.
if [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
fi

rc=0
PRESYO_HOME="$PRESYO_HOME" "$BIN" grabcard -auto -env-file "$ENV_FILE" || rc=$?
if [ "$rc" -eq 4 ]; then
  echo "grabpost.sh: nothing fresh for this hour yet (rc=4), ok"
  rc=0
elif [ "$rc" -ne 0 ]; then
  echo "error: presyo grabcard -auto failed (rc=$rc)"
fi
echo "=== grabpost.sh done rc=$rc $(ts) ==="
exit "$rc"
