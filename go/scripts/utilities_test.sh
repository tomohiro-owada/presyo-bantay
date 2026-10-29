#!/usr/bin/env bash
# Bats-free shell test for go/scripts/utilities.sh.
#
# Builds a temp PRESYO_HOME with a fake BIN script and a local bare git
# repo as origin (no network), then verifies section scheduling, the
# fetch/utilpages/commit flow, fbutil invocation, the disabled file,
# the daily lock, and PRESYO_UTIL_DRY_RUN=1.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
UTIL="$SCRIPT_DIR/utilities.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok: $1"; }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

export PRESYO_HOME="$TMP/home"
export REPO="$PRESYO_HOME/repo"
export DB="$PRESYO_HOME/data/presyo.db"
export BIN="$PRESYO_HOME/bin/presyo"
export LOG="$PRESYO_HOME/logs/utilities.log"
export LOCK="$PRESYO_HOME/.utilities.lock"
export UDATA="$PRESYO_HOME/data/utilities"

mkdir -p "$PRESYO_HOME/bin" "$PRESYO_HOME/data" "$PRESYO_HOME/logs"
touch "$DB"

CALLS="$TMP/calls.log"

# --- fake presyo binary (no network) ---
cat >"$BIN" <<FAKE
#!/usr/bin/env bash
set -euo pipefail
echo "bin: \$*" >>"$CALLS"
cmd="\${1:-}"; sub="\${2:-}"; shift 2 || true
if [ "\$cmd" = "utilities" ] && [ "\$sub" = "fetch" ]; then
  out=""; cache=""
  while [ \$# -gt 0 ]; do
    case "\$1" in
      -out) out="\$2"; shift 2 ;;
      -cache) cache="\$2"; shift 2 ;;
      *) shift ;;
    esac
  done
  mkdir -p "\$(dirname "\$out")" "\$cache"
  echo '{"fetched_at":"2026-10-27T06:20:00+08:00"}' >"\$out"
  exit 0
fi
if [ "\$cmd" = "utilpages" ]; then
  site=""
  while [ \$# -gt 0 ]; do
    case "\$1" in
      -site-public) site="\$2"; shift 2 ;;
      *) shift ;;
    esac
  done
  mkdir -p "\$site/u" "\$site/data"
  echo "<html>u index</html>" >"\$site/u/index.html"
  echo '{"generated_at":"2026-10-27T06:20:00+08:00"}' >"\$site/data/utilities.json"
  exit 0
fi
if [ "\$cmd" = "fbutil" ]; then
  exit "\${FAKE_FBUTIL_RC:-0}"
fi
echo "fake bin: unknown command \$cmd \$sub" >&2
exit 2
FAKE
chmod +x "$BIN"

calls() { [ -f "$CALLS" ] && cat "$CALLS" || true; }
reset_calls() { rm -f "$CALLS"; }
fetch_sections() { grep -o 'fetch -sections [^ ]*' "$CALLS" | head -1 || true; }

# --- temp git repo + bare origin ---
git init --bare -q "$TMP/origin.git"
git init -q -b main "$REPO"
git -C "$REPO" config user.name "test"
git -C "$REPO" config user.email "test@example.com"
mkdir -p "$REPO/site/public/data" "$REPO/site/public/u"
touch "$REPO/site/public/data/.keep" "$REPO/site/public/u/.keep"
git -C "$REPO" add site/public/data site/public/u
git -C "$REPO" commit -qm "seed"
git -C "$REPO" remote add origin "$TMP/origin.git"
git -C "$REPO" push -qu origin main
git -C "$REPO" branch --set-upstream-to=origin/main main 2>/dev/null || true
origin_rev() { git --git-dir="$TMP/origin.git" rev-parse refs/heads/main; }

# --- 1. Tuesday run fetches everything due ---
export UTIL_DOW=2 UTIL_DOM=12 UTIL_HOUR=6 UTIL_TODAY=2026-10-27
reset_calls
rev_before="$(origin_rev)"
if ! "$UTIL"; then bad "utilities.sh exited non-zero on Tue"; else ok "Tue run exit 0"; fi
[ "$(fetch_sections)" = "fetch -sections fuel,pump,meralco,lpg,water,fcda,power" ] \
  && ok "Tue 06:00 sections = all due + power" || bad "Tue sections: $(fetch_sections)"
calls | grep -q -- "-maynilad-x $PRESYO_HOME/inbox/maynilad_x.json" \
  && ok "Tue fetch passes default -maynilad-x" || bad "missing default -maynilad-x: $(calls)"
calls | grep -q "utilpages -snapshot $UDATA/snapshot.json -site-public $REPO/site/public" \
  && ok "utilpages runs with snapshot + site-public" || bad "utilpages call: $(calls)"
calls | grep -q "fbutil -db $DB -site-public $REPO/site/public -auto$" \
  && ok "fbutil -auto runs" || bad "fbutil call: $(calls)"
[ "$(origin_rev)" != "$rev_before" ] \
  && ok "commit pushed" || bad "no push happened"
