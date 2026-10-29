#!/usr/bin/env bash
# Household-utilities job (UTILITIES_SPEC_V2.md section B3).
#
# Runs on the VPS as user presyo from cron, hourly at :20.
# Single pass, never loops:
#   1. decide fetch sections by Manila time,
#   2. `$BIN utilities fetch -sections …` (PDF cache under data/utilities),
#   3. `$BIN utilpages -snapshot … -site-public …`,
#   4. commit site/public/u + site/public/data/utilities.json (if changed),
#   5. `$BIN fbutil -auto` unless disabled.
#
# Layout (all overridable via env for tests):
#   PRESYO_HOME=$HOME  REPO=$PRESYO_HOME/repo
#   BIN=$PRESYO_HOME/bin/presyo  DB=$PRESYO_HOME/data/presyo.db
#   LOG=$PRESYO_HOME/logs/utilities.log  LOCK=$PRESYO_HOME/.utilities.lock
#
# Time overrides for tests: UTIL_DOW (1-7, Mon-Sun), UTIL_DOM (01-31),
# UTIL_HOUR (00-23), defaulting to Asia/Manila wall clock.
#
# Never uses sudo and never writes outside $PRESYO_HOME.
set -euo pipefail

PRESYO_HOME="${PRESYO_HOME:-$HOME}"
REPO="${REPO:-$PRESYO_HOME/repo}"
BIN="${BIN:-$PRESYO_HOME/bin/presyo}"
DB="${DB:-$PRESYO_HOME/data/presyo.db}"
LOG="${LOG:-$PRESYO_HOME/logs/utilities.log}"
LOCK="${LOCK:-$PRESYO_HOME/.utilities.lock}"
UDATA="${UDATA:-$PRESYO_HOME/data/utilities}"

