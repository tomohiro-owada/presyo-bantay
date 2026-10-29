# Maintenance feeds: Maynilad weekly schedule (RSS) + Meralco planned interruptions

Extends UTILITIES_SPEC.md / UTILITIES_SPEC_V2.md / UTILITIES_MAYNILAD_X.md (same hard rules).

## Hard rules (unchanged + additions)
- Tests NEVER touch the network. Fixtures live in `internal/utilities/testdata/`
  (`maynilad_feed/feed_p1..p8.xml`, `meralco_ms/*.html`, both captured 2026-10-29 from the
  official sites; do not edit them; add new fixtures only as new files). `research/maintenance/`
  has the raw research captures (read-only reference).
- Never try to bypass a bot challenge. No header tricks, no cookies, no retries. If the Maynilad
  feed returns 403 / `cf-mitigated: challenge` / a "Just a moment" body, treat it as "feed blocked":
  record the Source (http status, err text "blocked; using X inbox"), print ONE stderr notice line,
  do NOT add it to `Snapshot.Errors`, and keep using the X inbox. Same for any non-200 Meralco
  response except it IS recorded in Errors (and the previous snapshot section is kept).
- Polite fetching: UA = the existing UserAgent string, MinDelay >= 3s per host (existing option),
  one attempt per URL, 60 s timeout, hard request cap per run.
- No new module dependencies, CGO off, gofmt, `go vet ./...`, `go test ./...` must pass. Known
  pre-existing failures: TestAutoOrder, TestAutoGap, TestFBPostSundayHold (stale fixtures) - do not
  touch them; everything else must be green.
- No git state changes. No server info (hostnames, usernames, paths of the VPS) in any file.
- Never invent numbers. Our own wording only (reason keys), never copy their sentences.
  Existing daily/weekly/fuel/bills outputs must stay byte-identical.
- Do NOT deploy, edit crontab, or post to Facebook. Graph only via httptest.

## A. Maynilad weekly schedule from the WordPress RSS feed

Source: `{MayniladBase}/feed/` (page 1 holds ~4 weeks of weekly posts; request `?paged=2` only when
page 1 has no weekly post; max 2 requests). It is currently NOT challenged (robots.txt allows it).
Weekly posts are titled "Maintenance activities for the upcoming week" (also seen: "Maintenance
activity for the upcoming week" - match `maintenance activit(y|ies) for the upcoming week`, case
-insensitive). `content:encoded` has `<figure class="wp-block-table"><table>` with a header row
(`City | Barangay | Schedule | Activity | Location of Activity`; the order changed between posts,
e.g. Schedule/Activity swapped) and cells with `rowspan`, or blank/missing City cells that inherit
the previous city. Look at all fixtures `feed_p1..p8.xml` to see variants (rowspan on city and on
other columns, "Brgy. 8, 25, 28, ..." lists, "Bacoor, Cavite" cities, a Valenzuela row with 3 schedules
and empty barangay/activity cells, entries where several schedule rows share a location).

