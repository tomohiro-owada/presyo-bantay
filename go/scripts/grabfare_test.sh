#!/usr/bin/env bash
# Shell test for go/scripts/grabfare.sh (no network: fake BIN, local bare remote).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
JOB="$SCRIPT_DIR/grabfare.sh"
pass=0; fail=0
ok() { pass=$((pass + 1)); echo "ok: $1"; }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
export PRESYO_HOME="$TMP/home"
export BIN="$PRESYO_HOME/bin/presyo"
export LOG="$PRESYO_HOME/logs/grabfare.log"
export LOCK="$PRESYO_HOME/.grabfare.lock"
export REPO="$PRESYO_HOME/repo"
mkdir -p "$PRESYO_HOME/bin" "$PRESYO_HOME/logs" "$PRESYO_HOME/.config/presyo"
git init -q --bare "$TMP/remote.git"
git init -q -b main "$REPO"
git -C "$REPO" remote add origin "$TMP/remote.git"
git -C "$REPO" -c user.name=t -c user.email=t@t commit -q --allow-empty -m init
git -C "$REPO" push -q origin main
MAIN_BEFORE="$(git -C "$TMP/remote.git" rev-parse main)"
cat >"$BIN" <<'FAKE'
#!/usr/bin/env bash
# fake presyo: grabfare -env-file F -out-dir D
[ "$1" = grabfare ] || exit 1
out=""; while [ $# -gt 0 ]; do [ "$1" = "-out-dir" ] && out="$2"; shift; done
[ "${FAKE_RC:-0}" = 0 ] || exit "$FAKE_RC"
printf '{\n  "generated_at": "%s",\n  "visible": true,\n  "fare": %s\n}\n' "$(date +%s%N)" "${FAKE_FARE:-308}" >"$out/grab-fare.json"
FAKE
chmod +x "$BIN"

# 1. publish off (default): local file only, no data branch.
"$JOB"
[ -f "$PRESYO_HOME/state/grab-fare/grab-fare.json" ] && ok "writes local file" || bad "no local file"
git -C "$TMP/remote.git" rev-parse --verify -q grab-data >/dev/null && bad "published while off" || ok "no push while PUBLISH off"

# 2. publish on: orphan branch created, main untouched.
export PRESYO_GRABFARE_PUBLISH=1
"$JOB"
git -C "$TMP/remote.git" rev-parse --verify -q grab-data >/dev/null && ok "data branch pushed" || bad "data branch missing"
[ "$(git -C "$TMP/remote.git" rev-parse main)" = "$MAIN_BEFORE" ] && ok "main untouched" || bad "main changed"
[ "$(git -C "$TMP/remote.git" ls-tree --name-only grab-data)" = "grab-fare.json" ] && ok "branch holds only grab-fare.json" || bad "unexpected branch content"

# 3. only generated_at differs -> skipped; changed fare -> amended (still one commit).
H1="$(git -C "$TMP/remote.git" rev-parse grab-data)"
"$JOB"
[ "$(git -C "$TMP/remote.git" rev-parse grab-data)" = "$H1" ] && ok "generated_at-only change skipped" || bad "pushed on generated_at only"
FAKE_FARE=315 "$JOB"
[ "$(git -C "$TMP/remote.git" rev-parse grab-data)" != "$H1" ] && ok "changed data pushed" || bad "change not pushed"
[ "$(git -C "$TMP/remote.git" rev-list --count grab-data)" = "1" ] && ok "history stays one commit" || bad "history grew"

# 4. fetch failure -> non-zero, nothing pushed.
H2="$(git -C "$TMP/remote.git" rev-parse grab-data)"
if FAKE_RC=1 FAKE_FARE=999 "$JOB"; then bad "failure not propagated"; else ok "failure propagates"; fi
[ "$(git -C "$TMP/remote.git" rev-parse grab-data)" = "$H2" ] && ok "no push after failure" || bad "pushed after failure"

# 5. outside PRESYO_HOME refused.
if BIN=/tmp/x "$JOB" 2>/dev/null; then bad "outside path accepted"; else ok "outside PRESYO_HOME refused"; fi
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
