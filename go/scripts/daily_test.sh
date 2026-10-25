#!/usr/bin/env bash
# Bats-free shell test for go/scripts/daily.sh.
#
# Builds a temp PRESYO_HOME with a fake BIN script and a local bare git
# repo as origin, then verifies:
#   1. commit+push when data changes,
#   2. no commit when only generated_at changed,
#   3. lock prevents overlap (exit 0 with a message, no commit).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DAILY="$SCRIPT_DIR/daily.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok: $1"; }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

export PRESYO_HOME="$TMP/home"
export REPO="$PRESYO_HOME/repo"
export DB="$PRESYO_HOME/data/presyo.db"
export RAW="$PRESYO_HOME/data/raw"
export BIN="$PRESYO_HOME/bin/presyo"
export LOG="$PRESYO_HOME/logs/daily.log"
export LOCK="$PRESYO_HOME/.daily.lock"

mkdir -p "$PRESYO_HOME/bin" "$PRESYO_HOME/data" "$PRESYO_HOME/logs"

# --- fake presyo binary ---
cat >"$BIN" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
cmd="${1:-}"; shift || true
JSON_DIR=""; OUT=""; SIGNALS=""; OUTDIR=""; ADVICE_OUT=""; SITE=""
while [ $# -gt 0 ]; do
  case "$1" in
    -json-dir) JSON_DIR="$2"; shift 2 ;;
    -out) OUT="$2"; shift 2 ;;
    -signals) SIGNALS="$2"; shift 2 ;;
    -out-dir) OUTDIR="$2"; shift 2 ;;
    -site-public) SITE="$2"; shift 2 ;;
    -db|-raw) shift 2 ;;
    -no-llm) shift ;;
    *) shift ;;
  esac
done
case "$cmd" in
  ingest)
    if [ "${FAKE_MODE:-change}" = "change" ]; then
      mkdir -p "$JSON_DIR"
      echo "{\"date\":\"2026-10-24\",\"markets\":{},\"n\":$RANDOM}" >"$JSON_DIR/2026-10-24.json"
    fi
    ;;
  fx) exit "${FAKE_FX_RC:-0}" ;;
  export)
    mkdir -p "$OUT"
    if [ "${FAKE_MODE:-change}" = "change" ]; then
      printf '{"generated_at":"%s","v":2}' "$(date -u +%FT%TZ)" >"$OUT/meta.json"
    else
      # bump only generated_at, keep everything else identical
      sed -E 's/"generated_at"[[:space:]]*:[[:space:]]*"[^"]*"/"generated_at":"'"$(date -u +%FT%TZ)"'"/' \
        "$OUT/meta.json" >"$OUT/meta.json.new"
      mv "$OUT/meta.json.new" "$OUT/meta.json"
    fi
    ;;
  signals)
    if [ "${FAKE_DEALS_FAIL:-0}" = "1" ]; then echo "fake signals fail" >&2; exit 3; fi
    mkdir -p "$(dirname "$OUT")"
    echo '{"as_of":"2026-10-24","deals":[],"pricier":[],"uses_nearby_basis":false}' >"$OUT"
    ;;
  advise)
    if [ "${FAKE_DEALS_FAIL:-0}" = "1" ]; then echo "fake advise fail" >&2; exit 3; fi
    mkdir -p "$OUTDIR"
    echo '{"text_source":"template"}' >"$OUTDIR/run.json"
    echo '{"status":"pass"}' >"$OUTDIR/check.json"
    echo '{"as_of":"2026-10-24"}' >"$OUTDIR/deals.json"
    ;;
  daypages)
    echo "daypages called" >>"${FAKE_CALLS:-/dev/null}"
    if [ "${FAKE_DAYPAGES_FAIL:-0}" = "1" ]; then echo "fake daypages fail" >&2; exit 4; fi
    mkdir -p "$SITE/d/2026-10-24"
    echo '{"date":"2026-10-24"}' >"$SITE/d/2026-10-24/day.json"
    ;;
  *) echo "fake presyo: unknown command $cmd" >&2; exit 1 ;;
esac
FAKE
chmod +x "$BIN"

