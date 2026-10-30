# Regional Facebook posts (v4) — spec for opencode

Goal: auto-post to the Presyo Bantay Facebook Page, one REGION per post, 5
regions per day. Each post = short plain text (approved format below) + a
1080x1350 PNG chart rendered in pure Go. Numbers come ONLY from Go
(deterministic). LLM is optional and OFF by default (template text).

Hard rules: edit only go/ (plus prototype/regions/ when asked). Keep the
site deals-card work and existing commands working. Never open
../data/presyo.db; local DB snapshot is /tmp/presyo-snap.db (read-only:
`file:...?mode=ro`). No git commits. NEVER post to Facebook from tests or
while developing: tests use httptest servers only. Never print/log the
token. Only new dependency allowed: golang.org/x/image (official Go
sub-repo, pure Go) for font rasterisation/scaling; justify it in
go/README.md. Binary must stay static (CGO_ENABLED=0).

## 1. Region mapping (`internal/regions`)
Explicit table market-name -> region id (exact names from the `markets`
table). Unknown/unmapped market names are reported by a `presyo regions`
subcommand (lists every market in the DB with its region, last price date,
and "UNMAPPED" if missing) and are ignored by signals.
Regions (id, order, TL title, EN title, JA title, cities):
1 south  "South Metro"  / "South Metro" / "南部（ラス・ピニャス周辺）"
  - New Las Piñas City Public Market, Suki Market Moonwalk (Las Piñas;
    KM20 Alabang–Zapote Rd, Talon Uno), La Huerta Market/Parañaque,
    Alabang Central Market, Pamilihang Lungsod ng Muntinlupa (Muntinlupa),
    Bicutan Market, Taguig People's Market (Taguig), Grace Marketplace,
    Pateros Market (Pateros; Taguig–Pateros district)
2 manila "Maynila at Pasay" / "Manila & Pasay" / "マニラ・パサイ"
  - Paco Market, Quinta Market/Manila, Pritil Market/Manila, San Andres
    Market/Manila, Trabajo Market, Dagonoy Market, Blumentritt Market,
    Obrero Market (Manila); Pasay City Market, Cartimar Market (Pasay)
3 qc "Quezon City" / "Quezon City" / "ケソン市"
  - Commonwealth Market/Quezon City, Muñoz Market/Quezon City, Mega
    Q-mart/Quezon City, Farmers Market (Cubao), Balintawak (Cloverleaf)
    Market, Murphy Public Market, North Diversion Market (Apolonio Samson,
    QC), Kamuning Public Market
4 north "CAMANAVA" / "North (CAMANAVA)" / "北部（カマナバ）"
  - Maypajo Public Market/Caloocan, Malabon Central Market, Hulong Duhat
    Public Market (Malabon), Navotas Agora Market, KLMV New Karuhatan Market,
    New Marulas Public Market/Valenzuela
5 east "Silangan" / "East Metro" / "東部"
  - Mutya ng Pasig Mega Market, Pasig City Mega Market (Pasig), Marikina
    Public Market, New Parang Market (Marikina), Mandaluyong Public Market,
    Mandaluyong Public Market I, Mandaluyong Public Market II, Agora Public
    Market/San Juan, Guadalupe Public Market/Makati, Poblacion Public Market
    (assumed Makati Poblacion)
Short market display names for posts: strip "/City" suffixes and generic
words, e.g. "Las Piñas", "Suki Moonwalk", "La Huerta", "Alabang", "Bicutan",
"Taguig People's", "Grace, Pateros", "Paco", "Quinta", "Pritil",
"San Andres", "Trabajo", "Dagonoy", "Blumentritt", "Obrero", "Pasay",
"Cartimar", "Commonwealth", "Muñoz", "Mega Q-mart", "Farmers, Cubao",
"Balintawak", "Murphy", "North Diversion", "Kamuning", "Maypajo",
"Malabon Central", "Hulong Duhat", "Navotas Agora", "Karuhatan",
"Marulas", "Mutya ng Pasig", "Pasig Mega", "Marikina", "Parang",
"Mandaluyong", "Mandaluyong I", "Mandaluyong II", "San Juan Agora",
"Guadalupe", "Poblacion". No short name may contain parentheses
("(", ")", "（", "）"): the PNG wraps it as "(<short>)" and text lines use
"· <short>", so a parens-bearing short would render nested like
"(Grace (Pateros))". Put the table in Go; test that every market in
/tmp/presyo-snap.db maps to exactly one region.

