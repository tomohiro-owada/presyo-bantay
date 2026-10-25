#!/usr/bin/env bash
# Weekly Facebook report job (WEEKLY_SPEC.md section 7).
#
# Runs on the VPS as user presyo from cron on Sundays ~18:00 Manila.
# Single pass, never loops: run `presyo fbweekly -auto` once (it enforces
# Sunday/window/reportable/state/gap/disabled itself) — unless
# $PRESYO_HOME/.config/presyo/fbpost.disabled exists.
#
# Layout (all overridable via env for tests):
#   PRESYO_HOME=$HOME  REPO=$PRESYO_HOME/repo
#   BIN=$PRESYO_HOME/bin/presyo  DB=$PRESYO_HOME/data/presyo.db
#   LOG=$PRESYO_HOME/logs/weekly.log  LOCK=$PRESYO_HOME/.weekly.lock
#
# Never uses sudo and never writes outside $PRESYO_HOME.
set -euo pipefail

PRESYO_HOME="${PRESYO_HOME:-$HOME}"
REPO="${REPO:-$PRESYO_HOME/repo}"
BIN="${BIN:-$PRESYO_HOME/bin/presyo}"
DB="${DB:-$PRESYO_HOME/data/presyo.db}"
LOG="${LOG:-$PRESYO_HOME/logs/weekly.log}"
LOCK="${LOCK:-$PRESYO_HOME/.weekly.lock}"

# Safety: every path we touch must live under $PRESYO_HOME.
for p in "$REPO" "$BIN" "$DB" "$LOG" "$LOCK"; do
  case "$p" in
    "$PRESYO_HOME"/*) ;;
    *)
      echo "error: path $p is outside PRESYO_HOME=$PRESYO_HOME" >&2
      exit 1
      ;;
  esac
done

mkdir -p "$PRESYO_HOME/logs" "$(dirname "$LOCK")"

# Lock first so concurrent cron runs (and log rotation) cannot overlap.
exec 9>"$LOCK"
if ! flock -n 9; then
  echo "weekly.sh: already running, exiting"
  exit 0
fi

# Simple log rotation: keep one old file.
if [ -f "$LOG" ]; then
  size="$(wc -c <"$LOG" 2>/dev/null || echo 0)"
  if [ "${size:-0}" -gt 5242880 ]; then
    mv -f "$LOG" "${LOG}.1"
  fi
fi

# shellcheck disable=SC2094
exec >>"$LOG" 2>&1

ts() { date '+%Y-%m-%d %H:%M:%S %Z'; }
echo "=== weekly.sh start $(ts) ==="

fail=0
fbweekly_rc="-"

if [ -f "$PRESYO_HOME/.config/presyo/fbpost.disabled" ]; then
  echo "weekly.sh: fbpost.disabled exists, skipping fbweekly -auto"
else
  if "$BIN" fbweekly -db "$DB" -auto -site-public "$REPO/site/public"; then
    fbweekly_rc=0
  else
    fbweekly_rc=$?
    echo "error: presyo fbweekly -auto failed (rc=$fbweekly_rc)"
    fail=1
  fi
fi

echo "=== weekly.sh end $(ts) rc=$fail (fbweekly=$fbweekly_rc) ==="
exit "$fail"
