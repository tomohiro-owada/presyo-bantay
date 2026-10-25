# Sunday weekly report — spec for opencode

Goal: a weekly report (published Sunday) on the site and as ONE Facebook
post on Sunday ~18:00 Manila, consistent with the daily regional posts
(REGIONS_SPEC.md) and day pages (DAYPAGES_SPEC.md).

Hard rules: no git commits; never contact Facebook, GitHub or DA (tests use
httptest only); never read local secret files; never open ../data/presyo.db —
use /tmp/presyo-snap.db read-only (`file:...?mode=ro`). Static Go
(CGO_ENABLED=0), no new deps. Never print/log the token. Daily fbpost output
(text + PNG) for a given region/date must stay byte-identical; daypages
output for existing days must stay identical except where stated in §6.

## 1. Week definition
ISO week id `YYYY-Www` (e.g. 2026-W43 = Mon Oct 19 .. Sun Oct 25, 2026).
Report dates = DB price dates Mon..Sat of that week (Sunday excluded)
present at generation time ("covered dates"). Previous week = Mon..Sat of
the previous ISO week. A week is reportable when it has >= 3 covered dates
and the previous week has >= 3. Only mapped markets (internal/regions) are
used. Record missing Mon..Sat dates in signals (`missing_dates`) — e.g.
W43 currently lacks Sat Oct 24 (DA publishes with a lag).

## 2. Weekly signals (`internal/weekly`, deterministic, JSON-serialisable)
- Metro Manila-wide per commodity: for each covered date, median price
  across all mapped markets reporting that commodity; weekly average = mean
  of those daily medians (needs >= 3 dates this week and >= 3 last week).
  pct = round((avg_this/avg_prev - 1) * 100) as an integer. Display price =
  avg_this rounded to whole peso (store both raw and `display_price`).
  Drops: pct <= -5, sorted by pct asc (ties by name), top 5.
  Rises: pct >= +5, sorted desc, top 3.
- Best deals per region (5 regions, order south, manila, qc, north, east):
  for each (market, commodity) in the region, for each covered date where
  the market has a price, compute pct vs that market's own 30-day median
  as of that date (same basis/fallbacks as the daily region signals; reuse
  the code). Weekly market-deal pct = round(mean of those daily pcts),
  require >= 2 dates; display price = round(mean price). Keep pct <= -10.
  Pick up to 2 per region, best first, at most one entry per commodity.
- Units/names: reuse daily display names (TL/EN/JA) and market short names.

## 3. Text (`RenderWeekly`) — template only (no LLM), one combined post
```
🗓️ Lingguhang ulat — Okt 19–23, 2026
📉 Bumaba ngayong linggo (buong Metro Manila, vs nakaraang linggo):
• Sayote ₱45/kg (−20%)
...
📈 Tumaas:
• Siling pula ₱350/kg (+15%)
🏪 Pinakasulit kada rehiyon (vs usual na presyo):
• South Metro: Sayote · Alabang (−45%), Repolyo · Bicutan (−30%)
... (5 lines; "walang kapansin-pansin" if none)
━━━━━━━━━━
🇬🇧 Weekly report — Oct 19–23, 2026
📉 Down vs last week (Metro Manila avg): Chayote ₱45 (−20%), ...
📈 Up: Red chili ₱350 (+15%), ...
🏪 Best deals by area: South Metro — Chayote at Alabang (−45%); Manila & Pasay — ...
━━━━━━━━━━
🇯🇵 週間レポート — 10/19〜10/23
📉 先週比の値下がり（マニラ首都圏平均）：サヨテ ₱45（−20%）、…
📈 値上がり：赤唐辛子 ₱350（+15%）、…
🏪 地域別お買い得：南部 サヨテ −45%（Alabang）、マニラ・パサイ …
━━━━━━━━━━
% = average this week vs last week; area deals vs each market's 30-day usual price
Source: DA Price Monitoring · Hindi opisyal na DA page
📊 https://abalol.xyz/presyo-bantay/w/2026-W43/
```
Date range = first..last covered date. JA short region labels: 南部,
マニラ・パサイ, ケソン市, 北部, 東部 (no nested parens anywhere; reuse the
nested-parens lint). Market names stay Latin in JA (allowed). If Mon..Sat
dates are missing, add one line per language, e.g. TL "Walang datos ng DA:
Okt 24", EN "No DA data: Oct 24", JA "DAデータなし：10/24".
Empty sections: "Walang malaking pagbabago" / "No big changes" / "大きな変動なし".

## 4. Checker (`CheckWeekly`)
Same approach as fbregion.CheckPost: every ₱ amount and % in the text must
match the weekly signals value bound to the commodity (and market/region
for area deals) named on the same line/segment, correct sign, correct
metric (week-over-week vs area-deal). Lint: nested parens, "$", internal
jargon (median_30d, pct_vs, basis, focus_market, ncr). "Metro Manila" /
"マニラ首都圏" ARE allowed in weekly text. Tests with tampered numbers,
swapped commodities, wrong sign, wrong metric.