1. `ParseMayniladFeed(body []byte) ([]Maintenance, error)`: all weekly posts, all cities. Map
   columns by header name (case-insensitive; "Location of Activity" -> Location). Honor `rowspan`
   properly (a rowspan'd cell carries into following rows for that column); also carry blank City
   cells forward. Keep `PostDate` (pubDate -> Manila date), `PostURL` (item link). Cities
   normalised: "Las Pinas City"/"Las Piñas"/"Las Piñas City" -> "Las Piñas"; "Parañaque"/"Paranaque
   City" -> "Parañaque"; "Muntinlupa City" -> "Muntinlupa"; "Bacoor, Cavite"/"Bacoor City" ->
   "Bacoor"; other cities keep the cell text trimmed. A row ALSO counts for a nearby city when that
   city name appears in the Barangay or Location cell (e.g. Imus row listing "..., Bacoor City;
   Brgys. Pasong Buaya I..."): add `AlsoCities []string` (normalised) to the row. Error only when the
   feed is not RSS/XML at all or has zero weekly posts is NOT an error (empty result); a weekly post
   whose table has no parsable header is skipped and counted (return a warning count via a second
   result or a package-level parse report struct).
2. Schedules (Manila time, `manilaLoc`): extend/replace `parseSchedTime`/`splitWindow` use in a new
   `ParseMaintSchedule(s string) ([]TimeWindow, MaintSchedule)` that handles
   - single window: "10:00 p.m. of August 26, 2026 to 1:00 p.m. of August 27, 2026"
   - same-day short window: "1:00 p.m. to 7:00 p.m. of August 24, 2026" (date once at the end)
   - daily range: "11:01 p.m. to 3:00 a.m. daily, from March 22 to March 29, 2026" (also
     "from January 25, 2026 to February 1, 2026", and "12:01 a.m. to 3:00 a.m. daily, from
     March 23 to March 29, 2026"): one `TimeWindow{Start,End}` per night (end next day when
     end<=start), flagged `Daily`, with `DailyFrom/DailyTo` (dates) and clock times kept so the
     post can say "every night 11:01 PM to 3:00 AM, Mar 22 to 29".
   - "10:00 p.m. from January 25, 2026 to 6:00 a.m. of January 26, 2026" and other odd variants seen
     in the fixtures. Anything unparsable keeps the raw text and `Windows` empty (such rows are
     shown on the site as raw text but never posted).
   Add `Windows []TimeWindow` and `Daily bool` (+ the daily fields) to `Maintenance` (json
   omitempty so older snapshots still load).
3. Retention: keep only rows whose last window end is >= now-24h (or unparsed rows from posts
   <= 8 days old). Las Piñas rows (City or AlsoCities) -> `WaterStatus.Maintenance` (existing field,
   so BuildWaterAlerts keeps working). Parañaque, Muntinlupa, Bacoor rows ->  new
   `WaterStatus.Nearby []Maintenance` (for the site page only, never posted).
4. `Fetch` (section `water`): new option `MayniladFeed bool` (default true) + `MayniladFeedFile
   string` (offline: read this local XML instead, for samples/tests) + CLI flags
   `-maynilad-feed` (default true) / `-maynilad-feed-file FILE`. Blocked-feed behaviour as in the
   hard rules. The existing X inbox path stays; the old WP-REST maintenance fetch stays off by
   default.
5. Dedupe against X advisories (`BuildWaterAlerts` or a merge step, with tests): an X advisory
   (kind scheduled/emergency from the inbox) for the same city whose window overlaps a feed-sourced
   alert window (per night for daily alerts) by >= 50 % of the shorter window is dropped; the feed
   alert wins because it has barangays. Also in `fbutil`: never post a water alert whose window
   overlaps (same rule, same city) an already-posted util-water entry, found by reading the posted
   ids' frozen `post.json` Start/End (so a feed alert is not re-posted after the same event was
   posted from X, and vice-versa). Daily alerts: ONE alert per row (not per night); its post
   Start/End = first night start / last night end, with `Daily` set so text says "bawat gabi"/"every
   night"/"毎晩" with the nightly clock window and the date range; never print "from first start to
   last end" as one continuous outage.
6. Our water alert caption gets one extra trend line ("Nth water interruption in Las Piñas this
   month") using the Las Piñas rows + X advisories we know in that Manila month (count distinct
   alerts after dedupe; omit the line when the count is 1 and we have no earlier data? -> show
   it only when N >= 2, plus "last month: M" only when the snapshot really covers last month; never
   guess). Add the numbers to `CheckPost` verification.

## B. Meralco planned interruptions (maintenance schedule pages)

Source: `{MeralcoCompanyBase}/news-and-advisories/maintenance-schedule` (default base
`https://company.meralco.com.ph`). Server-rendered Drupal view, newest event date first, 2 rows per
page, `?page=N` (0-based). Filter `?field_service_maintenance_loc_target_id=ID&page=N` with ids
215 Las Piñas, 223 Parañaque, 221 Muntinlupa, 213 Cavite (Bacoor has no own id; it is inside
Cavite). Each row: `views-field-title` (link to detail page; title like "October 7, 2026 - Las
Piñas City (Almanza I And Almanza Ii)", "October 29 - November 2, 2026 - Cavite (Dasmariñas City)",
"September 29, 2026 - Las Piñas City; And Muntinlupa City"), `views-field-field-service-maintenance-loc`
(city tag), `views-field-body` (one or more `<strong>BETWEEN … – PORTION OF CIRCUIT NAME</strong>`
blocks followed by area lines), `views-field-field-reason` ("REASON: ..."). Study the fixtures in
`testdata/meralco_ms/` (city213/215/221/223 pages 0-2, all_p0..p5, layout_changed.html = a 200 page
with no rows) and `research/maintenance/meralco_maintenance_schedule.html`.

1. `ParseMeralcoMaintenance(html []byte) (MeralcoMSPage, error)` -> items:
   `PowerItem{ID, URL, Title, Tags []string (tag text), Cities []string (normalised cities matched
   from tag + title + body, accent-insensitive: Las Piñas, Parañaque, Muntinlupa, Bacoor, and keep
   others), DateFrom, DateTo (YYYY-MM-DD from the title), Blocks []PowerBlock, Reason string,
   ReasonKey}` and `PowerBlock{Circuit string, Windows []TimeWindow, AreaLines []string, Barangays
   map[string][]string (by normalised city), Roads []string}`.
   - Time windows (Manila time): "BETWEEN 11:00PM (TUE., 11/03/26) AND 4:00AM (WED., 11/04/26)"
     (dates inline), "BETWEEN 10:00AM AND 1:00PM" (date from the title), split windows "BETWEEN
     12:01AM AND 12:30AM AND THEN BETWEEN 4:30AM AND 5:00AM" (two windows), "THEN BETWEEN 11:00PM
     AND 11:30PM (THU., 08/15/24) AND THEN BETWEEN 4:30AM AND 5:00AM (FRI., 08/16/24)", with end
     <= start meaning next day. Multi-day titles ("October 29 - November 2, 2026" or "Oct 26 - 27")
     where block windows carry no dates: look at the real fixtures and model them as nightly/daily
     repeats over the title date range with `Daily` + DailyFrom/DailyTo like Maynilad (flag
     `DateAmbiguous` if unsure; ambiguous items are shown on the site with raw text and never posted).
   - Barangays: collect from lines such as "... in Bgys. Almanza I & II in Las Piñas City" /
     "Bgy. Alabang, Muntinlupa City" per city; split on "," "&" "and"; keep the order; normalise
     "Bgys."/"Bgy." prefixes away; Roman numerals kept ("Almanza I"). Roads: first 3 distinct
     road names after "Portion of" (e.g. "Alabang-Zapote Road", "Investment Drive", "Daang Hari
     Road"; ASCII hyphen). A line mentioning a city that is not the tag still assigns its barangays
     to the mentioned city.
   - Duplicate entries (slugs ending "-0", repeated Imus/Kawit pages): dedupe by (date range,
     normalised circuit+window text) keeping the first URL.
   - Stable `ID` = first 12 hex of sha1(URL slug).
2. `Fetch` section `power` (name "meralco-ms"): option `MeralcoMS bool` (default true) +
   `MeralcoMSDir string` (offline: read `all_pN.html` / `cityID_pN.html` from this directory instead
   of HTTP; samples/tests) + CLI flags `-meralco-ms` / `-meralco-ms-dir DIR`, `MeralcoMSBase` for
   httptest. Request plan (cap 24 requests/run): (a) the four city filters page 0; (b) Las Piñas
   (215) filter walked page by page until an item is older than 40 days or 8 pages (history for the
   monthly trend); (c) unfiltered list from page 0, walking until a page has only items whose
   DateTo < today (Manila) or 12 pages, keeping items with DateTo >= today and DateFrom <= today+14d
   (items beyond +14d are still parsed but marked and shown on the site only up to +21d).
   Merge/dedupe all by item ID/content; store in `Snapshot.Power *PowerStatus{CheckedAt, Items
   []PowerItem, HistoryFrom string (earliest date covered for Las Piñas), LayoutAlarm string}` (json
   omitempty). All four cities are stored for the site; only Las Piñas (tag, title, or body mention)
   is ever posted (the code has no "big-impact neighbour" notion, so Las Piñas only).
3. Layout-change alarm: a 200 response with zero parsed rows on unfiltered page 0, or rows found but
   no item with a parsable date+window across the whole run -> set `PowerStatus.LayoutAlarm` (human
   text), append to `Snapshot.Errors`, print a stderr WARNING line, keep the previous Power items in
   the snapshot (do not replace with an empty list), and make every power alert unavailable (no
   frozen pages written, nothing posted) while the alarm is set. A legitimately empty city filter
   (200, filter form present, other pages parse) is NOT an alarm.
4. `BuildPowerAlerts(p *PowerStatus, now time.Time) []PowerAlert` for Las Piñas only:
   `PowerAlert{ID, City "Las Piñas", Start, End (first/last window), Windows, Daily..., Barangays
   (Las Piñas barangays only; if none extractable, say "ilang barangay"/"some barangays"/"一部の地区"
   markers like XAreas*), Roads (<=3), ReasonKey, SourceURL (detail URL), NthThisMonth int,
   PrevMonthCount int, HistoryComplete bool}`. Trend: N = number of distinct Las Piñas planned
   interruption items (distinct DateFrom, after dedupe) whose Start falls in the same Manila month
   as this alert's Start and whose Start <= this alert's Start (so it is the Nth); PrevMonthCount =
   the count in the previous month, only shown when HistoryComplete (the Las Piñas history walk
   reached the start of the previous month). Reason keys (our own wording TL/EN/JA): reconductoring/
   reconstruction/pole replacement -> "pagpapalit/pagkukumpuni ng linya at poste"/"line and pole
   works"/"電線・電柱工事"; substation maintenance -> "maintenance ng substation"/"substation
   maintenance"/"変電所メンテナンス"; installation of facilities -> "pagkakabit ng pasilidad"/
   "facility installation"/"設備の設置"; tree trimming -> "pagputol ng sanga"/"tree trimming"/
   "樹木の伐採"; unknown -> "maintenance ng Meralco"/"Meralco maintenance"/"Meralco作業". Never
   copy their sentences.

## C. Posts, images, site, posting rules

- New post kind `power` mirroring `water`: `RenderPowerAlertPost(a PowerAlert, pastSample bool)
  string`, `RenderPowerAlert(a, pastSample) image` (1080x1350 PNG, same style/footer as
  RenderWaterAlert: title "Abiso sa kuryente", big date + window(s), barangays, roads, reason,
  trend chip, footer "Source: Meralco maintenance schedule" / "Hindi opisyal na Meralco page"),
  `CheckPost(kind "power")` (verifies every number/date/time/barangay/road/trend in the caption
  and chart against the alert). Caption is trilingual TL / EN / JA with the existing separators,
  no nested parentheses, no markdown, own wording, one link line to the official detail page
  ("Details: <detail URL>") and the "Source: Meralco maintenance schedule · Hindi opisyal na
  Meralco page" line. Example shape (TL block; EN/JA equivalents):
  ```
  ⚡ Walang kuryente sa ilang bahagi ng Las Piñas
  📅 Miy Okt 7, 12:01 AM hanggang 12:30 AM, at 4:30 AM hanggang 5:00 AM
  📍 Almanza I, Almanza II · Alabang-Zapote Rd, Investment Dr
  🔧 Dahilan: maintenance ng substation
  📊 Ika-2 na planadong brownout sa Las Piñas ngayong Oktubre, 1 noong Setyembre
  Ihanda ang gadgets at flashlight.
  ```
  Daily items say "bawat gabi"/"every night"/"毎晩" with the clock windows and the date range.
  With pastSample the same "⚠️ SAMPLE — lumang datos / past data" first line and a red banner on
  the image as for water.
- Frozen pages `u/power/<id>/{index.html,post.json,power.png}` (same PostDoc fields as water,
  AlertKind "power", Start/End, AsOf=id), written by `utilpages`; site `u/index.html` gets a new
  section "Kuryente / Power / 電気" (Las Piñas planned interruptions now+upcoming, each with the
  official link and the trend; "none, checked <time>" otherwise; plus a compact list of nearby
  Parañaque/Muntinlupa/Bacoor items for the next 14 days) and the water section gets the weekly
  schedule rows for Las Piñas plus the nearby list (Parañaque/Muntinlupa/Bacoor) from
  `Water.Nearby`. A layout alarm is NOT shown publicly beyond "no data right now"; it is in the
  snapshot/logs only. Keep GA4/OG/canonical/lang-toggle/privacy line style. `data/utilities.json`
  gets the latest power ids (additive keys only).
- `fbutil`: accept `-kind power`; `-auto` priority water > power > fuel > bills (still ONE post per
  run); power due when a frozen page exists, check=pass, alert Start in [now+3h, now+72h], no
  state entry {region:"util-power", as_of:id}, and no util-power entry posted on the same Manila
  day; same window [12:00,19:00) Manila, Sunday 16:00-20:00 hold, disable file, global 2h min-gap,
  publish-delay, DA-first (power exempt like water). No posting when the snapshot has a layout
  alarm (frozen pages are simply not generated then). Tests (table-driven, `-now`, temp state,
  httptest Graph): priority order, 2h gap, one power/day, already posted, too soon (<3h), too far
  (>72h), cross-source overlap with a posted water window is NOT relevant for power.
- `scripts/utilities.sh`: section `power` when the Manila hour is divisible by 6 (06/12/18) and
  always include `water` as today (the feed fetch happens inside `water`); keep everything else.
  Update `scripts/utilities_test.sh`. `deploy.sh` unchanged except nothing needs adding (no new
  binary). Do not touch crontab.
- `presyo utilities render -kind power [-alert-id ID] [-past-sample]` (+ existing flags to select a
  snapshot) writes `power-<id>.txt/.png/.check.json` like water; with the offline flags the samples
  can be built from fixtures (`-meralco-ms-dir`, `-maynilad-feed-file`, `-now`).
- Docs: add this file's behaviour summary to go/README.md (utilities section), keep
  UTILITIES_MAYNILAD_X.md current (the feed is now the primary source for weekly schedules).
  Update `testdata/SOURCES.md`.

## D. Tests (fixtures only)
Parser tests with exact expectations taken from the fixtures (list them in the test names):
feed p3 (post 2026-08-21: Las Piñas Almanza Uno, Pilar, Talon Uno, Talon Dos, Talon Singko,
10:00 p.m. of August 26 to 1:00 p.m. of August 27, 2026, Facility Maintenance); feed p8 (2026-03-20:
Las Piñas Pamplona Uno, Pulang Lupa Uno and Pulang Lupa Dos, 10:00 p.m. of March 24 to 6:00 a.m. of
March 25, 2026; Caloocan daily 11:01 p.m. to 3:00 a.m. daily March 22-29 = 8 nights; Manila rowspan
rows); feed p1 (Oct 2026 posts: Muntinlupa Tunasan; Imus row that also lists Bacoor); Meralco
Las Piñas Oct 7 2026 (two split windows 12:01AM-12:30AM and 4:30AM-5:00AM; Almanza I & II; reason
"Maintenance works inside Meralco - Ayala Alabang substation"), Sep 29 2026 (Las Piñas and
Muntinlupa), the Muntinlupa Oct 29 2026 entry, the Cavite Bacoor entries, the inline-dates
Bulacan Nov 3-4 entry, the layout_changed fixture (alarm), dedupe against X advisories, trend
counts, fbutil auto rules, caption checker, image bounds/determinism, utilpages output, httptest
fetch tests for both sources including the blocked-feed (403 + cf-mitigated header) fallback and the
Meralco 200-but-empty alarm.
