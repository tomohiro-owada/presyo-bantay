#!/usr/bin/env bash
# Test for tools/push_maynilad_x.sh with a fake `ssh` on PATH (no network).
#
# Proves: merge, dedupe, atomic mv command, and that nothing is uploaded
# if normalization fails. Also proves --dry-run never touches ssh.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PUSH="$SCRIPT_DIR/push_maynilad_x.sh"
NORM="$SCRIPT_DIR/maynilad_x_normalize.py"

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

# --- seed remote inbox with one stale + one shared post ---
python3 - <<PY
import json, os
posts = [
  {"id": "SHARED1", "created_at": "2026-10-27T10:00:00+08:00", "text": "STALE shared text",
   "url": "https://x.com/maynilad/status/SHARED1", "lang": "en", "images": [], "has_video": False},
  {"id": "REMOTEONLY", "created_at": "2026-10-27T11:00:00+08:00", "text": "remote only post",
   "url": "https://x.com/maynilad/status/REMOTEONLY", "lang": "en", "images": [], "has_video": False},
]
json.dump({"version": 1, "account": "maynilad", "account_id": "2992779308",
           "fetched_at": "2026-10-27T12:00:00+08:00", "posts": posts},
          open("$FAKEREMOTE/inbox/maynilad_x.json", "w"))
PY

# --- input: shared id with NEW text + one brand-new post ---
INPUT="$TMP/input.json"
python3 - <<PY
import json
posts = [
  {"id": "SHARED1", "created_at": "2026-10-28T10:00:00+08:00", "text": "NEW shared text",
   "url": "https://x.com/maynilad/status/SHARED1", "lang": "en", "images": [], "has_video": False},
  {"id": "NEW1", "created_at": "2026-10-28T11:00:00+08:00", "text": "brand new",
   "url": "https://x.com/maynilad/status/NEW1", "lang": "en", "images": [], "has_video": False},
]
json.dump({"version": 1, "account": "maynilad", "account_id": "2992779308",
           "fetched_at": "2026-10-28T12:00:00+08:00", "posts": posts},
          open("$INPUT", "w"))
PY

: > "$SSH_LOG"
if ! bash "$PUSH" "$INPUT" >/tmp/push_out.txt 2>&1; then bad "push failed: $(cat /tmp/push_out.txt)"; else ok "push exit 0"; fi

# merge: remote-only kept, shared updated (new wins), dedupe by id
python3 - <<'PY'
import json, os
d = json.load(open(os.environ["FAKEREMOTE"] + "/inbox/maynilad_x.json"))
by = {p["id"]: p for p in d["posts"]}
assert by["SHARED1"]["text"] == "NEW shared text", by["SHARED1"]
assert "REMOTEONLY" in by, "remote-only lost"
assert "NEW1" in by, "new post lost"
assert len(d["posts"]) == 3, d["posts"]
print("merge-ok")
PY
[ $? -eq 0 ] && ok "merge + dedupe (new wins)" || bad "merge content wrong"

grep -q "mv" "$SSH_LOG" && grep -q "\.tmp\." "$SSH_LOG" \
  && ok "atomic mv command used" || bad "atomic mv missing: $(cat "$SSH_LOG")"
grep -q "mkdir -p" "$SSH_LOG" \
  && ok "mkdir -p in upload" || bad "mkdir missing"

# --- failure case: invalid input uploads nothing ---
BAD="$TMP/bad.json"
echo '{"nope": true}' > "$BAD"
rev_before="$(cat "$FAKEREMOTE/inbox/maynilad_x.json")"
: > "$SSH_LOG"
if bash "$PUSH" "$BAD" >/dev/null 2>&1; then bad "bad input unexpectedly succeeded"; else ok "bad input fails"; fi
[ "$(cat "$FAKEREMOTE/inbox/maynilad_x.json")" = "$rev_before" ] \
  && ok "nothing uploaded on failure" || bad "remote changed on failure"
# only the fetch ssh (cat) may have run; no mkdir/mv upload
if grep -q "mv" "$SSH_LOG"; then bad "upload attempted on failure"; else ok "no upload on failure"; fi

# --- dry-run never touches ssh ---
: > "$SSH_LOG"
if ! bash "$PUSH" "$INPUT" --dry-run >/tmp/dry.txt 2>&1; then bad "dry-run failed"; else ok "dry-run exit 0"; fi
[ ! -s "$SSH_LOG" ] && ok "dry-run touches no ssh" || bad "dry-run used ssh: $(cat "$SSH_LOG")"
grep -q "posts=" /tmp/dry.txt && ok "dry-run prints summary" || bad "dry-run summary missing"

echo "---"
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
