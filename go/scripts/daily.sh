#!/usr/bin/env bash
# Daily ingest/export/push job for the ingest host.
#
# Runs on the ingest host (non-root) from cron. The ingest host is the ONLY writer of
# data: ingest PDFs -> SQLite -> export site JSON -> commit + push.
#
# Layout (all overridable via env for tests):
#   PRESYO_HOME=$HOME  REPO=$PRESYO_HOME/repo  DB=$PRESYO_HOME/data/presyo.db
#   RAW=$PRESYO_HOME/data/raw  BIN=$PRESYO_HOME/bin/presyo  LOG=$PRESYO_HOME/logs/daily.log
#
# Flow: rotate log, take flock, git pull --rebase, run ingest/fx/export
# (recording each exit code without aborting), commit+push data/json,
# site/public/data and site/public/d (skipping the commit when only
# meta.json generated_at changed), exit non-zero if anything failed.
#
# Never uses sudo and never writes outside $PRESYO_HOME.
set -euo pipefail

PRESYO_HOME="${PRESYO_HOME:-$HOME}"
REPO="${REPO:-$PRESYO_HOME/repo}"
DB="${DB:-$PRESYO_HOME/data/presyo.db}"
RAW="${RAW:-$PRESYO_HOME/data/raw}"
BIN="${BIN:-$PRESYO_HOME/bin/presyo}"
LOG="${LOG:-$PRESYO_HOME/logs/daily.log}"
LOCK="${LOCK:-$PRESYO_HOME/.daily.lock}"

