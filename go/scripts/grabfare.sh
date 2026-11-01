#!/usr/bin/env bash
# Hourly Grab fare data job (GRABFARE_SPEC.md / GRABFARE_SITE_SPEC.md).
#
# Runs on the ingest host (non-root) from cron at :10. Single pass:
#   1. `presyo grabfare` fetches the fare API and writes grab-fare.json
#      (+ snippet/preview) into $OUT. A failed fetch exits non-zero but the
#      command itself rewrites an EXPIRED card as hidden, so an old fare
#      never looks current.
#   2. When PRESYO_GRABFARE_PUBLISH=1 (default 0 = local files only), the
#      small grab-fare.json is published to a dedicated data branch
#      ($DATA_BRANCH, default grab-data) of the repo: ONE file, ONE commit that
#      is amended and force-pushed, so the branch never grows and the GitHub
#      Pages workflow (which only runs on pushes to main) is NOT triggered.
#      The site reads the file at runtime from the raw branch URL.
#      The push is skipped when only generated_at changed.
#
# Layout (all overridable via env for tests):
#   PRESYO_HOME=$HOME  BIN=$PRESYO_HOME/bin/presyo
#   ENV_FILE=$PRESYO_HOME/.config/presyo/grab.env  OUT=$PRESYO_HOME/state/grab-fare
#   REPO=$PRESYO_HOME/repo (origin remote is reused)  DATA_REPO=$PRESYO_HOME/grab-data
#   LOG=$PRESYO_HOME/logs/grabfare.log  LOCK=$PRESYO_HOME/.grabfare.lock
#
# Never uses sudo and never writes outside $PRESYO_HOME.
set -euo pipefail

PRESYO_HOME="${PRESYO_HOME:-$HOME}"
BIN="${BIN:-$PRESYO_HOME/bin/presyo}"
ENV_FILE="${ENV_FILE:-$PRESYO_HOME/.config/presyo/grab.env}"
OUT="${OUT:-$PRESYO_HOME/state/grab-fare}"
REPO="${REPO:-$PRESYO_HOME/repo}"
DATA_REPO="${DATA_REPO:-$PRESYO_HOME/grab-data}"
DATA_BRANCH="${DATA_BRANCH:-grab-data}"
LOG="${LOG:-$PRESYO_HOME/logs/grabfare.log}"
LOCK="${LOCK:-$PRESYO_HOME/.grabfare.lock}"
PUBLISH="${PRESYO_GRABFARE_PUBLISH:-0}"

for p in "$BIN" "$ENV_FILE" "$OUT" "$REPO" "$DATA_REPO" "$LOG" "$LOCK"; do
  case "$p" in
    "$PRESYO_HOME"/*) ;;
    *)
      echo "error: path $p is outside PRESYO_HOME=$PRESYO_HOME" >&2
      exit 1
      ;;
  esac
done

mkdir -p "$PRESYO_HOME/logs" "$OUT" "$(dirname "$LOCK")"

exec 9>"$LOCK"
if ! flock -n 9; then
  echo "grabfare.sh: already running, exiting"
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
echo "=== grabfare.sh start $(ts) ==="

# PRESYO_GRABFARE_PUBLISH (and the API settings) live in the env file; the
# secrets in it are never echoed.
if [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
fi
PUBLISH="${PRESYO_GRABFARE_PUBLISH:-0}"

rc=0
"$BIN" grabfare -env-file "$ENV_FILE" -out-dir "$OUT" || rc=$?
if [ "$rc" -ne 0 ]; then
  echo "error: presyo grabfare failed (rc=$rc)"
fi

# strip the volatile generated_at line for comparisons
norm() { grep -v '"generated_at"' "$1" 2>/dev/null || true; }

if [ "$PUBLISH" = "1" ] && [ -f "$OUT/grab-fare.json" ]; then
  if [ ! -d "$DATA_REPO/.git" ]; then
    url="$(git -C "$REPO" remote get-url origin)"
    if git ls-remote --exit-code --heads "$url" "$DATA_BRANCH" >/dev/null 2>&1; then
      git clone -q --single-branch --branch "$DATA_BRANCH" "$url" "$DATA_REPO"
    else
      git init -q "$DATA_REPO"
      git -C "$DATA_REPO" checkout -q --orphan "$DATA_BRANCH"
      git -C "$DATA_REPO" remote add origin "$url"
    fi
    git -C "$DATA_REPO" config user.name "presyo-bot"
    git -C "$DATA_REPO" config user.email "presyo-bot@users.noreply.github.com"
  fi
  if [ "$(norm "$OUT/grab-fare.json")" = "$(norm "$DATA_REPO/grab-fare.json")" ] && [ -f "$DATA_REPO/grab-fare.json" ]; then
    echo "publish: unchanged (only generated_at differs), skipped"
  else
    cp "$OUT/grab-fare.json" "$DATA_REPO/grab-fare.json"
    git -C "$DATA_REPO" add grab-fare.json
    if git -C "$DATA_REPO" rev-parse -q --verify HEAD >/dev/null; then
      git -C "$DATA_REPO" commit -q --amend -m "grab-fare data" --date "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    else
      git -C "$DATA_REPO" commit -q -m "grab-fare data"
    fi
    if git -C "$DATA_REPO" push -q --force origin "$DATA_BRANCH"; then
      echo "publish: pushed grab-fare.json to $DATA_BRANCH"
    else
      echo "error: publish push failed"
      rc=1
    fi
  fi
fi

echo "=== grabfare.sh done rc=$rc $(ts) ==="
exit "$rc"
