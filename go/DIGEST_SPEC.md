# DA price digest (replaces per-region Facebook posts)

Status: built and tested locally on branch `feat/da-digest`; **not deployed,
nothing posted**. Going live needs the owner's approval (see "Going live").
Server details (host, paths, user, time zone, cron lines) are kept in private
ops notes, never in this repository.

## Why

DA regional posts were 28 of 30 Presyo posts in one week (up to six a day).
Regions queued one per 2 h, a new report date pushed older regions out
(regions of a date were never finished), and the utilities job waited for
"all DA regions posted" (DA gate): in six days only one utilities post got
out. Per-region detail already lives on the site day pages.

## Design

* **One digest per new data date**, at most **2 digests a day**. Morning:
  the first window tick after the report arrived (typically 07:00 Manila,
  see schedule). Afternoon/evening update: only when a report for a new date
  arrives (or a region that was dropped now passes). No data = no post.
* **Content** (`internal/digest`, `internal/fbchart/digest.go`): 1080x1350
  card, rows = 8 key staples (rice, pork belly, chicken, egg, galunggong,
  tomato, red onion, sugar), columns = included regions. Cell = region median
  price on the data date D plus a trend triangle: change vs that region's
  *previous report date* (latest earlier date with >= 2 reporting markets,
  within 7 days). The change is **like-for-like**: only markets that priced
  the item on both dates, median vs median, so a market missing on one day
  cannot fake a move. Flat when |change| < 1 %. Rises red, falls green, flat
  grey. Caption (TL / EN / JA): metro median per staple with the same trend
  (`▲+4%`, `▼−2%`, `→±0%`), the best "cheaper than usual" deal across regions
  (from the existing regional signals), a line naming dropped regions, the
  day-page link. Every Presyo post keeps its trend vs previous.
* **Checker safety**: a region is dropped (not the whole digest) when its
  regional signals/checker (`fbregion.CheckPost`, same gate as the day
  pages) fails, it has no data, or fewer than 5 of 8 cells are valid. A
  cell is blanked when the price is outside 0.4x-2.5x of the region's own
  30-day median; a trend is blanked beyond +-40 % in a step. Fewer than 3
  included regions = no digest. A separate caption checker verifies every
  peso amount / percent and glyph sign against the digest numbers; a
  failure blocks the post (exit 3). Dropped regions are named in the
  caption and in the site day page.
* **Site**: `daypages` writes `d/D/digest.png` + a `digest` entry in
  `day.json` and shows the digest card above the regional sections
  (og:image too). Regional images/text stay.
* **No DA gate for utilities** (`fbutil`): removed with `-bills-max-wait`.
  Routine window (fuel/bills) is now 10:00-19:00. Water/power alerts keep
  bypassing window start/gap rules as before.
* **Spacing**: any two planned posts (digest, fuel/bills, weekly, traffic)
  keep **75 min** (`defaultMinGap`, grace 5 min; was 2 h). Emergencies keep
  the 30 min emergency gap. Digest-to-digest 3 h, 2 per day, window
  07:00-19:00 (optionally a later weekday start).
* **Unchanged**: kill switch (`fbpost.disabled`), state file + flock
  (`withStateLock`), Sunday 16:00-20:00 weekly hold (digest holds too),
  posting window/credentials handling, `daily.sh` ingest/daypages push,
  `utilities.sh`. The digest never reposts: state entry
  `{region:"da-digest", as_of:D, regions:[...]}`; legacy per-region entries
  for D count as covered (clean cut-over).
* `watch.sh` calls `fbdigest -auto -site-public <repo>/site/public` instead
  of `fbpost -auto`; after a successful `daily.sh` it pauses
  `PRESYO_PUBLISH_WAIT` (default 190 s) and `fbdigest` insists the day page is
  >= 3 min old so the caption link is live. `PRESYO_DA_POST=region` is the
  rollback switch (old behaviour); `PRESYO_DIGEST_DRY_RUN=1` is the shadow
  mode (renders to the digest out dir, never posts, no state).
* **Traffic slot reservation** (`PRESYO_TRAFFIC_RESERVE=1`, off by default):
  on weekdays digest and routine utilities stay out of the 70 min before the
  07:00 / 17:30 traffic slots until the slot is posted or its window ends, so
  the 75 min gap can never make the traffic map miss its slot.

## Daily slot plan (Manila)

| time | what | notes |
|---|---|---|
| 06:00 | Sakuna daily | separate Facebook Page, own token/state/kill switch/caps (not in this timeline) |
| 07:00 | weekday traffic map | proposed, gated off; reserves 05:50-07:40 |
| 07:00 (08:15 on weekdays with traffic) | DA digest #1 | first tick after the report; Sat/Sun 07:00 |
| 10:00-19:00 | fuel / bills / planned power | `:20` ticks, >= 75 min after any post |
| 15:00-19:00 (until 16:15 with traffic) | DA digest #2 (only for a new date) | |
| 17:30 | weekday traffic map | proposed, gated off |
| any time 05:30-20:00 | water / power emergencies | bypass routine window, 30 min emergency gap |
| Sun 18:05 | weekly report | digest + routine posts hold from 16:00 |

## Going live (owner approval required)

1. Review preview images/captions and the simulated timeline.
2. Deploy the binary and scripts (`deploy.sh`, owner-approved); the cron lines
   do not need to change for the first stage.
3. Shadow stage: `PRESYO_DIGEST_DRY_RUN=1` in the watch job environment for a
   day (digest renders, nothing posts; legacy per-region posting is off in
   this mode, so also touch the kill switch or set `PRESYO_DA_POST=region`
   for the old behaviour while shadowing).
4. Switch: unset the dry-run variable; keep a way back
   (`PRESYO_DA_POST=region`, or the kill switch).
5. Traffic: when it goes live add `PRESYO_TRAFFIC_RESERVE=1` to the
   watch/utilities jobs and `-weekday-window-start 08:15`-style timing is
   handled automatically by the reservation + gap rule.

## Known limits

* A same-date correction by DA is not re-posted (an update only fires for
  new dates or newly includable regions).
* Report dates arriving after 19:00 wait for the next morning's tick.
* The day page digest is frozen with `day.json` (never re-rendered without
  `-force`); an afternoon update for the same date posts to Facebook with
  fresh numbers while the site card keeps the first version.
* Metro median = median of every reporting market in the included regions,
  not a basket index.