## 2. Region signals (`internal/signals`, new func + `presyo region-signals`)
`presyo region-signals -db DB -region south [-date] -out FILE`.
Per market in the region, per canonical commodity (skip "other" category,
same alias merging as now): market basis = that market's own daily price
series; requires a price on the as-of date and >= 10 priced dates in the 30
calendar days ending on as-of. Compute price, median_30d, pct_vs_median_30d
(1 dp), diff_vs_median_30d, trend_7d_pct (as before). (No fallback to
other markets inside a market — a market without enough data is just not a
candidate for that commodity.)
Deals: candidate (market, commodity) with pct <= -8 and n_days >= 10 and
(trend_7d null or <= +5). De-duplicate per commodity: keep the candidate
with the lowest pct (tie: lower price, then market name). Rank by pct
ascending, keep top 5. Entry: {rank, name, market (full), market_short,
unit, price, median_30d, pct_vs_median_30d, pct_rounded (int, round half
away from zero), diff_vs_median_30d, trend_7d_pct, n_markets_with_deal
(how many region markets also qualify for this commodity)}.
Pricier: per commodity, among region markets with valid basis, if at least
half of them (and >= 2 markets) have pct >= +8: entry with region medians:
{rank, name, unit, price_median (median of those markets' as-of prices,
2 dp), pct_median (median of their pct, 1 dp), pct_rounded,
n_markets_pricier, n_markets}. Rank by pct_median descending, top 3.
Top level: {as_of, region{id,title_tl,title_en,title_ja}, markets_used
[{name, short, priced_items_on_as_of}], markets_missing (mapped markets with
no price on as-of), thresholds, deals, pricier, generated_by}.
`as_of` defaults to the latest price date in the DB across the region's
markets. Tests with a synthetic DB: dedupe, min-days, ranking, pricier
majority rule, missing markets, rounding (−40.5 -> −41, 13.5 -> 14).

## 3. Post text (`internal/fbregion`), exact approved SHORT format
Reproduce this structure exactly (this is the approved example for one area;
for regions the deal lines end with " · <market_short>"; pricier lines have
no market). Use U+2212 "−" for negative percents, "+" for positive, integers
(pct_rounded). Dates: TL "Okt 23" (Ene Peb Mar Abr May Hun Hul Ago Set Okt
Nob Dis), EN "Oct 23", JA "10/23".

```
🛒 Sulit sa {TL title} — presyo noong {TL date}

✅ Mas mura kaysa usual:
• {TL name} ₱{price}/kg (−{p}%) · {market_short}
...

⚠️ Mas mahal ngayon:
• {TL name} ₱{price_median}/kg (+{p}%)
...

━━━━━━━━━━
🇬🇧 Deals in {EN title} — prices of {EN date}
✅ {EN name} ₱{price} (−{p}%), {EN name} ₱{price} (−{p}%), ...
⚠️ Pricier: {EN name} ₱{price} (+{p}%), ...

━━━━━━━━━━
🇯🇵 {JA title}のお買い得 — {JA date}の価格
✅ {JA name} ₱{price}（−{p}%）、...
⚠️ 高め：{JA name} ₱{price}（+{p}%）、...

━━━━━━━━━━
% = kumpara sa usual na presyo (30 araw) / vs. 30-day usual price / 30日間のいつもの価格比
Source: DA Price Monitoring · Hindi opisyal na DA page
📊 https://abalol.xyz/presyo-bantay/
```
Units: "/kg" for per kg; for per piece use "/pc" (TL "/piraso" ok) and JA
"1個". Prices: print without decimals when whole (₱130), else 2 dp (₱8.50).
JA names: short, no long glosses (ペチャイ・バギオ, サヨテ, トマト, ナス, 赤唐辛子,
青唐辛子, じゃがいも …) — reuse/extend the names table in internal/advice.
EN names short ("Red chili", "Potato", "Green chili", "Tomato" …).
TL names: Pechay Baguio, Sayote, Kamatis, Talong, Siling pula, Siling berde,
Patatas, Karot, Sibuyas pula, Sibuyas puti, Manok, Itlog, Galunggong,
Tilapia, Bangus, Liempo, Kasim, Ampalaya, Repolyo, Kalamansi, Asukal,
Bigas (premium/well-milled …) etc.
No deals: TL "✅ Walang kapansin-pansing mura ngayon." EN "✅ No notable
deals today." JA "✅ 今日は目立つお買い得なし。"; no pricier: omit the ⚠️
block/line. Never use "$". Never mention NCR/basis jargon.
Optional LLM rewrite behind `-llm` flag (default off) reusing the existing
opencode runner; if used, the checker must pass or the template is used.

