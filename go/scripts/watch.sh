#!/usr/bin/env bash
# Watch job for regional Facebook posts (REGIONS_SPEC.md section 9).
#
# Runs on the ingest host (non-root) from cron. Single pass, never loops:
# ask `presyo newdata` whether the DA listing has a report date newer than
# the DB; if so, run daily.sh once (it has its own lock; the 06:15/22:15
# daily runs stay as a safety net). Then run `presyo fbpost -auto` on EVERY
# run — new data or not, even if newdata errored (posting from the existing
# DB is still fine) — unless $PRESYO_HOME/.config/presyo/fbpost.disabled
# exists. fbpost -auto itself enforces window/gap/order/staleness.
#
# Layout (all overridable via env for tests):
#   PRESYO_HOME=$HOME  BIN=$PRESYO_HOME/bin/presyo
#   DAILY=$PRESYO_HOME/bin/daily.sh  DB=$PRESYO_HOME/data/presyo.db
#   LOG=$PRESYO_HOME/logs/watch.log  LOCK=$PRESYO_HOME/.watch.lock
#
# Never uses sudo and never writes outside $PRESYO_HOME.
set -euo pipefail

PRESYO_HOME="${PRESYO_HOME:-$HOME}"
BIN="${BIN:-$PRESYO_HOME/bin/presyo}"
DAILY="${DAILY:-$PRESYO_HOME/bin/daily.sh}"
DB="${DB:-$PRESYO_HOME/data/presyo.db}"
LOG="${LOG:-$PRESYO_HOME/logs/watch.log}"
LOCK="${LOCK:-$PRESYO_HOME/.watch.lock}"

# Safety: every path we touch must live under $PRESYO_HOME.
for p in "$BIN" "$DAILY" "$DB" "$LOG" "$LOCK"; do
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
  echo "watch.sh: already running, exiting"
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
echo "=== watch.sh start $(ts) ==="

newdata_rc=0
if "$BIN" newdata -db "$DB"; then
  newdata_rc=0
else
  newdata_rc=$?
fi

fail=0
daily_rc="-"
fbpost_rc="-"

if [ "$newdata_rc" -eq 0 ]; then
  echo "watch.sh: new data found, running daily.sh once"
  if "$DAILY"; then
    daily_rc=0
  else
    daily_rc=$?
    echo "error: daily.sh failed (rc=$daily_rc)"
    fail=1
  fi
elif [ "$newdata_rc" -eq 10 ]; then
  echo "watch.sh: no new data, skipping daily.sh"
else
  echo "error: presyo newdata failed (rc=$newdata_rc)"
  fail=1
fi

# Post at most one region on every run (fbpost -auto decides; it enforces
# window/gap/order/staleness itself), unless disabled.
if [ -f "$PRESYO_HOME/.config/presyo/fbpost.disabled" ]; then
  echo "watch.sh: fbpost.disabled exists, skipping fbpost -auto"
else
  if "$BIN" fbpost -db "$DB" -auto; then
    fbpost_rc=0
  else
    fbpost_rc=$?
    echo "error: presyo fbpost -auto failed (rc=$fbpost_rc)"
    fail=1
  fi
fi

echo "=== watch.sh end $(ts) rc=$fail (newdata=$newdata_rc daily=$daily_rc fbpost=$fbpost_rc) ==="
exit "$fail"
