# Meralco emergency line trouble from X (@meralco)

The Meralco maintenance-schedule site covers planned interruptions.
Emergency line trouble is only announced on X, so a scheduled routine
(outside the VPS) reads @meralco (user id 15371662) with X's public
read tools and pushes a normalized v1 JSON file to
`$PRESYO_HOME/inbox/meralco_x.json`. The Go side (`go/internal/utilities`,
`meralco_x.go`) reads it for the `meralcox` section.

## Inbox format (v1)

Same shape as `UTILITIES_MAYNILAD_X.md`:

```json
{
  "version": 1, "account": "meralco", "account_id": "15371662",
  "fetched_at": "2026-10-30T12:00:00+08:00",
  "posts": [ {"id": "<string>", "created_at": "2026-10-30T11:00:00+08:00",
    "text": "<full text, note_tweet preferred>",
    "url": "https://x.com/meralco/status/<id>",
    "lang": "en", "images": [], "has_video": false} ]
}
```

`text` is always the full text (`note_tweet.text` when present).
`url` is canonical.

## Post patterns

Emergency (yields an event, Las Piñas only, accent-insensitive):

> Your area in \<city\> may have been affected by an emergency line
> trouble which occurred at \<time\> with an estimated restoration
> time of N hrs

`which`/`that` both match; clock like `8:30 AM`/`8:30 a.m.`;
`estimated restoration time of N hrs|hours` is optional.
Posts containing restoration markers (`restored`, `has resumed`,
`back to normal`, `naibalik`, `bumalik na`, `tapos na`, `lifted`, ...)
never yield events.

Scheduled (ignored, never an emergency):

> ADVISORY: May scheduled power interruptions in several areas sa
> \<cities\> sa \<dates\>

Only posts whose area text mentions Las Piñas yield events.
Resolution posts never yield events.

## Tooling (repo root `tools/`)

- `tools/push_meralco_x.sh INPUT.json [--dry-run]`: same pipeline as
  `push_maynilad_x.sh` (merge with the remote inbox over ssh,
  `PRESYO_VPS_HOST` required, never hard-coded;
  `PRESYO_MERALCO_INBOX_PATH` defaults to `inbox/meralco_x.json`),
  normalizer args `--account meralco --account-id 15371662
  --originals-only`. Replies/retweets are dropped (customer-support
  replies carry private handles and are never outage alerts).
  See `--dry-run` for offline checks.
- Tests: `tools/test_meralco_x_normalize.py`,
  `tools/test_push_meralco_x.sh` (fake `ssh` on PATH, no network).

## Go behaviour

- `utilities fetch -meralco-x FILE [-sections ...,meralcox]`: the
  `meralcox` section works offline (local file read). Missing inbox
  file: silent, no failure, no section (so `scripts/utilities.sh`
  always passes the flag). Malformed JSON/version or wrong account:
  error with the base name only (never the full path). Stale inbox
  (`fetched_at` older than 12h at fetch time): warning only.
- City-level only (no barangays): captions use the generic marker
  (`PowerAreasTL/EN/JA`: "Ilang barangay — tingnan ang abiso ng
  Meralco" / ...), never copied sentences.
- Window = occurred time + ETR: `Start` is the occurred clock on the
  post's Manila date (rolled back one day when >30m in the future);
  `End = Start + ETR` with `EstimatedEnd` when an ETR parsed, else
  `OpenEnded` (`Start` = post time, due only within 24h of the post).
  Events older than 7d at parse time, or starting >48h ago, are
  dropped; restoration posts dropped. Dedupe by (day, area, clock,
  ETR) keeps the earliest post.
- `BuildMeralcoXAlerts`: one emergency `PowerAlert` per event (`ID`
  `x-<postID>`, `Kind` `emergency`, `City` Las Piñas, `ReasonKey`
  `emergency`). An event overlapping any planned alert window by
  >=50% of the shorter window is dropped; events ended >24h ago, or
  open-ended posted >24h ago, are dropped.
- `fbutil -auto` emergency power rules (`cmd_fbutil.go`):
  planned (`AlertKind != "emergency"`) keeps the old rules (one
  `util-power` per Manila day, 72h start horizon, not ended);
  emergency (`AlertKind == "emergency"`) is due when `End` is set and
  after now, or `OpenEnded` and `now <= GeneratedAt+24h` (no 72h
  rule, starts in the past), `oldEnough` still applies. Emergencies
  are tried before planned (earliest `Start` first), are exempt from
  the planned one-per-day rule (`postedToday` ignores `x-` ids) but
  capped at 3 emergency posts per Manila day. Overlap dedupe
  (`WindowsOverlap50` vs frozen `post.json` windows, `OpenEnded`
  uses `GeneratedAt+24h`): emergencies skip when overlapping any
  posted `util-power` entry, planned skip when overlapping a posted
  emergency entry. Min-gap: `-emergency-gap` (default 30m, same
  `-gap-grace`) blocks everything below it; between it and `-min-gap`
  (default 2h) only emergencies (water `AlertKind` emergency, power
  emergency) may post. Priority water > power emergency > power
  planned > fuel > bills; window 05:30-20:00 unchanged; the Sunday
  16:00-20:00 weekly hold gates only non-emergency candidates.
- `scripts/utilities.sh`: `MERALCO_X_INBOX` (default
  `$PRESYO_HOME/inbox/meralco_x.json`) is always passed as
  `-meralco-x`, and section `meralcox` runs on every hourly
  invocation (like `water`); the maintenance `power` section stays on
  hours divisible by 6.

## Cadence (30 minutes)

The box routine pushes `~/inbox/meralco_x.json` about every 30 minutes
(:00 and :30 Manila, 05:00-20:00). The VPS runs the emergency path at about
:10 and :40 Manila with `utilities.sh --meralcox-only` (server-TZ cron
`10,40 6-20 * * *`): fetch only the `meralcox` section, then the usual
utilpages / commit / `fbutil -auto` steps. It takes the same lock as the
hourly job (waits up to 240 s), writes no daily stamps, and `fbutil -auto`
still applies the posting window, the gaps (30 min for emergencies) and
the kill switch. The hourly `utilities.sh` keeps fetching `meralcox` as well.
