# Household utilities prototype (fuel / LPG / Meralco / Maynilad) — spec for opencode

Goal: a PROTOTYPE that fetches + parses official public sources for household
utility costs, and renders sample Facebook-style post images + trilingual
captions locally (dry run). NO Facebook code, NO posting, NO cron/VPS changes.

## Hard rules
- No git commands that change state (no add/commit/checkout/reset/stash).
- Tests must NEVER touch the network: use only the real saved fixtures in
  `internal/utilities/testdata/` (fetched 2026-10-26 from the official sites;
  do not edit or regenerate them). Do not write to /tmp outside t.TempDir().
- Do not touch `data/`, `site/`, `scripts/`, `.github/`, existing commands'
  behaviour, or the sibling project. Never read secret/env files.
- Static Go (CGO_ENABLED=0), NO new module dependencies. PDF text must come
  from the existing pure-Go `internal/pdftext` (no poppler / pdftotext).
- Never invent numbers: every number in text/charts comes from parsed data or
  a documented derivation below. If a field is missing, omit that line.
- gofmt, `go vet ./...`, `go test ./...` must pass (all existing tests too).
- Existing daily/weekly outputs (fbregion text, fbchart Render/RenderWeekly
  pixels, daypages, weekpages) must stay byte-identical. Only ADD code.

## 1. Package `internal/utilities`

### 1.1 PDF lines (`pdflines.go`)
`PDFLines(path string) ([]Line, error)`: for every page, group
`pdftext.Page.Chars` into visual lines (same page, |Top| within ~2.5 pt),
sort chars by X0, build `Line{Page int; Top float64; Words []Word; Text string}`
where a new Word starts when the horizontal gap > ~0.25 * char size (and a
space char ends a word). `Text` = words joined with single spaces. Lines in
reading order (page, Top, X). Must work on all PDFs in testdata (verify; if a
font decodes oddly, fix in this layer, not in pdftext, unless it is a clear
pdftext bug — then fix minimally and keep DA parity tests green).

### 1.2 DOE attachment pages (`doe.go`)
`ParseAttachments(html []byte, base string) []Attachment{Title, URL string}`
from `<a href="https://d24qbtp4vooyzi.cloudfront.net/api/media/file/...pdf?...">TITLE</a>`
(document order, HTML-unescaped title, whitespace collapsed, dedupe by URL
path ignoring the query string; keep the first URL seen). Fixtures:
`doe_oil_monitor_page.html` (first = "Oil Monitor 20 October"),
`doe_ncr_pump_page.html` (titles like "October 13 to 19"; NOT in date order —
callers must sort by the date parsed from the PDF itself),
`doe_ncr_lpg_page.html` ("October 1 to 8", "September 1 to 7"),
`doe_price_adjustments_page.html`.

### 1.3 Oil Monitor (weekly adjustment) `ParseOilMonitor(lines []Line) (OilAdjustment, error)`
Section "DOMESTIC OIL PRICES". Text is free-form with typos, e.g.
"increased by: • Php 1.90/L to Php 1.93L/ for gasoline", "Php 4.60/LPhp 4.62/L
for kerosene", and a "Meanwhile, ... diesel decreased by Php 3.00/L to Php 3.02/L."
sentence. Output:
```go
type Range struct{ Lo, Hi float64 } // signed pesos/L; Lo = smaller magnitude, Hi = larger magnitude (same sign)
type OilAdjustment struct {
  AsOf string            // "2026-10-20" from "As of 20 October 2026" (or header)
  EffectiveFrom, EffectiveTo string // from "effective 20 through 26 October 2026" / "29 September through 05 October 2026"
  MovementNo int         // "The 42nd price movement this year"
  Gasoline, Diesel, Kerosene *Range // nil when not mentioned
  SourceURL string
}
```
Expected (tests, all fixtures):
| file | from..to | no | gasoline | diesel | kerosene |
|---|---|---|---|---|---|
| oilmon_20oct.pdf | 2026-10-20..2026-10-26 | 42 | −4.30 | +2.60 | −1.00 |
| oilmon_13oct.pdf | 2026-10-13..2026-10-19 | 41 | +1.50..+1.56 | −3.00..−3.02 | +1.70..+1.77 |
| oilmon_06oct.pdf | 2026-10-06..2026-10-12 | 40 | +1.90..+1.93 | −1.30 | +3.30 |
| oilmon_29sep.pdf | 2026-09-29..2026-10-05 | 39 | −0.24..−0.50 | −7.55..−8.00 | −5.85..−5.90 |
| oilmon_22sep.pdf | 2026-09-22..2026-09-29 | 38 | +4.80..+4.88 | +8.80..+8.82 | +6.40..+6.47 |
| oilmon_15sep.pdf | 2026-09-15..2026-09-21 | 37 | +5.00..+5.70 | +4.00..+4.40 | +4.60..+4.62 |
(single value ⇒ Lo == Hi). Sanity: |value| < 30, else error.