# --- origin (bare) + repo seed ---
git init --bare -q "$TMP/origin.git"
git init -q -b main "$REPO"
git -C "$REPO" config user.name "test"
git -C "$REPO" config user.email "test@example.com"
mkdir -p "$REPO/data/json" "$REPO/site/public/data"
echo '{"date":"2026-10-23","markets":{}}' >"$REPO/data/json/2026-10-23.json"
printf '{"generated_at":"2026-10-23T00:00:00Z","v":1}' >"$REPO/site/public/data/meta.json"
git -C "$REPO" add data/json site/public/data
git -C "$REPO" commit -qm "seed"
git -C "$REPO" remote add origin "$TMP/origin.git"
git -C "$REPO" push -qu origin main
git -C "$REPO" branch --set-upstream-to=origin/main main 2>/dev/null || true

rev() { git -C "$REPO" rev-parse HEAD; }
origin_rev() { git --git-dir="$TMP/origin.git" rev-parse refs/heads/main; }

# --- 1. data changes -> commit + push ---
export FAKE_MODE=change
before="$(rev)"
if ! "$DAILY"; then bad "daily.sh exited non-zero on data change"; else ok "daily.sh exit 0 on data change"; fi
after="$(rev)"
[ "$before" != "$after" ] && ok "new commit created on data change" || bad "no commit on data change"
[ "$(origin_rev)" = "$after" ] && ok "commit pushed to origin" || bad "commit not pushed to origin"
git -C "$REPO" log -1 --format=%s | grep -qE '^data: [0-9]{4}-[0-9]{2}-[0-9]{2}$' \
  && ok "commit message is 'data: YYYY-MM-DD'" || bad "commit message wrong: $(git -C "$REPO" log -1 --format=%s)"
[ -f "$REPO/data/json/2026-10-24.json" ] && ok "json snapshot present" || bad "json snapshot missing"
git -C "$REPO" status --porcelain -- data/presyo.db | grep -q . \
  && bad "data/presyo.db staged (must never be added)" || ok "data/presyo.db never added"
grep -q "daily.sh start" "$LOG" && grep -q "daily.sh end" "$LOG" \
  && ok "timestamped start/end lines in log" || bad "start/end lines missing in log"

# --- 2. only generated_at changes -> no commit, meta.json restored ---
export FAKE_MODE=nochange
before2="$(rev)"
if ! "$DAILY"; then bad "daily.sh exited non-zero on no-change run"; else ok "daily.sh exit 0 on no-change run"; fi
after2="$(rev)"
[ "$before2" = "$after2" ] && ok "no commit when only generated_at changed" || bad "unexpected commit on no-change run"
[ -z "$(git -C "$REPO" status --porcelain -- data/json site/public/data)" ] \
  && ok "worktree clean after no-change run" || bad "worktree dirty after no-change run: $(git -C "$REPO" status --porcelain -- data/json site/public/data)"

# --- 3. lock prevents overlap ---
# Hold the lock with a background sleeper; wait for it to exit naturally
# afterwards so the lock is really released (killing flock would orphan
# sleep, which inherits the lock fd and keeps holding it).
export FAKE_MODE=change
before3="$(rev)"
flock -n "$LOCK" sleep 8 &
holder=$!
sleep 0.5 # let the holder take the lock
out="$(bash "$DAILY" 2>&1)"; rc=$?
[ "$rc" -eq 0 ] && ok "locked run exits 0" || bad "locked run exit code $rc (want 0)"
echo "$out" | grep -qi "already running" \
  && ok "locked run prints already-running message" || bad "locked run printed: $out"
[ "$(rev)" = "$before3" ] && ok "locked run creates no commit" || bad "locked run created a commit"
wait "$holder"

# --- 4. failing export still exits non-zero but log has end line ---
export FAKE_MODE=change FAKE_FX_RC=3
if bash "$DAILY" >/dev/null 2>&1; then bad "daily.sh exited 0 despite fx failure"; else ok "daily.sh exits non-zero when a step fails"; fi
unset FAKE_FX_RC

