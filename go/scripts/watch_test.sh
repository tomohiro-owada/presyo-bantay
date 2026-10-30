#!/usr/bin/env bash
# Bats-free shell test for go/scripts/watch.sh.
#
# Builds a temp PRESYO_HOME with a fake BIN script and a fake daily.sh,
# then verifies (no network anywhere — the fake BIN never fetches):
#   1. newdata rc=10 -> exit 0, daily.sh not run, fbdigest -auto still runs.
#   2. newdata rc=0  -> daily.sh run once, then fbdigest -auto run once.
#   3. disabled file -> daily.sh runs, the DA post is skipped.
#   4. newdata rc=1  -> non-zero exit, daily.sh not run.
#   5. lock prevents overlap (exit 0 with a message, nothing runs).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WATCH="$SCRIPT_DIR/watch.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok: $1"; }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

export PRESYO_HOME="$TMP/home"
export BIN="$PRESYO_HOME/bin/presyo"
export DAILY="$PRESYO_HOME/bin/daily.sh"
export DB="$PRESYO_HOME/data/presyo.db"
export LOG="$PRESYO_HOME/logs/watch.log"
export LOCK="$PRESYO_HOME/.watch.lock"
export REPO="$PRESYO_HOME/repo"
export PRESYO_PUBLISH_WAIT=0
SITE="-site-public $REPO/site/public"

mkdir -p "$PRESYO_HOME/bin" "$PRESYO_HOME/data" "$PRESYO_HOME/logs"
touch "$DB"

CALLS="$TMP/calls.log"

# --- fake presyo binary (no network) ---
cat >"$BIN" <<FAKE
#!/usr/bin/env bash
set -euo pipefail
cmd="\${1:-}"; shift || true
echo "bin: \$cmd \$*" >>"$CALLS"
case "\$cmd" in
  newdata) exit "\${FAKE_NEWDATA_RC:-10}" ;;
  fbpost) exit "\${FAKE_FBPOST_RC:-0}" ;;
  fbdigest) exit "\${FAKE_FBPOST_RC:-0}" ;;
  *) echo "fake presyo: unknown command \$cmd" >&2; exit 1 ;;
esac
FAKE
chmod +x "$BIN"

# --- fake daily.sh ---
cat >"$DAILY" <<FAKE
#!/usr/bin/env bash
set -euo pipefail
echo "daily run" >>"$CALLS"
exit "\${FAKE_DAILY_RC:-0}"
FAKE
chmod +x "$DAILY"

calls() { [ -f "$CALLS" ] && cat "$CALLS" || true; }
reset_calls() { rm -f "$CALLS"; }

# --- 1. no new data -> daily.sh skipped, fbpost -auto still runs ---
export FAKE_NEWDATA_RC=10
reset_calls
if ! "$WATCH"; then bad "watch.sh exited non-zero when no new data"; else ok "watch.sh exit 0 when no new data"; fi
[ "$(calls)" = "$(printf 'bin: newdata -db %s\nbin: fbdigest -db %s -auto %s' "$DB" "$DB" "$SITE")" ] \
  && ok "fbdigest -auto runs even with no new data, daily.sh skipped" || bad "wrong calls: $(calls)"
grep -q "watch.sh start" "$LOG" && grep -q "watch.sh end" "$LOG" \
  && ok "timestamped start/end lines in log" || bad "start/end lines missing in log"

# --- 2. new data -> daily.sh once, then fbpost -auto once ---
export FAKE_NEWDATA_RC=0
reset_calls
if ! "$WATCH"; then bad "watch.sh exited non-zero on new data"; else ok "watch.sh exit 0 on new data"; fi
[ "$(calls)" = "$(printf 'bin: newdata -db %s\ndaily run\nbin: fbdigest -db %s -auto %s' "$DB" "$DB" "$SITE")" ] \
  && ok "daily.sh once then fbdigest -auto once, in order" || bad "wrong call order: $(calls)"

# --- 3. disabled file -> daily.sh runs, fbpost skipped ---
export FAKE_NEWDATA_RC=0
mkdir -p "$PRESYO_HOME/.config/presyo"
touch "$PRESYO_HOME/.config/presyo/fbpost.disabled"
reset_calls
if ! "$WATCH"; then bad "watch.sh exited non-zero when disabled"; else ok "watch.sh exit 0 when disabled"; fi
calls | grep -q "daily run" && ok "daily.sh still runs when disabled" || bad "daily.sh missing: $(calls)"
calls | grep -q "fbdigest\|fbpost" && bad "DA post ran despite disabled file" || ok "DA post skipped when disabled"
rm -f "$PRESYO_HOME/.config/presyo/fbpost.disabled"

# --- 4. newdata error -> non-zero exit, daily.sh not run, fbpost still runs ---
export FAKE_NEWDATA_RC=1
reset_calls
if "$WATCH" >/dev/null 2>&1; then bad "watch.sh exited 0 despite newdata failure"; else ok "watch.sh exits non-zero when newdata fails"; fi
calls | grep -q "daily run" && bad "daily.sh ran after newdata failure" || ok "daily.sh not run after newdata failure"
calls | grep -q "fbdigest" && ok "fbdigest -auto still runs after newdata failure" || bad "fbdigest missing: $(calls)"

# --- 4b. fbpost failure -> non-zero exit ---
export FAKE_NEWDATA_RC=10 FAKE_FBPOST_RC=2
reset_calls
if "$WATCH" >/dev/null 2>&1; then bad "watch.sh exited 0 despite fbpost failure"; else ok "watch.sh exits non-zero when fbpost fails"; fi
unset FAKE_FBPOST_RC

# --- 4c. rollback switch and dry-run switch ---
export FAKE_NEWDATA_RC=10
reset_calls
PRESYO_DA_POST=region "$WATCH" >/dev/null 2>&1 || bad "region fallback exited non-zero"
[ "$(calls)" = "$(printf 'bin: newdata -db %s\nbin: fbpost -db %s -auto' "$DB" "$DB")" ] \
  && ok "PRESYO_DA_POST=region falls back to fbpost -auto" || bad "wrong fallback calls: $(calls)"
reset_calls
PRESYO_DIGEST_DRY_RUN=1 "$WATCH" >/dev/null 2>&1 || bad "dry-run digest exited non-zero"
[ "$(calls)" = "$(printf 'bin: newdata -db %s\nbin: fbdigest -db %s -auto %s -dry-run' "$DB" "$DB" "$SITE")" ] \
  && ok "PRESYO_DIGEST_DRY_RUN=1 passes -dry-run" || bad "wrong dry-run calls: $(calls)"

# --- 5. lock prevents overlap ---
export FAKE_NEWDATA_RC=0
reset_calls
flock -n "$LOCK" sleep 8 &
holder=$!
sleep 0.5 # let the holder take the lock
out="$(bash "$WATCH" 2>&1)"; rc=$?
[ "$rc" -eq 0 ] && ok "locked run exits 0" || bad "locked run exit code $rc (want 0)"
echo "$out" | grep -qi "already running" \
  && ok "locked run prints already-running message" || bad "locked run printed: $out"
[ -z "$(calls)" ] && ok "locked run invokes nothing" || bad "locked run invoked: $(calls)"
wait "$holder"

echo "---"
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