### 1.4 NCR pump price summary `ParseNCRPumpSummary(lines) (PumpSummary, error)`
Block "PREVAILING RETAIL PRICES OF PETROLEUM PRODUCTS NCR (for the week of
October 13-19, 2026)" with rows `Product Low High Common`:
```go
type PumpPrice struct{ Product string; Low, High, Common float64 } // Product keys: "RON97/100","RON95","RON91","Diesel","Diesel Plus","Kerosene"
type PumpSummary struct{ WeekFrom, WeekTo string; Prices []PumpPrice; SourceURL string }
```
Expected ncr_pump_1013.pdf (2026-10-13..2026-10-19): RON97/100 84.83 105.40 98.80;
RON95 80.00 102.80 88.90; RON91 79.20 98.07 87.90; Diesel 85.70 96.20 91.40;
Diesel Plus 90.70 100.17 95.60; Kerosene 115.00 146.34 124.40.
ncr_pump_1006.pdf (week as printed in the PDF, expect 2026-10-06..2026-10-12):
84.28 105.90 97.30; 79.60 101.80 88.40; 78.60 96.50 88.30; 83.00 99.40 93.70;
90.24 103.17 98.60; 115.00 144.57 122.70.
(Note: Las Piñas is NOT a monitored city in this PDF; do not fabricate it.)

### 1.5 NCR LPG monthly `ParseNCRLPG(lines) (LPGMonth, error)`
```go
type LPGMonth struct {
  PeriodFrom, PeriodTo string // "(For October 01-08, 2026)" -> 2026-10-01..2026-10-08
  Low, High, Common float64   // "11.0 kg 1,268.00 - 1,710.00 1,430.00"
  AutoLPG float64             // "Auto-LPG 58.40 58.40" -> 58.40 (0 if absent)
  City string                 // "Las Pinas City" row (match "Las Pi" case-insensitive)
  CityLow, CityHigh float64   // the "a - b" range near the END of that row
  CityCommon *float64         // value after the range; "#N/A" -> nil
  SourceURL string
}
```
Expected ncr_lpg_oct.pdf: 2026-10-01..2026-10-08, 1268/1710/1430, auto 58.40,
Las Piñas 1368–1569, common nil. ncr_lpg_sep.pdf: 2026-09-01..2026-09-07,
1025/1535/1341, Las Piñas 1185–1393, common nil.

### 1.6 Meralco (`meralco.go`)
- `ParseMeralcoListing(html) []string`: absolute URLs of
  `https://company.meralco.com.ph/news-and-advisories/<slug>` where slug
  matches `rates-(january|...|december)-\d{4}$` (e.g.
  higher-residential-rates-october-2026, lower-rates-september-2026,
  lower-rates-august-2026), dedup, newest month first.
- `ParseMeralcoRelease(html) (MeralcoRate, error)` on visible text (strip
  script/style/tags, unescape):
```go
type MeralcoRate struct {
  Month string           // "2026-10" (from slug or title "this October 2026")
  Announced string       // "2026-10-13" from "MANILA, PHILIPPINES, 13 OCTOBER 2026"
  Title string
  Adjustment float64     // signed P/kWh, e.g. +0.3191 / -0.0409
  Rate, PrevRate float64 // "overall rate for a typical household to P15.0615 from P14.7424 per kWh"
                         // also "to P14.7424 this month from P14.7833 per kWh"
  Impact200 int          // signed pesos, "consuming 200 kWh, ... increase of around P64" / "reduction of around P8"
  SourceURL string
}
```
Validation: round(Rate-PrevRate, 4) == Adjustment (sign from increase/
decrease wording must agree), and |Adjustment*200 - |Impact200|| <= 1.
Expected: meralco_2026_10.html → 2026-10, 2026-10-13, +0.3191, 15.0615,
14.7424, +64. meralco_2026_09.html → 2026-09, 2026-09-11, −0.0409, 14.7424,
14.7833, −8. Listing fixture `meralco_news_page.html`.

