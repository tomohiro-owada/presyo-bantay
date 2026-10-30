# Utilities v2 — spec for opencode (extends UTILITIES_SPEC.md; same hard rules)

Hard rules (unchanged + additions): no git state changes; tests never touch
the network (fixtures in internal/utilities/testdata only, incl. the new
`maynilad_fcda_posts.json`); no new deps; existing daily/weekly outputs
byte-identical; never read secret/env files; never contact Facebook (Graph
calls only against httptest). Never invent numbers: if a previous-period
value is missing, the text says so ("walang datos ng nakaraang …" /
"previous … not available" / "前回データなし") instead of guessing.
Every number in captions must be verified by `CheckPost` (extend it for all
new derivations below). Use U+2212 for minus.

GENERAL RULE (user requirement): every utility post, image and site page
ALWAYS shows the trend vs the previous period (people do not remember last
values): fuel = this week's AND last week's adjustment + 6-week mini chart +
pump prices now vs previous week; bills = Meralco old → new, LPG old → new,
water rate old → new, family total old → new. Water alerts: event only.

## PART A — data, text, checker, charts

### A1. Pump prices for nearby cities (Las Piñas is NOT monitored by DOE)
Extend `ParseNCRPumpSummary` result with `Cities []CityPump` parsed from the
per-city table: `CityPump{City string; Prices []CityPrice}` and
`CityPrice{Product string; Low, High float64; Common *float64}` (product keys
RON100, RON97, RON95, RON91, Diesel, Diesel Plus, Kerosene; "#N/A" → nil).
Layout: blocks of up to 7 product rows (RON 100 … KEROSENE); the city label
sits on ONE of the block's product rows (Parañaque on the RON 100 row,
Muntinlupa on the RON 91 row); page 2 also has a stray standalone "Caloocan
City" line inside Muntinlupa's block — a label on a product row wins over a
standalone label line. Each row ends with "low - high common" (common may
be #N/A). Only Parañaque and Muntinlupa are required (parse all if easy).
Expected (tests):
- ncr_pump_1013.pdf Parañaque: RON95 82.10–99.36 common 90.50; RON91
  81.10–96.36 common 89.50; Diesel 89.50–96.20 common 91.80; Diesel Plus
  93.70–99.90 common 94.80; Kerosene 138.50–138.50 common nil.
  Muntinlupa: RON95 86.70–102.40 common 91.20; RON91 85.75–98.07 common
  91.00; Diesel 89.90–94.20 common 90.90; Diesel Plus 93.90–95.60 common
  95.60; Kerosene 130.00–131.24 common nil.
- ncr_pump_1006.pdf Parañaque: RON95 82.90–97.80 common 91.30; RON91
  81.80–94.80 common 90.30; Diesel 92.50–98.32 common 94.80.
  Muntinlupa: RON95 85.20–101.80 common 89.70; RON91 84.25–96.50 common
  88.70; Diesel 91.93–97.20 common 93.90.

### A2. Maynilad water rate (FCDA notices)
`ParseFCDAPosts(body []byte) ([]WaterRate, error)` for WP REST
`/wp-json/wp/v2/posts?search=FCDA&per_page=6&_fields=id,date,link,title,content`.
Text: "FCDA equivalent to negative 0.02% of the Average Basic Charge of P
52.86/cu. m. or an average rebate of P 0.01/cu.m. … effective … on 01 October
2026" (₱ or P; "negative" → sign; "rebate" negative, "increase"/"additional"
positive). `WaterRate{Effective "2026-10-01", AvgBasic 52.86, FCDAPct -0.02,
FCDA -0.01 (₱/cu.m, signed), Rate = AvgBasic+FCDA (52.85), PostURL, Posted}`.
Expected from fixture: 2026-10-01 → 52.86, −0.02 %, −0.01, rate 52.85;
2026-07-01 → 52.86, −0.47 %, −0.25, rate 52.61; 2026-04-01 → 52.86, −0.24 %,
−0.13, rate 52.73. Newest first. `WaterRateFor(month "2026-10")` = the
newest notice with Effective <= first day of that month.
Add to Snapshot: `WaterRates []WaterRate` and fetch it (Maynilad host,
MinDelay applies).

