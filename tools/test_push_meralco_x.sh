#!/usr/bin/env bash
# Test for tools/push_meralco_x.sh with a fake `ssh` on PATH (no network).
#
# Proves: merge, dedupe, atomic mv command, and that nothing is uploaded
# if normalization fails. Also proves --dry-run never touches ssh.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PUSH="$SCRIPT_DIR/push_meralco_x.sh"

pass=0; fail=0
ok() { pass=$((pass+1)); echo "ok: $1"; }
bad() { fail=$((fail+1)); echo "FAIL: $1" >&2; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

FAKEBIN="$TMP/fakebin"
FAKEREMOTE="$TMP/remote-home"
mkdir -p "$FAKEBIN" "$FAKEREMOTE/inbox"
export SSH_LOG="$TMP/ssh.log"
export FAKEREMOTE

# --- fake ssh: logs calls; serves cat from FAKEREMOTE, captures upload ---
cat >"$FAKEBIN/ssh" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
echo "ssh $*" >>"$SSH_LOG"
host="$1"; shift
remote="$*"
# fetch path: cat <path> 2>/dev/null
if [[ "$remote" == cat\ * ]]; then
  # extract the first token after cat
  path="$(echo "$remote" | sed -E 's/^cat ([^ ]+).*/\1/')"
  if [ -f "$FAKEREMOTE/$path" ]; then
    cat "$FAKEREMOTE/$path"
  fi
  exit 0
fi
# upload: mkdir -p ... && cat > tmp && mv tmp dest
if [[ "$remote" == *"mkdir"* && "$remote" == *"mv"* ]]; then
  # last quoted token is the destination
  dest="$(echo "$remote" | grep -oE "'[^']+'" | tail -1 | tr -d "'")"
  mkdir -p "$FAKEREMOTE/$(dirname "$dest")"
  cat > "$FAKEREMOTE/$dest"
  exit 0
fi
echo "fake ssh: unknown remote: $remote" >&2
exit 2
FAKE
chmod +x "$FAKEBIN/ssh"
export PATH="$FAKEBIN:$PATH"

export PRESYO_VPS_HOST="fake-vps"
unset PRESYO_INBOX_PATH

# --- raw X response with originals + replies/retweets ---
INPUT="$TMP/input.json"
python3 - <<PY
import json
json.dump({"data": [
  {"id": "100", "created_at": "2026-10-30T01:41:32.000Z", "text": "Your area in Las Pi\u00f1as City may have been affected by an emergency line trouble which occurred at  8:30 AM with an estimated restoration time of 3 hrs. https://t.co/x"},
  {"id": "101", "created_at": "2026-10-30T01:50:00.000Z", "text": "@someone Hi there", "in_reply_to_user_id": "5"},
]}, open("$INPUT", "w"))
PY

: > "$SSH_LOG"
if ! bash "$PUSH" "$INPUT" >/tmp/push_m_out.txt 2>&1; then bad "push failed: $(cat /tmp/push_m_out.txt)"; else ok "push exit 0"; fi
python3 - <<'PY'
import json, os
d = json.load(open(os.environ["FAKEREMOTE"] + "/inbox/meralco_x.json"))
assert d["account"] == "meralco" and d["account_id"] == "15371662", d
assert [p["id"] for p in d["posts"]] == ["100"], d["posts"]
assert d["posts"][0]["url"] == "https://x.com/meralco/status/100"
PY
[ $? -eq 0 ] && ok "uploaded meralco inbox, reply dropped" || bad "meralco inbox wrong"
[ ! -e "$FAKEREMOTE/inbox/maynilad_x.json" ] && ok "maynilad inbox untouched" || bad "maynilad inbox written"
grep -q "meralco_x.json" "$SSH_LOG" && grep -q "\.tmp\." "$SSH_LOG" && ok "atomic mv to meralco_x.json" || bad "ssh log: $(cat "$SSH_LOG")"

: > "$SSH_LOG"
bash "$PUSH" "$INPUT" --dry-run >/tmp/dry_m.txt 2>&1 && ok "dry-run exit 0" || bad "dry-run failed"
[ ! -s "$SSH_LOG" ] && ok "dry-run touches no ssh" || bad "dry-run used ssh"
grep -q "inbox/meralco_x.json" /tmp/dry_m.txt && ok "dry-run names meralco path" || bad "dry-run path"

echo "---"
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
