#!/usr/bin/env bash
# Bats-free shell test for go/scripts/weekly.sh.
#
# Builds a temp PRESYO_HOME with a fake BIN script, then verifies:
#   1. fbweekly -auto runs with the frozen site-public path.
#   2. disabled file -> fbweekly skipped, exit 0.
#   3. fbweekly failure -> non-zero exit.
#   4. lock prevents overlap (exit 0 with a message, nothing runs).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WEEKLY="$SCRIPT_DIR/weekly.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok: $1"; }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

export PRESYO_HOME="$TMP/home"
export REPO="$PRESYO_HOME/repo"
export BIN="$PRESYO_HOME/bin/presyo"
export DB="$PRESYO_HOME/data/presyo.db"
export LOG="$PRESYO_HOME/logs/weekly.log"
export LOCK="$PRESYO_HOME/.weekly.lock"

mkdir -p "$PRESYO_HOME/bin" "$PRESYO_HOME/data" "$PRESYO_HOME/logs" "$REPO/site/public"
touch "$DB"

CALLS="$TMP/calls.log"

# --- fake presyo binary (no network) ---
cat >"$BIN" <<FAKE
#!/usr/bin/env bash
set -euo pipefail
echo "bin: \$*" >>"$CALLS"
exit "\${FAKE_FBWEEKLY_RC:-0}"
FAKE
chmod +x "$BIN"

calls() { [ -f "$CALLS" ] && cat "$CALLS" || true; }
reset_calls() { rm -f "$CALLS"; }

# --- 1. fbweekly -auto runs with the frozen site-public path ---
reset_calls
if ! "$WEEKLY"; then bad "weekly.sh exited non-zero"; else ok "weekly.sh exit 0"; fi
[ "$(calls)" = "bin: fbweekly -db $DB -auto -site-public $REPO/site/public" ] \
  && ok "fbweekly -auto runs with site-public" || bad "wrong calls: $(calls)"
grep -q "weekly.sh start" "$LOG" && grep -q "weekly.sh end" "$LOG" \
  && ok "timestamped start/end lines in log" || bad "start/end lines missing in log"
grep -q "fbweekly=0" "$LOG" && ok "fbweekly rc recorded in log" || bad "fbweekly rc missing in log"

# --- 2. disabled file -> skipped ---
mkdir -p "$PRESYO_HOME/.config/presyo"
touch "$PRESYO_HOME/.config/presyo/fbpost.disabled"
reset_calls
if ! "$WEEKLY"; then bad "weekly.sh exited non-zero when disabled"; else ok "weekly.sh exit 0 when disabled"; fi
[ -z "$(calls)" ] && ok "fbweekly skipped when disabled" || bad "fbweekly ran: $(calls)"
grep -q "fbpost.disabled exists" "$LOG" && ok "disabled skip logged" || bad "disabled skip not logged"
rm -f "$PRESYO_HOME/.config/presyo/fbpost.disabled"

# --- 3. fbweekly failure -> non-zero exit ---
export FAKE_FBWEEKLY_RC=2
reset_calls
if "$WEEKLY" >/dev/null 2>&1; then bad "weekly.sh exited 0 despite fbweekly failure"; else ok "weekly.sh exits non-zero when fbweekly fails"; fi
unset FAKE_FBWEEKLY_RC

# --- 4. lock prevents overlap ---
reset_calls
flock -n "$LOCK" sleep 8 &
holder=$!
sleep 0.5 # let the holder take the lock
out="$(bash "$WEEKLY" 2>&1)"; rc=$?
[ "$rc" -eq 0 ] && ok "locked run exits 0" || bad "locked run exit code $rc (want 0)"
echo "$out" | grep -qi "already running" \
  && ok "locked run prints already-running message" || bad "locked run printed: $out"
[ -z "$(calls)" ] && ok "locked run invokes nothing" || bad "locked run invoked: $(calls)"
wait "$holder"

echo "---"
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