git -C "$REPO" log -1 --format=%s | grep -qE '^utilities: [0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}$' \
  && ok "commit message is 'utilities: YYYY-MM-DD HH:MM'" || bad "commit message: $(git -C "$REPO" log -1 --format=%s)"

# --- 2. Thursday 08:00: stamped dailies + hourly water (no fuel, no fcda) ---
export UTIL_DOW=4 UTIL_DOM=12 UTIL_HOUR=8 UTIL_TODAY=2026-10-29
rm -rf "$UDATA/stamps"
reset_calls
if ! "$UTIL"; then bad "Thu 08:00 run failed"; else ok "Thu 08:00 exit 0"; fi
[ "$(fetch_sections)" = "fetch -sections meralco,lpg,water" ] \
  && ok "Thu 08:00 sections = meralco,lpg,water" || bad "sections: $(fetch_sections)"
calls | grep -q -- "-maynilad-x " \
  && ok "fetch passes -maynilad-x" || bad "missing -maynilad-x: $(calls)"

# --- 3. second run same day: stamps suppress dailies, hourly water still runs ---
reset_calls
if ! "$UTIL"; then bad "second run failed"; else ok "second run exit 0"; fi
[ "$(fetch_sections)" = "fetch -sections water" ] \
  && ok "second run fetches water only" || bad "sections: $(fetch_sections)"
calls | grep -q "fbutil .* -auto" \
  && ok "fbutil still runs with water-only fetch" || bad "fbutil missing: $(calls)"

# --- 4. Thursday 10:00: first run after 09:00 fetches fuel+pump ---
rm -rf "$UDATA/stamps"
export UTIL_HOUR=10
reset_calls
if ! "$UTIL"; then bad "Thu 10:00 run failed"; else ok "Thu 10:00 exit 0"; fi
[ "$(fetch_sections)" = "fetch -sections fuel,pump,meralco,lpg,water" ] \
  && ok "Thu 10:00 sections include fuel,pump,water" || bad "sections: $(fetch_sections)"

# --- 4b. power at hours divisible by 6, water always included ---
rm -rf "$UDATA/stamps"
export UTIL_DOW=4 UTIL_DOM=12 UTIL_HOUR=12 UTIL_TODAY=2026-10-29
reset_calls
if ! "$UTIL"; then bad "12:00 run failed"; else ok "12:00 exit 0"; fi
[ "$(fetch_sections)" = "fetch -sections fuel,pump,meralco,lpg,water,fcda,power" ] \
  && ok "12:00 sections include power + water" || bad "12:00 sections: $(fetch_sections)"
export UTIL_HOUR=13
reset_calls
if ! "$UTIL"; then bad "13:00 run failed"; else ok "13:00 exit 0"; fi
[ "$(fetch_sections)" = "fetch -sections water" ] \
  && ok "13:00 sections exclude power, keep water" || bad "13:00 sections: $(fetch_sections)"

# --- 5. disabled file skips fbutil only ---
mkdir -p "$PRESYO_HOME/.config/presyo"
touch "$PRESYO_HOME/.config/presyo/fbpost.disabled"
reset_calls
if ! "$UTIL"; then bad "disabled run failed"; else ok "disabled run exit 0"; fi
calls | grep -q "fbutil" \
  && bad "fbutil ran when disabled: $(calls)" || ok "fbutil skipped when disabled"
grep -q "fbpost.disabled exists" "$LOG" \
  && ok "disabled skip logged" || bad "disabled skip not logged"
rm -f "$PRESYO_HOME/.config/presyo/fbpost.disabled"

# --- 6. PRESYO_UTIL_DRY_RUN=1 adds -dry-run ---
export PRESYO_UTIL_DRY_RUN=1
reset_calls
if ! "$UTIL"; then bad "dry-run env run failed"; else ok "dry-run env exit 0"; fi
calls | grep -q "fbutil .* -auto -dry-run" \
  && ok "fbutil gets -dry-run" || bad "fbutil call: $(calls)"
unset PRESYO_UTIL_DRY_RUN

# --- 7. fbutil failure fails the run ---
export FAKE_FBUTIL_RC=2
reset_calls
if "$UTIL" >/dev/null 2>&1; then bad "run exited 0 despite fbutil failure"; else ok "run exits non-zero when fbutil fails"; fi
unset FAKE_FBUTIL_RC

# --- 8. lock prevents overlap ---
reset_calls
flock -n "$LOCK" sleep 8 &
holder=$!
sleep 0.5 # let the holder take the lock
out="$(bash "$UTIL" 2>&1)"; rc=$?
[ "$rc" -eq 0 ] && ok "locked run exits 0" || bad "locked run exit code $rc (want 0)"
echo "$out" | grep -qi "already running" \
  && ok "locked run prints already-running message" || bad "locked run printed: $out"
[ -z "$(calls)" ] && ok "locked run invokes nothing" || bad "locked run invoked: $(calls)"
wait "$holder"

echo "---"
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