## 4. Checker coverage
Extend internal/advice checker (or add a region checker) so that the FINAL
post text is validated against the region signals JSON: every ₱ amount and
% must bind to the item on its line/segment (TL per-line; EN/JA
comma-separated segments each naming one item) and for deal lines the
" · <market_short>" must be the market of that deal (market mismatch
otherwise). Handle U+2212 minus and integer-rounded percents (match
pct_rounded exactly for integers). Lint: no "$", no jargon, no Latin words
in the JA block except "₱" and digits/%. Tests incl. tampering (wrong
market, wrong pct, wrong price, "$").

## 5. PNG chart (`internal/fbchart`), pure Go, 1080x1350
Match the approved design (see ../prototype/advice/fb_post_chart.png; Read
it): cream background (#FBF6E9-ish), header row with small basket icon
(embed a downscaled ~96px copy of ../branding/presyo-bantay-icon.png,
generate it once with a small `go run` helper or at init via x/image/draw
from an embedded downscaled PNG — do not embed the 780 KB original) next to
"Presyo Bantay"; big dark-green (#1E5631-ish) bold title "Sulit ngayon sa
{TL title} · {TL date}"; subtitle "Presyo ngayon vs usual (30-day median),
per kg"; section pill "Mas mura kaysa usual" (dark green bg, white text)
then one row per deal: left column item name (bold, dark green), "₱{price}/kg"
and "({market_short})" smaller; bar area with ONE shared zero line for both
sections and a common scale (max |pct| across deals+pricier → ~45% of bar
area width); green bars extend left of zero with "−41%" label at the bar's
left end; then red section pill "Mas mahal ngayon" and red bars to the
right with "+70%" label; footer band (dark green) with "Source: DA Price
Monitoring · Hindi opisyal na DA page" and "abalol.xyz/presyo-bantay".
Thin row separators. Handle 0..5 deals and 0..3 pricier gracefully (empty
section text "Walang kapansin-pansing mura ngayon."). Long names must not
overflow (shrink or ellipsis). Use the minus sign "−". No "$".
Fonts: embed OFL Montserrat static TTFs from /tmp/fonts (Montserrat-Bold,
-SemiBold, -Medium, -ExtraBold; copy the needed ones + OFL.txt into
go/internal/fbchart/fonts/). Check at init/tests whether Montserrat has a
glyph for '₱' (U+20B1) and '−' (U+2212); if not, fall back per-rune to
Noto Sans (/tmp/fonts/NotoSans-Bold.ttf / -SemiBold.ttf, OFL, copy with
NotoSans-OFL.txt). Implement per-rune fallback in the text drawer. Test: the
glyph lookup for '₱' succeeds in the face actually used (no tofu).
`presyo chart -signals region.json -out file.png`.

## 6. `presyo fbpost`
Flags: -db, -region {south|manila|qc|north|east} (required), -date
(default latest), -out-dir (default $PRESYO_HOME/fbpost/<date>/ or
./fbpost/<date>), -dry-run, -state FILE (default
$PRESYO_HOME/state/fbpost.json, PRESYO_HOME default $HOME), -env-file
(default $PRESYO_HOME/.config/presyo/fb.env containing
PRESYO_FB_PAGE_ID=... and PRESYO_FB_PAGE_TOKEN=...; env vars of the same
names override), -max-age-days 2, -disable-file (default
$PRESYO_HOME/.config/presyo/fbpost.disabled), -graph-base (default
https://graph.facebook.com/v21.0, for tests), -llm (off).
Flow: compute region signals (as-of = latest date) -> if as-of is older
than max-age-days vs today in Asia/Manila: log "stale data, skip" exit 0 ->
render text + PNG, run checker (fail => exit 3, no post) -> write
<region>.txt, <region>.png, <region>.signals.json, <region>.check.json in
out-dir -> if -dry-run: print paths, exit 0 (no network, token not needed)
-> if disable-file exists: log "disabled", exit 0 -> if state already has
(region, as_of): log "already posted", exit 0 -> POST multipart to
{graph-base}/{page-id}/photos with fields source (png file), message (text),
access_token (form field, never in URL), published=true; timeout 60 s; ONE
attempt only (no retries). On success record {region, as_of, post_id,
photo_id, posted_at} in the state file (atomic write via temp+rename) and
log the post id. On any error: log status code + Graph error message/code
(never the token; scrub the token string from any error text), exit 1.
At most one post per invocation. Tests with httptest: success path writes
state; second run skips; disabled file skips; stale skips; Graph error
returns 1 and does not write state; request has multipart fields and token
not in URL/logs; dry-run makes no HTTP request.

## 7. Cron (do NOT install; orchestrator does it, commented out)
One post per region and day at 08:00 south, 11:00 manila, 14:00 qc, 17:00 north, 20:00 east (Manila time); the exact crontab lives in private ops notes.
(the binary reads the env file itself; sourcing is not required — keep the
lines simple: no secret on the command line).

## 8. Finish
gofmt, go vet ./..., go test ./..., bash scripts/daily_test.sh. Build
static: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath
-ldflags "-s -w" -o bin/presyo ./cmd/presyo. Then run on the local
snapshot for all 5 regions:
`./bin/presyo fbpost -db /tmp/presyo-snap.db -region R -dry-run -out-dir
/tmp/fbdry -max-age-days 9999` and print the 5 texts. (The orchestrator
will inspect the PNGs.)

## 9. SCHEDULE CHANGE (supersedes §6 cadence and §7 cron)
DA PDFs typically appear ~05:20–05:45 Manila the next morning (sometimes
late batches, e.g. 21:52). Posts should go out as early as useful.

(a) New-data check: `presyo newdata -db DB [-state FILE] [-listing-url]`:
ONE GET of the DA listing HTML (https://www.da.gov.ph/price-monitoring/ —
never fetch PDFs here; robots disallows *.pdf), using conditional GET
(If-None-Match / If-Modified-Since cached in a small state file,
default $PRESYO_HOME/state/newdata.json; a 304 means "no new data"). Parse
with the existing listing parser; if any listed date is newer than
MAX(prices.date) in the DB, print it and exit 0; otherwise exit 10 (no new
data); exit 1 on error. Same UA/timeout as ingest. Tests with httptest
(new date, none, 304, error).
Script `go/scripts/watch.sh` (same PRESYO_HOME conventions/safety checks as
daily.sh, its own flock, log $PRESYO_HOME/logs/watch.log): if `presyo
newdata` exits 0 -> run $PRESYO_HOME/bin/daily.sh once (it has its own
lock; the existing 06:15/22:15 daily runs stay as a safety net). Then run
`presyo fbpost -auto ...` (see b) unless $PRESYO_HOME/.config/presyo/
fbpost.disabled exists. Never loops/retries. Extend tests (watch_test.sh
with a fake BIN like daily_test.sh).

(b) `presyo fbpost -auto` (in addition to explicit -region): decides which
region is due, at most ONE post per run:
- D = latest data date in DB. If D older than 2 days vs today (Manila):
  log skip, exit 0.
- Posting window: only between 05:30 and 20:00 Asia/Manila (flags
  -window-start 05:30 -window-end 20:00); outside -> log, exit 0.
- Order: candidates are regions with no state entry for D, most-overdue
  first (the key is the region's latest state as-of; no entry sorts
  first; ties keep south, manila, qc, north, east order). If all 5
  posted for D -> exit 0.
- Spacing: if the last successful post (any region/date) was less than
  2h ago (flag -min-gap 2h) minus -gap-grace (default 5m) -> exit 0.
  Exception: none (South for a new date also waits for the gap; that's
  fine).
- A checker failure (exit 3) for the first candidate tries the next
  candidate in the same run; all failed -> exit 3. Still at most ONE
  successful post per run.
- If a newer date arrives while an older date's regions are unfinished,
  just switch to the newer date (older date's remaining regions are
  dropped).
- Never repost the same region+date. One attempt, no retries.
- `-now` flag (RFC3339) for tests. Tests: window edges, gap, order,
  switching to newer date, all-done, stale.

(c) Titles must make clear the data date is the DA report date (usually
yesterday), not today (em dash "—" for the date part in all three
languages; never wrap the date in parentheses — the EN title itself may
contain "(CAMANAVA)" and JA titles may contain "（…）", so parens would
double/nest like "(Grace (Pateros))" or "Deals in North (CAMANAVA)
(prices of Oct 23)"):
TL "🛒 Sulit sa {TL title} — presyo noong {Okt 23}"
EN "🇬🇧 Deals in {EN title} — prices of {Oct 23}"
JA "🇯🇵 {JA title}のお買い得 — {10/23}の価格"
Chart title: "Sulit sa {TL title}" with a second line "Presyo noong Okt 23 ·
vs usual (30-day median), per kg" (replace the old subtitle). Update the
checker/tests accordingly (checker must flag nested "(" / "（" and
heading ") (" / "）（" double groups; tests render all 5 regions and assert
no text line or PNG "(<short>)" label nests).

(d) Schedule (Manila time; the exact crontab is kept in private ops notes and
installed commented out): `watch.sh` every 15 min 05:00-08:59, hourly
09:00-22:00 and once at 23:00.