# Safety: every path we touch must live under $PRESYO_HOME.
for p in "$REPO" "$BIN" "$DB" "$LOG" "$LOCK" "$UDATA"; do
  case "$p" in
    "$PRESYO_HOME"/*) ;;
    *)
      echo "error: path $p is outside PRESYO_HOME=$PRESYO_HOME" >&2
      exit 1
      ;;
  esac
done

mkdir -p "$PRESYO_HOME/logs" "$UDATA/cache" "$UDATA/stamps" "$(dirname "$LOCK")"

# Lock first so concurrent cron runs (and log rotation) cannot overlap.
exec 9>"$LOCK"
if ! flock -n 9; then
  echo "utilities.sh: already running, exiting"
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
echo "=== utilities.sh start $(ts) ==="

fail=0
fetch_rc="-"
utilpages_rc="-"
commit_rc="-"
push_rc="-"
fbutil_rc="-"

DOW="${UTIL_DOW:-$(TZ=Asia/Manila date +%u)}"   # 1=Mon .. 7=Sun
DOM="${UTIL_DOM:-$(TZ=Asia/Manila date +%d)}"   # 01-31
HOUR="${UTIL_HOUR:-$(TZ=Asia/Manila date +%H)}" # 00-23
TODAY="${UTIL_TODAY:-$(TZ=Asia/Manila date +%F)}"
DOW=$((10#$DOW)); DOM=$((10#$DOM)); HOUR=$((10#$HOUR))

# --- 1. decide sections ---
sections=""
stamp_is_today() { [ -f "$UDATA/stamps/$1" ] && [ "$(cat "$UDATA/stamps/$1")" = "$TODAY" ]; }
add_section() {
  if [ -z "$sections" ]; then sections="$1"; else sections="$sections,$1"; fi
}
# fuel+pump: hourly on Tue/Wed, else the first run after 09:00.
if [ "$DOW" -eq 2 ] || [ "$DOW" -eq 3 ]; then
  add_section fuel; add_section pump
  echo "$TODAY" >"$UDATA/stamps/fuel-pump"
elif [ "$HOUR" -ge 9 ] && ! stamp_is_today fuel-pump; then
  add_section fuel; add_section pump
  echo "$TODAY" >"$UDATA/stamps/fuel-pump"
fi
# meralco: daily on days 5-20.
if [ "$DOM" -ge 5 ] && [ "$DOM" -le 20 ] && ! stamp_is_today meralco; then
  add_section meralco
  echo "$TODAY" >"$UDATA/stamps/meralco"
fi
# lpg: daily on days 1-15.
if [ "$DOM" -ge 1 ] && [ "$DOM" -le 15 ] && ! stamp_is_today lpg; then
  add_section lpg
  echo "$TODAY" >"$UDATA/stamps/lpg"
fi
# water: every hour (the feed fetch happens inside water); fcda: hours
# divisible by 3 on days 10-20 only (still the website fetch).
# power: Manila hours divisible by 6 (06/12/18).
add_section water
if [ "$((HOUR % 3))" -eq 0 ]; then
  if [ "$DOM" -ge 10 ] && [ "$DOM" -le 20 ]; then
    add_section fcda
  fi
fi
if [ "$((HOUR % 6))" -eq 0 ]; then
  add_section power
fi
echo "utilities.sh: sections='$sections' (dow=$DOW dom=$DOM hour=$HOUR)"

SNAP="$UDATA/snapshot.json"
MAYNILAD_X_INBOX="${MAYNILAD_X_INBOX:-$PRESYO_HOME/inbox/maynilad_x.json}"

# --- 2. fetch (skipped when no section is due) ---
if [ -z "$sections" ]; then
  echo "utilities.sh: no sections due, skipping fetch/utilpages/commit"
else
  if "$BIN" utilities fetch -sections "$sections" -cache "$UDATA/cache" -out "$SNAP" -maynilad-x "$MAYNILAD_X_INBOX"; then
    fetch_rc=0
  else
    fetch_rc=$?
    echo "error: presyo utilities fetch failed (rc=$fetch_rc)"
    fail=1
  fi

  # --- 3. static utilities pages ---
  if [ -f "$SNAP" ]; then
    if "$BIN" utilpages -snapshot "$SNAP" -site-public "$REPO/site/public"; then
      utilpages_rc=0
    else
      utilpages_rc=$?
      echo "error: presyo utilpages failed (rc=$utilpages_rc)"
      fail=1
    fi
  fi

  # --- 4. commit site/public/u + site/public/data/utilities.json ---
  # Take the shared daily lock (flock -w 300) exactly like daily.sh.
  exec 10>"$PRESYO_HOME/.daily.lock"
  if ! flock -w 300 10; then
    echo "error: could not take the daily lock, skipping commit"
    fail=1
  else
    u_status="$(git -C "$REPO" status --porcelain -- site/public/u || true)"
    util_status="$(git -C "$REPO" status --porcelain -- site/public/data/utilities.json || true)"
    if [ -z "$u_status$util_status" ]; then
      echo "no utilities changes, nothing to commit"
    else
      git -C "$REPO" add site/public/u site/public/data/utilities.json
      if git -C "$REPO" diff --cached --quiet; then
        echo "nothing staged after add, nothing to commit"
      else
        stamp="$(TZ=Asia/Manila date '+%F %H:%M')"
        if git -C "$REPO" commit -m "utilities: $stamp"; then
          commit_rc=0
        else
          commit_rc=$?
          echo "error: git commit failed (rc=$commit_rc)"
          fail=1
        fi
        if [ "$commit_rc" -eq 0 ]; then
          # Pull first so the push does not fail on a race, retry once.
          if git -C "$REPO" pull --rebase && git -C "$REPO" push; then
            push_rc=0
          else
            echo "warning: pull/push failed, retrying once"
            if git -C "$REPO" pull --rebase && git -C "$REPO" push; then
              push_rc=0
            else
              push_rc=$?
              echo "error: git push failed after retry (rc=$push_rc)"
              fail=1
            fi
          fi
        fi
      fi
    fi
    flock -u 10
  fi
  exec 10>&-
fi

# --- 5. post due utilities (dry-run when asked) ---
if [ -f "$PRESYO_HOME/.config/presyo/fbpost.disabled" ]; then
  echo "utilities.sh: fbpost.disabled exists, skipping fbutil -auto"
else
  dry=""
  if [ "${PRESYO_UTIL_DRY_RUN:-0}" = "1" ]; then
    dry=" -dry-run"
  fi
  # shellcheck disable=SC2086
  if "$BIN" fbutil -db "$DB" -site-public "$REPO/site/public" -auto${dry}; then
    fbutil_rc=0
  else
    fbutil_rc=$?
    echo "error: presyo fbutil -auto failed (rc=$fbutil_rc)"
    fail=1
  fi
fi

echo "=== utilities.sh end $(ts) rc=$fail (fetch=$fetch_rc utilpages=$utilpages_rc commit=$commit_rc push=$push_rc fbutil=$fbutil_rc) ==="
exit "$fail"