# Safety: every path we touch must live under $PRESYO_HOME.
for p in "$REPO" "$DB" "$RAW" "$BIN" "$LOG" "$LOCK"; do
  case "$p" in
    "$PRESYO_HOME"/*) ;;
    *)
      echo "error: path $p is outside PRESYO_HOME=$PRESYO_HOME" >&2
      exit 1
      ;;
  esac
done

mkdir -p "$PRESYO_HOME/logs" "$PRESYO_HOME/data" "$(dirname "$LOCK")"

# Lock first so concurrent cron runs (and log rotation) cannot overlap.
exec 9>"$LOCK"
if ! flock -n 9; then
  echo "daily.sh: already running, exiting"
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
echo "=== daily.sh start $(ts) ==="

fail=0

# Current snapshot dir first; a failure here must not stop the run.
if ! git -C "$REPO" pull --rebase --quiet; then
  echo "warning: initial git pull --rebase failed, continuing"
fi

mkdir -p "$RAW" "$(dirname "$DB")" "$REPO/data/json" "$REPO/site/public/data" "$REPO/site/public/d"

ingest_rc=0
fx_rc=0
export_rc=0
daypages_rc=0
weekpages_rc=0

if "$BIN" ingest -db "$DB" -raw "$RAW" -json-dir "$REPO/data/json"; then
  ingest_rc=0
else
  ingest_rc=$?
  echo "error: presyo ingest failed (rc=$ingest_rc)"
fi

if "$BIN" fx -db "$DB"; then
  fx_rc=0
else
  fx_rc=$?
  echo "error: presyo fx failed (rc=$fx_rc)"
fi

if "$BIN" export -db "$DB" -out "$REPO/site/public/data"; then
  export_rc=0
else
  export_rc=$?
  echo "error: presyo export failed (rc=$export_rc)"
fi

# Optional deals card step (v3, off by default). Builds signals + advise
# (template fallback unless PRESYO_DEALS_LLM=1) and stages deals.json for
# the commit below. Never fails the ingest/export result: log + record rc.
deals_rc=0
if [ "${PRESYO_DEALS:-0}" = "1" ]; then
  deals_tmp="$(mktemp -d)"
  deals_date="$(TZ=Asia/Manila date +%F)"
  deals_out="$PRESYO_HOME/advice/$deals_date"
  advise_extra=""
  if [ "${PRESYO_DEALS_LLM:-0}" != "1" ]; then
    advise_extra="-no-llm"
  fi
  if "$BIN" signals -db "$DB" -out "$deals_tmp/signals.json" && \
     "$BIN" advise -signals "$deals_tmp/signals.json" -out-dir "$deals_out" $advise_extra; then
    if [ -f "$deals_out/deals.json" ]; then
      cp "$deals_out/deals.json" "$REPO/site/public/data/deals.json"
    else
      deals_rc=1
      echo "error: presyo deals step produced no deals.json (rc=$deals_rc)"
    fi
  else
    deals_rc=$?
    echo "error: presyo deals step failed (rc=$deals_rc)"
  fi
  rm -rf "$deals_tmp"
fi

[ "$ingest_rc" -ne 0 ] && fail=1
[ "$fx_rc" -ne 0 ] && fail=1
[ "$export_rc" -ne 0 ] && fail=1

# Static daily report pages (daypages, on by default unless
# PRESYO_DAYPAGES=0). A failure is logged and fails the run but must not
# block the commit/push of the other data below.
if [ "${PRESYO_DAYPAGES:-1}" = "0" ]; then
  echo "daypages skipped (PRESYO_DAYPAGES=0)"
else
  if "$BIN" daypages -db "$DB" -site-public "$REPO/site/public"; then
    daypages_rc=0
  else
    daypages_rc=$?
    echo "error: presyo daypages failed (rc=$daypages_rc)"
    fail=1
  fi
fi

# Static Sunday weekly report pages (weekpages, on by default unless
# PRESYO_WEEKPAGES=0). A failure is logged and fails the run but must not
# block the commit/push of the other data below.
if [ "${PRESYO_WEEKPAGES:-1}" = "0" ]; then
  echo "weekpages skipped (PRESYO_WEEKPAGES=0)"
else
  if "$BIN" weekpages -db "$DB" -site-public "$REPO/site/public"; then
    weekpages_rc=0
  else
    weekpages_rc=$?
    echo "error: presyo weekpages failed (rc=$weekpages_rc)"
    fail=1
  fi
fi

# --- change detection + commit + push (only these dirs, never the DB) ---
commit_rc=0
push_rc=0

json_status="$(git -C "$REPO" status --porcelain -- data/json || true)"
site_status="$(git -C "$REPO" status --porcelain -- site/public/data || true)"
days_status="$(git -C "$REPO" status --porcelain -- site/public/d || true)"
weeks_status="$(git -C "$REPO" status --porcelain -- site/public/w || true)"
# Utilities pages ride along only when they exist (written by utilities.sh).
util_status="$(git -C "$REPO" status --porcelain -- site/public/u site/public/data/utilities.json || true)"
util_paths=""
if [ -e "$REPO/site/public/u" ]; then
  util_paths="$util_paths site/public/u"
fi
if [ -e "$REPO/site/public/data/utilities.json" ]; then
  util_paths="$util_paths site/public/data/utilities.json"
fi

only_generated_at=0
  if [ -z "$json_status" ] && [ -z "$days_status" ] && [ -z "$weeks_status" ] && [ -z "$util_status" ] && [ -n "$site_status" ]; then
  # Exactly one modified tracked meta.json and nothing else?
  if [ "$site_status" = " M site/public/data/meta.json" ]; then
    old_tmp="$(mktemp)"
    new_tmp="$(mktemp)"
    if git -C "$REPO" show "HEAD:site/public/data/meta.json" >"$old_tmp" 2>/dev/null; then
      norm="s/\"generated_at\"[[:space:]]*:[[:space:]]*\"[^\"]*\"/\"generated_at\":\"\"/g"
      if [ "$(sed -E "$norm" "$old_tmp")" = "$(sed -E "$norm" "$REPO/site/public/data/meta.json")" ]; then
        only_generated_at=1
      fi
    fi
    rm -f "$old_tmp" "$new_tmp"
  fi
fi

  if [ -z "$json_status$site_status$days_status$weeks_status$util_status" ]; then
    echo "no data changes, nothing to commit"
  elif [ "$only_generated_at" = "1" ]; then
  git -C "$REPO" checkout -- site/public/data/meta.json
  echo "only generated_at changed, restored meta.json, nothing to commit"
else
    # shellcheck disable=SC2086
    git -C "$REPO" add data/json site/public/data site/public/d site/public/w $util_paths
  if git -C "$REPO" diff --cached --quiet; then
    echo "nothing staged after add, nothing to commit"
  else
    today="$(TZ=Asia/Manila date +%F)"
    if git -C "$REPO" commit -m "data: $today"; then
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

echo "=== daily.sh end $(ts) rc=$fail (ingest=$ingest_rc fx=$fx_rc export=$export_rc deals=$deals_rc daypages=$daypages_rc weekpages=$weekpages_rc commit=$commit_rc push=$push_rc) ==="
exit "$fail"