### A3. Fuel caption + chart (trend)
Caption adds (TL/EN/JA each):
- last week's adjustment line: "Noong nakaraang linggo (Okt 13–19):
  gasolina +₱1.50 hanggang +₱1.56 · diesel −₱3.00 hanggang −₱3.02 · kerosene
  +₱1.70 hanggang +₱1.77" (EN "Last week (Oct 13–19): …", JA "先週（10/13〜10/19）：…").
  If Adjustments[1] missing: say previous week not available.
- 6-week cumulative line over the Adjustments shown in the chart (signed
  sums of per-week minima and maxima, where a week's min/max are the signed
  min/max of Lo and Hi): "Kabuuan sa huling 6 na linggo (Set 15–Okt 26):
  gasolina +₱8.40 hanggang +₱9.53 · diesel +₱3.08 hanggang +₱3.97"
  (single value when min sum == max sum). Expected for the fixtures:
  gasoline 8.40..9.53, diesel 3.08..3.97, kerosene omitted from this line.
- pump prices, NOW vs PREVIOUS WEEK, for "NCR (common)", "Parañaque" and
  "Muntinlupa" (label the cities as nearby: TL "Malapit sa Las Piñas", EN
  "Nearby cities (Las Piñas is not in DOE's monitoring)", JA
  "近隣市（ラスピニャスはDOE調査対象外）"), products RON 91, RON 95, Diesel:
  "RON 91 ₱87.90 (−₱0.40 vs Okt 6–12)" — NO nested parens anywhere: use
  "RON 91 ₱87.90, −₱0.40 vs Okt 6–12" style or a separate line. Use the
  newest pump week (which ends before this week's adjustment, say so:
  "bago ang galaw ngayong linggo") and the one before it. If a city's
  common price is nil for a product, write "walang common price"/"no
  common price"/"一般価格なし" — never substitute the range.
  Expected deltas 1013 vs 1006: NCR RON91 87.90 vs 88.30 (−0.40), RON95
  88.90 vs 88.40 (+0.50), Diesel 91.40 vs 93.70 (−2.30); Parañaque RON91
  89.50 vs 90.30 (−0.80), RON95 90.50 vs 91.30 (−0.80), Diesel 91.80 vs
  94.80 (−3.00); Muntinlupa RON91 91.00 vs 88.70 (+2.30), RON95 91.20 vs
  89.70 (+1.50), Diesel 90.90 vs 93.90 (−3.00).
Chart `RenderFuelWeekly` (1080x1350, keep style): main bars show this week
and, as a small muted text on each row, last week's value ("nakaraan:
+₱1.50–1.56"); keep the 6-week history mini chart (legend kept); replace
the 3 NCR chips with a compact table: rows NCR / Parañaque / Muntinlupa,
columns RON 91 / RON 95 / Diesel, each cell "₱87.90" plus a small signed
delta chip "−0.40" (green down / red up / grey 0 or n/a); a caption line
above the table "Pump price DOE Okt 13–19 vs Okt 6–12 · Parañaque at
Muntinlupa: malapit sa Las Piñas". Everything must fit above the footer
(shrink history rows if needed); keep bounds tests.

### A4. Bills caption + chart (old → new, family total)
Month M = Meralco[0].Month. Lines (TL shown; EN/JA equivalents):
- "⚡ Meralco: ₱14.7424 → ₱15.0615/kWh, +₱0.3191 vs Setyembre" + "Sa 200
  kWh: mga +₱64" (Impact200 as stated by Meralco).
- "🔥 LPG 11 kg NCR, karaniwang presyo (DOE): ₱1,341 → ₱1,430, +₱89 vs
  Set 1–7" + "Sa Las Piñas: ₱1,368–₱1,569, Set: ₱1,185–₱1,393".
- "💧 Tubig Maynilad, average basic charge + FCDA: ₱52.61 → ₱52.85/cu.m,
  +₱0.24 mula Okt 1" (water rate for M vs M−1 via WaterRateFor; when equal:
  "walang pagbabago ngayong buwan"). Plus the Las Piñas interruption
  status line as before.