### 1.7 Maynilad (`maynilad.go`)
- `ParseInterruptions(body []byte, kind string) ([]Interruption, error)`:
  body is a JSON string containing an HTML table (endpoint
  `/wp-json/interruptions/{scheduled|emergency|rotational}?id=&city=<city>`).
  Columns: City, Barangay, Specific Area, From, To, Time, Reason (rotational
  has no To). `Interruption{Kind, City, Barangays, Area, From, To, Time, Reason string}`
  (From/To normalized to YYYY-MM-DD when parseable, raw kept in `FromRaw/ToRaw`).
  Fixtures: maynilad_scheduled_laspinas_empty.json → 0 rows;
  maynilad_scheduled_imus.json → ≥1 row, first City "Imus City", Reason
  "Network and Facility Limitations", From 2026-10-01, To 2026-10-31,
  Time "5:00 AM to 11:00 PM"; maynilad_emergency_qc.json → first City
  "Quezon City", Barangays "Brgys. Sauyo and Talipapa".
- `ParseMaintenancePosts(body []byte, cityPrefix string) ([]Maintenance, error)`
  for WP REST `/wp-json/wp/v2/posts?search=maintenance%20activities&_fields=id,date,link,title,content`:
  only posts titled "Maintenance activities for the upcoming week"; parse the
  table rows (City, Barangay, Schedule, Activity, Location). Rows that omit
  the City cell (rowspan) inherit the previous City. Return rows whose City
  starts with cityPrefix (accent-insensitive: "Las Pinas" == "Las Piñas").
  `Maintenance{PostDate, PostURL, City, Barangays, Schedule, Activity, Location}`.
  Fixture maynilad_maintenance_posts.json with "Las Pi": exactly 1 row
  (post 2026-08-21): Barangays "Almanza Uno, Pilar, Talon Uno, Talon Dos and
  Talon Singko", Schedule "10:00 p.m. of August 26, 2026 to 1:00 p.m. of
  August 27, 2026", Activity "Facility Maintenance", Location "Marcos Alvarez
  Pumping Station and Magdiwang Pumping Station and Reservoir". With
  "Caloocan": 2 rows from the 2026-10-23 post (second inherits the city).

### 1.8 Snapshot + fetch (`fetch.go`, `snapshot.go`)
```go
type Snapshot struct {
  FetchedAt string // RFC3339, Asia/Manila
  Fuel struct{ Adjustments []OilAdjustment; NCRPump []PumpSummary } // newest first
  LPG []LPGMonth                 // newest first
  Meralco []MeralcoRate          // newest first
  Water *WaterStatus             // City, CheckedAt, Scheduled, Emergency, Rotational []Interruption, Maintenance []Maintenance
  Sources []Source               // {Name, URL, FetchedAt, HTTPStatus, Bytes, SHA256, Err}
  Errors []string                // per-source failures; partial snapshots are OK
}
```
`Fetch(ctx, opts FetchOptions) (*Snapshot, error)` with `FetchOptions{Client *http.Client;
CacheDir string; Now time.Time; MinDelay time.Duration (default 5s per host);
MaxOilMonitors int (default 6); MaxRequests int (default 30); City string ("Las Piñas")}`:
1. DOE oil-monitor page → newest MaxOilMonitors PDFs (cache in CacheDir by
   sanitized file name; cached files are not re-downloaded) → ParseOilMonitor,
   sort by EffectiveFrom desc.
2. DOE NCR pump page → parse up to the 4 first-listed PDFs, sort by WeekFrom
   desc, keep newest 2.
3. DOE NCR LPG page → 2 newest by PeriodFrom.
4. Meralco latest-news page → newest 2 releases.
5. Maynilad interruptions scheduled/emergency/rotational for City, plus
   maintenance posts (per_page=3).
UA = the existing `UserAgent` string from cmd/presyo (pass it in options; add
`UserAgent string` to FetchOptions), 60 s timeout, one attempt per URL (no
retry loops), honour MinDelay between two requests to the same host. A
failure in one source records an Error and continues. Test Fetch with an
httptest server that serves the fixtures (rewrite base URLs via options:
`DOEBase, DOEMediaBase, MeralcoBase, MayniladBase`).