# --- 5. PRESYO_DEALS=1 writes deals.json and includes it in the commit ---
export FAKE_MODE=change PRESYO_DEALS=1
rm -f "$REPO/site/public/data/deals.json"
before5="$(rev)"
if ! "$DAILY"; then bad "daily.sh with PRESYO_DEALS=1 exited non-zero"; else ok "daily.sh exit 0 with PRESYO_DEALS=1"; fi
[ -f "$REPO/site/public/data/deals.json" ] && ok "deals.json staged for commit" || bad "deals.json missing after PRESYO_DEALS=1 run"
[ "$(rev)" != "$before5" ] && ok "new commit created with deals.json" || bad "no commit with PRESYO_DEALS=1"
grep -q "deals=" "$LOG" && ok "deals rc recorded in log" || bad "deals rc missing in log"
unset PRESYO_DEALS

# --- 6. deals failure must not fail the daily job ---
export FAKE_MODE=change PRESYO_DEALS=1 FAKE_DEALS_FAIL=1
if ! "$DAILY"; then bad "daily.sh exited non-zero despite deals-only failure"; else ok "daily.sh exit 0 when only deals step fails"; fi
grep -q "deals step failed" "$LOG" && ok "deals failure logged" || bad "deals failure not logged"
unset PRESYO_DEALS FAKE_DEALS_FAIL

# --- 7. daypages runs by default: called, rc recorded, d/ files committed ---
export FAKE_MODE=change FAKE_CALLS="$TMP/calls.log"
rm -f "$FAKE_CALLS"
before7="$(rev)"
if ! "$DAILY"; then bad "daily.sh exited non-zero on daypages run"; else ok "daily.sh exit 0 with daypages"; fi
grep -q "daypages called" "$FAKE_CALLS" && ok "daypages called by default" || bad "daypages not called"
[ -f "$REPO/site/public/d/2026-10-24/day.json" ] && ok "d/ day.json present" || bad "d/ day.json missing"
[ "$(rev)" != "$before7" ] && ok "new commit created" || bad "no commit"
git -C "$REPO" ls-files --error-unmatch "site/public/d/2026-10-24/day.json" >/dev/null \
  && ok "d/ files committed" || bad "d/ files not tracked in git"
[ -n "$(git -C "$REPO" log --format=%H -- site/public/d/2026-10-24/day.json)" ] \
  && ok "d/ files have commit history" || bad "d/ files have no commit"
[ "$(origin_rev)" = "$(rev)" ] && ok "commit with d/ pushed to origin" || bad "commit not pushed"
grep -q "daypages=0" "$LOG" && ok "daypages rc recorded in log" || bad "daypages rc missing in log"
unset FAKE_CALLS

# --- 8. PRESYO_DAYPAGES=0 skips daypages ---
export FAKE_MODE=change PRESYO_DAYPAGES=0 FAKE_CALLS="$TMP/calls.log"
rm -f "$FAKE_CALLS"
if ! "$DAILY"; then bad "daily.sh exited non-zero with PRESYO_DAYPAGES=0"; else ok "daily.sh exit 0 with PRESYO_DAYPAGES=0"; fi
[ ! -f "$FAKE_CALLS" ] && ok "daypages skipped with PRESYO_DAYPAGES=0" || bad "daypages called despite PRESYO_DAYPAGES=0"
grep -q "daypages skipped" "$LOG" && ok "daypages skip logged" || bad "daypages skip not logged"
unset PRESYO_DAYPAGES FAKE_CALLS

# --- 9. daypages failure fails the run but still commits other data ---
export FAKE_MODE=change FAKE_DAYPAGES_FAIL=1
before9="$(rev)"
if bash "$DAILY" >/dev/null 2>&1; then bad "daily.sh exited 0 despite daypages failure"; else ok "daily.sh exits non-zero when daypages fails"; fi
[ "$(rev)" != "$before9" ] && ok "other data still committed despite daypages failure" || bad "commit blocked by daypages failure"
[ "$(origin_rev)" = "$(rev)" ] && ok "commit pushed despite daypages failure" || bad "push blocked by daypages failure"
grep -q "presyo daypages failed" "$LOG" && ok "daypages failure logged" || bad "daypages failure not logged"
unset FAKE_DAYPAGES_FAIL

echo "---"
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