## 5. Chart (`internal/fbchart`, `RenderWeekly`) 1080x1350, same style
Cream bg, icon + "Presyo Bantay", big title "Lingguhang ulat", subtitle
"Okt 19–23, 2026 · Metro Manila · vs nakaraang linggo"; green pill
"Bumaba ngayong linggo" + up to 5 green bars (name, ₱avg/kg, inside %);
red pill "Tumaas" + up to 3 red bars; dark-green pill "Pinakasulit kada
rehiyon" + 5 compact rows "South Metro · Sayote −45% (Alabang)" (best one
per region, % as a small green chip); same footer as daily. Reuse the
existing drawing helpers, fonts, layout rules (labels never spill; no
nested parens). Compact PNG for the site via EncodeCompact.

## 6. Site pages (`presyo weekpages`, internal/weekpages or extend daypages)
Flags like daypages: `-db -site-public -week 2026-W43 | -from-week/-to-week
-force -base-url -html-only -now RFC3339`. Default target: the latest
reportable week whose Sunday <= today (Asia/Manila). FREEZE: if
`w/<week>/week.json` exists, do not re-render without -force (Saturday data
arriving later must not change a published report).
Writes `SITE/w/<week>/{week.png,week.json,index.html}`, `SITE/w/index.html`
(newest first), `SITE/data/weeks.json` {latest, weeks:[{id, path, label,
summary}]}. Page = same template style as day pages: GA4 G-VL047YCMK8 snippet
(identical to day pages), OG/Twitter (absolute og:image .../w/<week>/week.png,
1080x1350, canonical), lang toggle (pb-lang), privacy line (same 3
sentences), DA attribution, prev/next week links, links to /d/ archive and
top; content = chart + the 3 language blocks + "missing dates" note +
links to that week's day pages that exist. Deterministic output.
Integration: `d/index.html` archive gets a link "Lingguhang ulat / Weekly
reports / 週間レポート → ../w/" (this regenerates day archive HTML only;
day pages themselves unchanged). Site top page: new card or a line in the
daily-reports card linking the latest week (from data/weeks.json, missing
file = no-op), tl/en/ja strings, vitest for helpers.

## 7. Facebook: `presyo fbweekly`
Flags: `-db -week (default: ISO week containing "now" in Manila) -dry-run
-out-dir -auto -site-public -state -env-file -disable-file -min-gap 2h
-window-start 18:00 -window-end 20:00 -graph-base -now`. Same token
handling/redaction and Graph photo upload as fbpost (reuse the code).
- Uses the FROZEN page output when `SITE/w/<week>/week.json` exists
  (read text + PNG from there so FB == site); `-auto` REQUIRES it (skip
  "weekly page not generated yet"); dry-run without it computes fresh.
- `-auto` posts only if: Manila weekday is Sunday, now in [window-start,
  window-end), the week is reportable, no state entry with
  region "weekly" and as_of = week id, last successful post of ANY kind
  >= min-gap ago, disable file absent. At most one post.
- State: same state file as fbpost; entry {region:"weekly", as_of:"2026-W43",
  post_id, photo_id, posted_at}. Take an exclusive flock on
  "<state>.lock" around read-check-post-write in BOTH fbpost and fbweekly
  (syscall.Flock, pure Go).
- Changes to `fbpost -auto` (daily): (a) ignore "weekly" entries when
  deciding the next region; (b) the min-gap check counts weekly posts too;
  (c) Sunday hold: on Sunday Manila, from 16:00 until the weekly post for
  that week exists or 20:00, daily -auto skips ("holding for weekly post").
  Update fbpost tests accordingly; existing behaviour otherwise unchanged.
- `scripts/weekly.sh` (like watch.sh: PRESYO_HOME layout, path safety,
  flock, log to $PRESYO_HOME/logs/weekly.log, respects fbpost.disabled):
  runs `$BIN fbweekly -db $DB -auto -site-public $REPO/site/public`.
  `scripts/weekly_test.sh` with fake BIN.
- `scripts/daily.sh`: after daypages, run `$BIN weekpages -db $DB
  -site-public $REPO/site/public` unless PRESYO_WEEKPAGES=0; record
  weekpages_rc; commit `site/public/w` too. Update daily_test.sh.
- deploy.sh: print the new commented cron lines (VPS TZ is server-TZ = Manila+1h):
  `#FBWEEKLY 5 19,20 * * 0 $HOME/bin/weekly.sh   # Sun 18:05 & 19:05 Manila`

## 8. Tests / verification
Unit tests for signals (fixture DB or in-memory data), text, checker,
chart (size, labels inside), weekpages HTML (OG absolute, GA4, privacy,
prev/next, freeze), fbweekly auto decisions (table-driven with -now and
temp state: not Sunday, before 18:00, already posted, gap after a daily
post, disabled, page missing) using httptest for the Graph API, flock, and
daily Sunday-hold. gofmt, go vet ./..., go test ./..., scripts/daily_test.sh,
scripts/watch_test.sh, scripts/weekly_test.sh, site npm test + build.
Update go/README.md.
