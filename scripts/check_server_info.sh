#!/usr/bin/env bash
# Fail when tracked files carry server identifiers (host aliases, home
# paths, deploy-key aliases, server time zone, optional private patterns).
# Operational details belong in private ops notes outside the repository.
set -euo pipefail
cd "$(dirname "$0")/.."
# Bracketed characters keep this file from matching its own patterns.
# Extra private patterns (e.g. the server IP prefix) come from the environment
# or an untracked file; never commit them.
PAT='presyo[-]vps|/home/pre[s]yo|/home/saku[n]a|github-pre[s]yo|github-saku[n]a|Asia/Tok[y]o|saku[n]a[-]vps'
if [ -n "${SERVER_INFO_EXTRA_PAT:-}" ]; then PAT="$PAT|$SERVER_INFO_EXTRA_PAT"; fi
if [ -f go/.server-info-pattern ]; then PAT="$PAT|$(cat go/.server-info-pattern)"; fi
if hits="$(git grep -n -I -E "$PAT" -- . ':!scripts/check_server_info.sh' ':!go/scripts/deploy_test.sh' ':!tests/fixtures' ':!go/internal/utilities/testdata')"; then
  echo "server info found in tracked files:" >&2
  echo "$hits" >&2
  exit 1
fi
echo "ok: no server identifiers in tracked files"