- FAMILY TOTAL "👪 Tantiyang buwanang gastos ng isang pamilya:" with an
  explicit assumption line, all amounts with 2 decimals so the sums add up
  exactly:
  current = 200 × Meralco[0].Rate + 1 × LPG[0].Common + 20 × water rate(M)
  previous = 200 × Meralco[0].PrevRate + 1 × LPG[1].Common + 20 × water rate(M−1)
  Fixture/real expected: current 3,012.30 + 1,430.00 + 1,057.00 = 5,499.30;
  previous 2,948.48 + 1,341.00 + 1,052.20 = 5,341.68; change +157.62 =
  +63.82 (kuryente) +89.00 (LPG) +4.80 (tubig).
  Text: "👪 Tantiyang gastos/buwan: ₱5,341.68 → ₱5,499.30, +₱157.62 vs
  Setyembre · kuryente +₱63.82, LPG +₱89.00, tubig +₱4.80" and
  "Palagay: 200 kWh kuryente sa typical-household rate ng Meralco · 1 tangke
  LPG 11 kg sa NCR common price ng DOE · 20 cu.m tubig sa average basic
  charge + FCDA ng Maynilad, hindi kasama ang ibang singil at VAT ng tubig ·
  hindi kasama ang gasolina" (EN: "Assumptions: 200 kWh at Meralco's
  typical-household rate · one 11-kg LPG tank at DOE's NCR common price ·
  20 cu.m water at Maynilad's average basic charge + FCDA, excluding other
  water charges and VAT · fuel not included"; JA equivalent). If any
  component's previous value is missing, show only the current total and
  say the previous total is not available (no partial comparison).
  Note the 200 kWh impact line uses Meralco's own rounded figure (+₱64)
  while the family total uses the exact 200 × ΔRate (+₱63.82); both are
  allowed derivations in the checker.
Chart `RenderBills`: 4 cards — Meralco (old → new, chip "+₱0.3191 vs Set"),
LPG (old → new, chip "+₱89 vs Set 1–7" — the chip MUST include the
comparison period), Water (old → new ₱/cu.m, chip "+₱0.24 mula Okt 1",
plus Las Piñas interruption status), Family total (old → new, chip
"+₱157.62", one-line breakdown and a short assumption line). Fit/ellipsize,
bounds tests, deterministic.

### A5. Water alert (event post)
`BuildWaterAlerts(w *WaterStatus) []WaterAlert` from Scheduled, Emergency,
Rotational and Maintenance rows: `WaterAlert{ID (stable sha1 of kind+city+
barangays+start+end, 12 hex), Kind, City, Areas []string (barangay names
split on ",", " and "), Start, End time.Time (Asia/Manila) when parseable,
WindowText, ReasonKey, SourceURL}`. Parse "10:00 p.m. of August 26, 2026 to
1:00 p.m. of August 27, 2026" and the interruptions From/To/Time columns.
ReasonKey maps known phrases to OUR OWN wording (never copy their sentences):
"Facility Maintenance" → maintenance ng pasilidad / facility maintenance /
設備メンテナンス; "Network Maintenance" → maintenance ng linya ng tubig /
pipe network maintenance / 配管網メンテナンス; "Network Repair Activity" →
pagkukumpuni ng linya / pipe repair / 配管修理; "Network and Facility
Limitations" → limitadong suplay / limited supply capacity / 供給能力の制約;
unknown → generic "trabaho ng Maynilad" / "Maynilad work" / "Maynilad作業".
Do not include Maynilad's location text; barangay names and times are facts.
`RenderWaterAlertPost(a WaterAlert, pastSample bool) string`, short, 3
languages:
```
💧 Walang tubig sa ilang bahagi ng Las Piñas
📅 Miy Ago 26, 10:00 PM hanggang Huw Ago 27, 1:00 PM
📍 Almanza Uno, Pilar, Talon Uno, Talon Dos, Talon Singko
🔧 Dahilan: maintenance ng pasilidad
Mag-ipon ng tubig bago magsimula.
━━━━━━━━━━
🇬🇧 Water interruption in parts of Las Piñas
Wed Aug 26, 10:00 PM to Thu Aug 27, 1:00 PM · Almanza Uno, Pilar, Talon Uno, Talon Dos, Talon Singko · Reason: facility maintenance. Store water beforehand.
━━━━━━━━━━
🇯🇵 ラスピニャスの一部で断水
8/26（水）22:00〜8/27（木）13:00・Almanza Uno、Pilar、Talon Uno、Talon Dos、Talon Singko・理由：設備メンテナンス。事前に水の確保を。
━━━━━━━━━━
Details: https://www.mayniladwater.com.ph/service-advisories-2/ (+ the post link for maintenance items)
Source: Maynilad advisory · Hindi opisyal na Maynilad page
```
(Check the actual weekdays with Go's time package; Aug 26 2026 must be
computed, not assumed.) When `pastSample` is true, the FIRST line of the
caption is "⚠️ SAMPLE — lumang datos / past data, hindi kasalukuyang abiso"
and the image gets a red banner "PAST DATA SAMPLE — not a current
advisory". Chart `RenderWaterAlert(a, pastSample)` 1080x1350, same style:
title "Abiso sa tubig", big date/time window, areas list (wrap), reason,
"Mag-ipon ng tubig", footer "Source: Maynilad advisory" / "Hindi opisyal na
Maynilad page". CheckPost(kind "water") verifies dates/times/areas come
from the alert. An emergency X advisory with no parsable end time is kept
open-ended (`OpenEnded`, `End` zero, `Start` = post `created_at`, due only
within 24h of the post): the caption says "hanggang may abiso" /
"until further notice" / "追って通知があるまで" instead of an end time,
and the frozen `post.json` carries `open_ended` (omitempty).

