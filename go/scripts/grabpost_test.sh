#!/usr/bin/env bash
# Shell test for go/scripts/grabpost.sh (fake BIN, no network).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
JOB="$SCRIPT_DIR/grabpost.sh"
pass=0; fail=0
ok() { pass=$((pass + 1)); echo "ok: $1"; }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
export PRESYO_HOME="$TMP/home"
export BIN="$PRESYO_HOME/bin/presyo"
export LOG="$PRESYO_HOME/logs/grabpost.log"
export LOCK="$PRESYO_HOME/.grabpost.lock"
export ENV_FILE="$PRESYO_HOME/.config/presyo/grab.env"
mkdir -p "$PRESYO_HOME/bin" "$PRESYO_HOME/logs" "$PRESYO_HOME/.config/presyo"
printf 'PRESYO_GRAB_FB=1\nPRESYO_GRAB_API_TOKEN=secret-token-xyz\n' >"$ENV_FILE"
cat >"$BIN" <<'FAKE'
#!/usr/bin/env bash
[ "$1" = grabcard ] && [ "$2" = "-auto" ] || exit 1
echo "gate=${PRESYO_GRAB_FB:-unset} home=${PRESYO_HOME:-unset}" >"$PRESYO_HOME/fake.seen"
exit "${FAKE_RC:-0}"
FAKE
chmod +x "$BIN"

"$JOB" && ok "rc 0 passes" || bad "rc 0 failed"
grep -q "gate=1" "$PRESYO_HOME/fake.seen" && ok "env file loaded (gate visible to the binary)" || bad "gate not loaded"
FAKE_RC=4 "$JOB" && ok "exit 4 (no fresh data) becomes rc 0" || bad "exit 4 not tolerated"
if FAKE_RC=1 "$JOB"; then bad "failure swallowed"; else ok "failure propagates"; fi
grep -q "secret-token-xyz" "$LOG" && bad "token in log" || ok "token never logged"
if BIN=/tmp/x "$JOB" 2>/dev/null; then bad "outside path accepted"; else ok "outside PRESYO_HOME refused"; fi
( flock -n 9 && sleep 3 ) 9>"$LOCK" &
sleep 0.5
"$JOB" | grep -q "already running" && ok "overlapping tick exits quietly" || bad "overlap not handled"
wait
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
