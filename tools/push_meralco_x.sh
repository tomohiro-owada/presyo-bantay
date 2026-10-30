#!/usr/bin/env bash
# Push a normalized Meralco (@meralco, id 15371662) X inbox to the VPS.
#
# Usage: push_meralco_x.sh INPUT.json [--dry-run]
#
# Same pipeline as push_maynilad_x.sh (merge with the remote inbox, validate,
# atomic upload); only the account and the remote path differ:
#   remote path: $PRESYO_MERALCO_INBOX_PATH (default inbox/meralco_x.json)
#   normalizer : --account meralco --account-id 15371662 --originals-only
# Replies/retweets are dropped (customer-support replies carry private
# handles and are never outage alerts). Needs PRESYO_VPS_HOST unless --dry-run.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
export PRESYO_INBOX_PATH="${PRESYO_MERALCO_INBOX_PATH:-inbox/meralco_x.json}"
export NORMALIZER_ARGS="--account meralco --account-id 15371662 --originals-only"
exec "$SCRIPT_DIR/push_maynilad_x.sh" "$@"