### A6. CLI
`presyo utilities render -kind fuel|bills|water ...`; for water:
`-alert-id ID` (default first) and `-maintenance-json FILE -city "Las Piñas"
-past-sample` to build a water-only snapshot offline from a WP posts JSON
file (used for the sample). Output names for water: `water-<id>.txt/.png/
.check.json`, and with -past-sample the prefix `water-alert_PAST-SAMPLE_
<YYYY-MM-DD of start>`. `presyo utilities fetch -sections
fuel,pump,lpg,meralco,water,fcda` (default all) MERGES into an existing
`-out` snapshot (keep sections not fetched this run; record per-section
fetched_at).

## PART B — site pages, posting integration, scripts (all dry-run/local)

### B1. `presyo utilpages -snapshot FILE -site-public DIR [-force] [-base-url URL] [-now RFC3339]`
Static, deterministic HTML like weekpages (reuse its template style: GA4
snippet identical, OG/Twitter absolute og:image, canonical, pb-lang toggle,
privacy line, logo header). Writes:
- `u/index.html` "Bayarin / Utilities / 光熱費": latest fuel (this + last
  week + 6-week list), NCR/Parañaque/Muntinlupa pump table now vs previous
  week, bills (old → new for Meralco, LPG, water, family total + assumptions),
  Las Piñas water interruptions (current list or "none, checked <time>"),
  sources with links + checked times, links to frozen posts.
- FROZEN per-post pages (never re-rendered without -force when post.json
  exists): `u/fuel/<EffectiveFrom>/`, `u/bills/<YYYY-MM>/`,
  `u/water/<alert id>/`, each `{index.html, post.json (text, kind, as_of,
  generated_at, check), <kind>.png}` — the SAME text/PNG used for FB.
  Water pages must show our own wording and the Maynilad advisory link.
- `data/utilities.json` {latest fuel/bills/water ids+paths, generated_at}.
- Day archive / top page: NOT modified in this prototype (note as TODO).

### B2. `presyo fbutil -db DB -site-public DIR [-auto] [-kind fuel|bills|water|power] [-id ID] [-dry-run] [-state] [-env-file] [-disable-file] [-min-gap 2h] [-gap-grace 5m] [-window-start 05:30] [-window-end 20:00] [-routine-window-start 12:00] [-routine-window-end 19:00] [-publish-delay 10m] [-bills-max-wait 24h] [-graph-base] [-now RFC3339]`
Reuse fbpost/fbweekly helpers (state file, withStateLock, postPhoto, token
redaction, env file). Posts ONLY frozen pages (FB == site); `-auto` requires
the page's post.json generated_at to be ≥ publish-delay old (GitHub Pages
deploy time). `-auto` decision (at most ONE post per run, priority water >
power > fuel > DA-first gate > bills, a stale bills page bypasses the
gate), all under the state flock:
- windows: water/power [05:30, 20:00) Manila (-window-start/end);
  fuel/bills routine [12:00, 19:00) Manila
  (-routine-window-start/end, evaluated only inside it, otherwise
  skipped to "nothing due"); Sunday 16:00–20:00 hold (skip all);
  disable file; global min-gap over ALL entries (DA, weekly, util) ≥ 2h
  minus -gap-grace.
