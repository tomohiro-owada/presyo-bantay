#!/usr/bin/env bash
# Push a normalized Maynilad X inbox to the VPS (atomic upload).
#
# Usage: push_maynilad_x.sh INPUT.json [--dry-run]
#
# INPUT.json is a raw X tool response or a v1 inbox. The script merges it
# with the remote inbox (if any), normalizes, validates, and uploads
# atomically. Requires PRESYO_VPS_HOST (ssh host/alias; never hard-code any
# host, user or IP). Optional PRESYO_INBOX_PATH (default
# inbox/maynilad_x.json, relative to the remote home).
#
# --dry-run prints the normalized JSON path/summary and never touches ssh.
set -euo pipefail

DRY_RUN=0
INPUT=""
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help)
      echo "usage: push_maynilad_x.sh INPUT.json [--dry-run]" >&2
      exit 0
      ;;
    *)
      if [ -z "$INPUT" ]; then INPUT="$arg"; else
        echo "error: unexpected argument $arg" >&2; exit 1
      fi
      ;;
  esac
done

if [ -z "$INPUT" ]; then
  echo "error: need INPUT.json" >&2
  echo "usage: push_maynilad_x.sh INPUT.json [--dry-run]" >&2
  exit 1
fi
if [ ! -f "$INPUT" ]; then
  echo "error: input not found: $INPUT" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
NORMALIZER="$SCRIPT_DIR/maynilad_x_normalize.py"
REMOTE_PATH="${PRESYO_INBOX_PATH:-inbox/maynilad_x.json}"
REMOTE_DIR="$(dirname "$REMOTE_PATH")"
REMOTE_BASE="$(basename "$REMOTE_PATH")"
REMOTE_TMP="$REMOTE_DIR/.$REMOTE_BASE.tmp.$$"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

REMOTE_COPY="$TMPDIR/remote.json"
NORMALIZED="$TMPDIR/normalized.json"

if [ "$DRY_RUN" -eq 1 ]; then
  python3 "$NORMALIZER" "$INPUT" > "$NORMALIZED"
  python3 -m json.tool "$NORMALIZED" >/dev/null
  COUNT="$(python3 -c 'import json;print(len(json.load(open("'"$NORMALIZED"'"))["posts"]))')"
  NEWEST="$(python3 -c 'import json;d=json.load(open("'"$NORMALIZED"'"));print(max(p["created_at"] for p in d["posts"]) if d["posts"] else "-")')"
  echo "dry-run: normalized=$NORMALIZED posts=$COUNT newest=$NEWEST"
  echo "dry-run: would upload to $REMOTE_PATH (ssh untouched)"
  exit 0
fi

if [ -z "${PRESYO_VPS_HOST:-}" ]; then
  echo "error: PRESYO_VPS_HOST is not set (ssh host/alias)" >&2
  exit 1
fi

# Fetch the current remote inbox (missing file is OK -> empty merge).
ssh "$PRESYO_VPS_HOST" "cat $REMOTE_PATH 2>/dev/null" > "$REMOTE_COPY" || true

python3 "$NORMALIZER" "$INPUT" --merge-from "$REMOTE_COPY" > "$NORMALIZED"
python3 -m json.tool "$NORMALIZED" >/dev/null

COUNT="$(python3 -c 'import json;print(len(json.load(open("'"$NORMALIZED"'"))["posts"]))')"
NEWEST="$(python3 -c 'import json;d=json.load(open("'"$NORMALIZED"'"));print(max(p["created_at"] for p in d["posts"]) if d["posts"] else "-")')"

ssh "$PRESYO_VPS_HOST" "mkdir -p '$REMOTE_DIR' && cat > '$REMOTE_TMP' && mv '$REMOTE_TMP' '$REMOTE_PATH'" < "$NORMALIZED"

echo "uploaded: posts=$COUNT newest=$NEWEST path=$REMOTE_PATH"
