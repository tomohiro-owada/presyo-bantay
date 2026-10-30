#!/usr/bin/env bash
# Test for go/deploy.sh target handling (no network, no real deploy):
# the host comes from the environment or an untracked .deploy.env, never
# from the script itself.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEPLOY="$SCRIPT_DIR/../deploy.sh"
pass=0; fail=0
ok() { pass=$((pass+1)); echo "ok: $1"; }
bad() { fail=$((fail+1)); echo "FAIL: $1" >&2; }
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

# 1. no host, dry run: placeholder host, exit 0, nothing executed
out="$(env -u VPS_HOST -u REMOTE_DIR DRY_RUN=1 bash "$DEPLOY" 2>&1)" && ok "dry-run without host exits 0" || bad "dry-run failed: $out"
echo "$out" | grep -q "<vps-host>" && ok "dry-run uses placeholder host" || bad "no placeholder host"

# 2. no host, real mode: refuses
if env -u VPS_HOST bash "$DEPLOY" >/dev/null 2>"$TMP/err"; then bad "real run without host succeeded"; else ok "real run without host fails"; fi
grep -q "VPS_HOST is not set" "$TMP/err" && ok "error names VPS_HOST" || bad "error text: $(cat "$TMP/err")"

# 3. host from env
out="$(VPS_HOST=example-host DRY_RUN=1 bash "$DEPLOY" 2>&1)"
echo "$out" | grep -q "scp bin/presyo example-host:./bin/presyo.new" && ok "env host used, remote dir defaults to remote home" || bad "env host: $out"

# 4. host from untracked .deploy.env next to the script
mkdir -p "$TMP/g"; cp "$DEPLOY" "$TMP/g/deploy.sh"
printf 'VPS_HOST=envfile-host\nREMOTE_DIR=srv\n' >"$TMP/g/.deploy.env"
out="$(env -u VPS_HOST -u REMOTE_DIR DRY_RUN=1 bash "$TMP/g/deploy.sh" 2>&1)"
echo "$out" | grep -q "envfile-host:srv/bin/presyo.new" && ok ".deploy.env honoured" || bad ".deploy.env: $out"

# 5. the script carries no server identifiers
if grep -nE 'presyo[-]vps|/home/pre[s]yo|crontab -l' "$DEPLOY" >/dev/null; then bad "server info in deploy.sh"; else ok "no server info in deploy.sh"; fi

echo "---"; echo "pass=$pass fail=$fail"; [ "$fail" -eq 0 ]