- DA FIRST (fuel only; water/power exempt; bills bypass when the frozen
  page is ≥ -bills-max-wait old): if the DB's latest price date D is ≤ 2
  days old (same rule as fbpost) and not every region in regions.IDs()
  has a state entry for D, skip ("waiting for DA posts"). Water and power
  alerts are exempt from DA-first but obey the main window/gap/hold.
- fuel: page u/fuel/<from>/ exists (latest page only, id =
  effective_from date), <from> is 0–6 days old, no state entry
  {region:"util-fuel", as_of:<from>}; any weekday; only when the
  Manila time of day is inside the routine window.
- bills: page u/bills/<M>/ exists for current Manila month M, no entry
  {region:"util-bills", as_of:M}; only when the Manila time of day is
  inside the routine window (including the starvation bypass).
- water: page u/water/<id>/ exists, the alert has not yet ended and its
  start is within 72h ahead (open-ended emergencies while posted within
  the last 24h), no entry {region:"util-water", as_of:<id>}, and no
  util-water entry posted on the same Manila day; main window only.
- power: page u/power/<id>/ exists, check=pass, the alert has not yet
  ended and its start is within 72h ahead, no entry
  {region:"util-power", as_of:<id>}, and no util-power entry posted on
  the same Manila day; main window only.
- `-dry-run` never posts, never writes state, never needs a token; prints
  the decision and writes the frozen text/png copy to an out dir.
Existing `fbpost -auto` / `fbweekly -auto` behaviour must stay unchanged
(their gap checks already count every state entry; add a regression test
that a util entry blocks fbpost within 2h and does not fill a region slot).
Table-driven tests with -now and temp state: outside window, Sunday hold,
DA pending, DA done → fuel, gap, water exempt from DA-first, 1 water/day,
already posted, page too fresh, disabled; Graph via httptest only.

### B3. Scripts (NOT deployed; only files + tests)
`scripts/utilities.sh` (same structure as weekly.sh: PRESYO_HOME layout,
path safety, flock on $PRESYO_HOME/.utilities.lock, log to
logs/utilities.log, respects fbpost.disabled for the FB step):
1. decide sections by Manila time: fuel+pump hourly on Tue/Wed, else once
   a day (first run after 09:00); meralco daily on days 5–20; lpg daily on
   days 1–15; water + fcda every run whose Manila hour is divisible by 3
   (and fcda on days 10–20 only).
2. `$BIN utilities fetch -sections … -cache $PRESYO_HOME/data/utilities/cache -out $PRESYO_HOME/data/utilities/snapshot.json`
3. `$BIN utilpages -snapshot … -site-public $REPO/site/public`
4. if git status shows changes under site/public/u or
   site/public/data/utilities.json: take $PRESYO_HOME/.daily.lock (flock -w
   300, shared with daily.sh), commit "utilities: <Manila date time>",
   pull --rebase, push (one retry) — exactly like daily.sh.
5. `$BIN fbutil -db $DB -site-public $REPO/site/public -auto` unless
   disabled; PRESYO_UTIL_DRY_RUN=1 adds -dry-run.
`scripts/utilities_test.sh` with a fake BIN/git like the other *_test.sh.
`deploy.sh`: also install utilities.sh (hourly at :20, 06:20-19:20 Manila; the
exact crontab is kept in private ops notes). deploy.sh must still never touch crontab. Do NOT run deploy.sh.
Also make `daily.sh` add `site/public/u` and `site/public/data/utilities.json`
to its commit paths only if they exist (update daily_test.sh).

### B4. Docs
Update go/README.md utilities section (commands, posting rules, cron line,
water wording policy: own words + link, no copied text/images).

### B5. Maynilad water via X (offline inbox)
The website is Cloudflare-challenged, so water advisories come from
@maynilad via a pushed inbox file instead of the site fetch. See
`go/UTILITIES_MAYNILAD_X.md` for the v1 inbox format, the normalizer /
push tooling, and the Go fetch behaviour (`-maynilad-x`, `-maynilad-site`,
`WaterStatus.Advisories`, `XAdvisory`, `XAreasLabel`).