### 1.9 Post text (`text.go`) — template only, no LLM
`RenderFuelPost(s *Snapshot) (string, error)` uses Adjustments[0] (+ the newest
NCRPump whose WeekTo < Adjustments[0].EffectiveFrom, labelled as "before this
change"). `RenderBillsPost(s *Snapshot) (string, error)` uses Meralco[0],
LPG[0] (+LPG[1] for the month-on-month change), Water status line.
Format (TL first, then EN, then JA; separators "━━━━━━━━━━"; no "$"; no
nested parentheses; no markdown). Values: ₱ with 2 decimals for fuel per L,
4 decimals for Meralco per kWh, whole pesos with thousands separator for LPG
and for 200 kWh impact; ranges shown "−₱3.00 hanggang −₱3.02" (TL), "−₱3.00 to
−₱3.02" (EN), "−₱3.00〜−₱3.02" (JA). Use U+2212 minus like the weekly post.
Fuel example (2026-10-20 data):
```
⛽ Presyo ng langis — epektibo Okt 20–26, 2026
• Gasolina: −₱4.30/L
• Diesel: +₱2.60/L
• Kerosene: −₱1.00/L
💡 Sa 10 litro: gasolina −₱43.00, diesel +₱26.00
📍 Karaniwang presyo sa NCR bago ang galaw na ito (DOE, Okt 13–19): RON 91 ₱87.90 · RON 95 ₱88.90 · Diesel ₱91.40
━━━━━━━━━━
🇬🇧 Fuel prices — effective Oct 20–26, 2026
Gasoline −₱4.30/L · Diesel +₱2.60/L · Kerosene −₱1.00/L
Per 10 L: gasoline −₱43.00, diesel +₱26.00
NCR common pump prices before this change (DOE, Oct 13–19): RON 91 ₱87.90 · RON 95 ₱88.90 · Diesel ₱91.40
━━━━━━━━━━
🇯🇵 燃料価格 — 10/20〜10/26適用
ガソリン −₱4.30/L・軽油 +₱2.60/L・灯油 −₱1.00/L
10Lあたり：ガソリン −₱43.00、軽油 +₱26.00
今回の改定前のNCR一般価格（DOE、10/13〜10/19）：RON91 ₱87.90・RON95 ₱88.90・軽油 ₱91.40
━━━━━━━━━━
Source: DOE Oil Monitor & NCR pump price monitoring (doe.gov.ph) · Hindi opisyal na DOE page
```
"Sa 10 litro" = 10 × adjustment; for a range use 10×Lo .. 10×Hi. Bills
example:
```
🏠 Bayarin sa bahay — Oktubre 2026
⚡ Meralco: +₱0.3191/kWh → ₱15.0615/kWh mula ₱14.7424
   Sa 200 kWh: mga +₱64 sa bill
🔥 LPG 11 kg, karaniwang presyo sa NCR (DOE, Okt 1–8): ₱1,430, +₱89 vs Set 1–7
   Sa Las Piñas: ₱1,368–₱1,569
💧 Maynilad, Las Piñas: walang naka-schedule na water interruption — tiningnan Okt 26
━━━━━━━━━━
🇬🇧 Household bills — October 2026
⚡ Meralco: +₱0.3191/kWh → ₱15.0615/kWh, from ₱14.7424. At 200 kWh: about +₱64 on the bill
🔥 LPG 11 kg, NCR common price (DOE, Oct 1–8): ₱1,430, +₱89 vs Sep 1–7. Las Piñas: ₱1,368–₱1,569
💧 Maynilad, Las Piñas: no scheduled water interruptions — checked Oct 26
━━━━━━━━━━
🇯🇵 家計の光熱費 — 2026年10月
⚡ Meralco：+₱0.3191/kWh → ₱15.0615/kWh（前月 ₱14.7424）。200kWhで約+₱64
🔥 LPG 11kg・NCR一般価格（DOE、10/1〜10/8）：₱1,430、9/1〜9/7比 +₱89。ラスピニャス：₱1,368〜₱1,569
💧 Maynilad・ラスピニャス：計画断水なし（10/26確認）
━━━━━━━━━━
Source: Meralco, DOE LPG monitoring, Maynilad advisories · Hindi opisyal na page
```
Water line variants: when Las Piñas rows exist list up to 3 ("💧 Water interruption,
Las Piñas: <Barangays>, <From/Time or Schedule>, <Reason/Activity>"); when water
fetch failed, omit the water line entirely. The Meralco line's "+₱64" must
be the release's Impact200; the LPG "+₱89" = LPG[0].Common − LPG[1].Common.
Month names TL: Ene Peb Mar Abr May Hun Hul Ago Set Okt Nob Dis; full TL
month for the title (Oktubre). Las Piñas spelled with ñ in captions.

### 1.10 Checker (`check.go`)
`CheckPost(text string, s *Snapshot, kind string) []string` (problems): every
₱ amount / number with "/L", "/kWh", "kWh" etc. in the text must equal a
value from the snapshot or an allowed derivation (10×fuel adj, LPG diff,
Impact200, 200), correct sign; lint: no "$", no nested parentheses (ASCII
and full-width), no "NaN", no empty sections. Tests: rendered fixtures pass;
tampered numbers / flipped sign / swapped product fail.

## 2. Charts `internal/fbchart/utilities.go`
1080x1350 PNG, SAME visual style as the weekly chart (cream bg, icon +
"Presyo Bantay" header, pills, rounded bars, fonts, footer bar). Reuse
existing helpers; add new functions only. Footer left text for these:
"Source: DOE · Meralco · Maynilad" / "Hindi opisyal na page" (fuel chart:
"Source: DOE (doe.gov.ph)" / "Hindi opisyal na DOE page").
- `RenderFuelWeekly(s *utilities.Snapshot) (*image.RGBA, error)`: title
  "Presyo ng langis", subtitle "Epektibo Okt 20–26, 2026 · per litro";
  section pill "Galaw ngayong linggo": 3 big horizontal bars Gasolina /
  Diesel / Kerosene, green for rollback (negative), red for increase,
  label "−₱4.30/L" (ranges "−₱3.00–3.02/L"), bar length ∝ Hi magnitude;
  section pill "Nakaraang mga linggo": small paired bars (gasoline, diesel)
  for each Adjustments[i] newest last (up to 6 weeks), x labels "Okt 20" etc,
  value label above/below each bar; section pill "Karaniwang presyo sa NCR
  (DOE, Okt 13–19)" with 3 value chips RON 91 / RON 95 / Diesel.
- `RenderBills(s *utilities.Snapshot) (*image.RGBA, error)`: title
  "Bayarin sa bahay", subtitle "Oktubre 2026"; card ⚡ Meralco (big
  "₱15.0615/kWh", chip "+₱0.3191", line "200 kWh: mga +₱64"); card 🔥 LPG
  11 kg NCR ("₱1,430", chip "+₱89", line "Las Piñas ₱1,368–₱1,569 · DOE Okt 1–8");
  card 💧 Maynilad Las Piñas (status text). Emoji may be dropped in the PNG
  if the embedded fonts lack them (do not add fonts); text must never spill
  outside cards (use fitSize/ellipsize). Red chip for increases, green for
  decreases.
Tests: size 1080x1350, deterministic (render twice → identical bytes via
EncodeCompact), labels within bounds (reuse existing approach).

## 3. CLI `presyo utilities`
In `cmd/presyo/cmd_utilities.go`, wired into main's dispatch and usage:
- `presyo utilities fetch -cache DIR -out snapshot.json [-city "Las Piñas"] [-max-oil 6]`
  — the ONLY network-touching mode (DOE/Meralco/Maynilad public pages,
  polite as above). Writes indented JSON. Exit 0 even with partial source
  errors (printed to stderr), 1 if nothing parsed.
- `presyo utilities render -snapshot FILE -kind fuel|bills -out-dir DIR`
  — offline; writes `<kind>.txt`, `<kind>.png` (EncodeCompact), and
  `<kind>.check.json` ({ok, problems}); exit 3 if the checker fails.
  Never posts anything; there is no posting code.
Tests for render using a snapshot built from fixtures (parse the testdata
files directly; no network).

## 4. Docs
Add a short "Household utilities (prototype)" section to go/README.md
(commands, sources, that it is dry-run only). Keep this spec file.
