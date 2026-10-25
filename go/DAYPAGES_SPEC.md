# Daily report pages ("blog") — spec for opencode

Goal: publish every data date's 5 regional Facebook posts (same Go-rendered
chart PNGs + same trilingual text as `presyo fbpost`) on the static site as
a blog-like archive: one static page per data date, an archive index,
prev/next links, OG/Twitter meta so links share well on Facebook, GA4 tag.

Hard rules: no git commits; never contact Facebook, GitHub or DA; never read local secret files; never open ../data/presyo.db — local DB snapshot is
/tmp/presyo-snap.db (open read-only `file:...?mode=ro`). Go stays static
(CGO_ENABLED=0), no new Go deps (golang.org/x/image already allowed). Do NOT
change the Facebook posting behaviour/output of `presyo fbpost` (it is live).
Site: no new npm deps, no CDN except the GA4 tag below. Keep all existing
tests passing.

## 1. Go: `presyo daypages`
Flags: `-db` (read-only), `-site-public DIR` (e.g. ../site/public),
`-date YYYY-MM-DD` (default: latest price date in DB) or `-from`/`-to`
(inclusive range, for backfill), `-force` (re-render existing dates),
`-base-url` (default `https://abalol.xyz/presyo-bantay/`, trailing slash),
`-html-only` (skip rendering; just rebuild HTML+manifest from the day.json
files already on disk).

For each target date D (as-of = D, exactly how `fbpost -date D` computes it:
same region signals, same `fbchart.Render`, same `fbregion.RenderPost` text,
same checker — refactor shared code into a function used by both, without
changing fbpost output):
- A region "has data" if at least one of its markets has a price on D.
  Skip D entirely (log `daypages: skip D: ...`) if fewer than 3 of the 5
  regions have data, or if D already has `d/D/day.json` and no `-force`.
- Regions without data: no PNG; page shows a "no data" note for it.
- If the checker does not pass for a region: do not publish that region
  (treat as no data, log it); never publish unchecked numbers.
- Write `SITE/d/D/<region>.png` (south, manila, qc, north, east) using a
  COMPACT encoder (new `fbchart.EncodeCompact(w, img)`): exact-colour
  histogram; if <=256 colours use them, else the 256 most frequent (ties by
  colour value, deterministic) and map others to the nearest palette colour
  (squared RGB distance, memoised); no dithering; png.BestCompression;
  paletted image. Same 1080x1350 size. Must stay crisp/readable. Unit test:
  deterministic output, size below the RGBA encoding, and every pixel within
  a small distance of the original.
- Write `SITE/d/D/day.json`: {date, generated_at, regions:[{id, order,
  title:{tl,en,ja}, has_data, image (file name or ""), image_bytes,
  alt:{tl,en,ja}, text:{tl,en,ja}, check:"pass"}], footer:{tl,en,ja}}.
  `text` = the per-language block of the FB post (split the RenderPost
  output on the existing separator; the TL block, EN block, JA block; the
  shared footer lines go into `footer`, the site link line excluded). Add a
  `fbregion` helper that returns these blocks so site and FB text never
  drift. `alt` = short sentence like "South Metro: mas mura ang Sayote
  (−50%), …" built from the deals (top 3), per language.

After rendering, ALWAYS (also with -html-only) rebuild from all
`SITE/d/*/day.json`:
- `SITE/data/days.json`: {latest, days:[{date, path:"d/D/", label:{tl,en,ja},
  regions_with_data, south:{text:{tl,en,ja}, image:"d/D/south.png",
  title:{tl,en,ja}}}]} newest first. `south` is the featured region
  (south when it has data, else the first region with data) and `title`
  is that region's title per language (used for the archive prefix).
- `SITE/d/D/index.html` for every day (so prev/next links stay right).
- `SITE/d/index.html` archive list (newest first).
Output must be byte-deterministic (no generated_at inside HTML; sorted
order) so re-runs give empty git diffs.

## 2. Day page HTML (Go html/template, embedded with go:embed)
Static, no external CSS/JS/fonts, small inline <style> (~same look as the
site: cream bg #faf8f2, white cards, accent #0b5d3b, system font stack,
max-width 880px, mobile-first) and a tiny inline <script>.
- <html lang="fil">, charset, viewport, <title> e.g. "Sulit sa palengke ·
  Okt 23, 2026 — Presyo Bantay" (og:title identical), meta description =
  South TL deals summary + " · Prices from DA Bantay Presyo".
- OG: og:type=article, og:site_name="Presyo Bantay", og:title,
  og:description, og:url (absolute, base-url + "d/D/"), og:image (absolute
  base-url + "d/D/south.png"; if South has no data use the first region
  with data), og:image:width 1080, og:image:height 1350, og:image:alt,
  og:locale fil_PH (+ og:locale:alternate en_US, ja_JP). Twitter:
  twitter:card=summary_large_image, title, description, image.
  <link rel="canonical">. article:published_time = D.
- GA4 in <head> (exactly this, measurement id G-VL047YCMK8):
  <script async src="https://www.googletagmanager.com/gtag/js?id=G-VL047YCMK8"></script>
  <script>window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments);}gtag('js',new Date());gtag('config','G-VL047YCMK8');</script>
- Header: "Presyo Bantay" linking to the site top (relative "../../"),
  language toggle buttons Taglish / English / 日本語 (white-space:nowrap;
  the "Taglish / English / 日本語:" label hides below 520px via
  `@media(max-width:519px){.lang-label{display:none}}`, the group keeps
  `aria-label="Wika / Language / 言語"` for screen readers).
- Language: every translatable string is emitted for all 3 languages with
  `data-lang="tl|en|ja"`; the script reads localStorage key "pb-lang"
  (values tl/en/ja, default tl — same as the main site), hides the others,
  saves on toggle and sets <html lang> (fil/en/ja). Without JS: all three
  visible (acceptable).
- Body: h1 with the date in the active language (TL "Okt 23, 2026", EN
  "Oct 23, 2026", JA "2026年10月23日") and a heading "Sulit sa palengke /
  Market deals / 市場のお買い得" (not "today": the page may show an older
  day); short intro "% = vs usual price (30-day)"
  per language; prev/next nav (older/newer day) at top and bottom; then 5
  region sections in order south, manila, qc, north, east: h2 region title
  (active language), <img> (width=1080 height=1350, CSS max-width:100%;
  height:auto; first image eager + fetchpriority=high, others
  loading=lazy decoding=async; alt per language — use the TL alt in the
  attribute and swap via script), then the text block for each language
  rendered as lines (heading line bold, bullet lines as list items or
  lines; ₱ amounts nowrap). Image max ~540px wide on desktop next to/above
  text is fine; on mobile full width.
- Footer: source attribution "Source: DA Price Monitoring (Bantay Presyo)"
  linking https://www.da.gov.ph/price-monitoring/ , "Hindi opisyal na DA
  page / Not an official DA page / 農業省の公式ページではありません",
  privacy notice "Gumagamit ang site na ito ng Google Analytics para sa
  bilang ng bisita. / This site uses Google Analytics to count visits. /
  アクセス解析にGoogleアナリティクスを使っています。" (data-lang
  tl/en/ja like the other strings),
  links to archive ("../") and top ("../../"), and the per-language
  footer note.
- Archive page `d/index.html`: same head (GA4, OG with latest day's image,
  canonical base-url+"d/"), title "Araw-araw na ulat / Daily reports /
  毎日のレポート", days grouped under month headings newest first (TL
  "Oktubre 2026", EN "October 2026", JA "2026年10月" via
  `fbregion.FormatMonthLong`), newest day first within each month: date
  (per lang) linking "D/", plus the language-specific top deal line
  prefixed with the featured region name, e.g. "South Metro: Sayote
  ₱40/kg (−50%) · Alabang" (JA "南部（ラス・ピニャス周辺）: サヨテ
  ₱40（−50%）"). Links to top. Same lang-toggle mobile rule as day pages.
  Footer carries the same source/unofficial lines as day pages plus the
  trilingual privacy notice "Gumagamit ang site na ito ng Google Analytics
  para sa bilang ng bisita. / This site uses Google Analytics to count
  visits. / アクセス解析にGoogleアナリティクスを使っています."
  (data-lang tl/en/ja).

## 3. Site (TypeScript, existing Vite app in ../site)
- Add the same GA4 snippet to site/index.html <head>.
- Load data/days.json (missing file => no-op, like deals.json).
- New card right after the deals card: "Araw-araw na ulat / Daily reports /
  毎日のレポート": prominent link to the latest day page ("Tingnan ang ulat
  ngayong <date> — 5 rehiyon →" / "See the <date> report — 5 areas →" /
  "<date>のレポート（5地域）→"), then up to 7 most recent days as links,
  then "Lahat ng ulat / All reports / すべてのレポート →" to d/.
  Links built from import.meta.env.BASE_URL.
- Deals card integration: PREFER days.json latest South text (same as the
  Facebook post, active language) with its date; use deals.json only when
  days.json is missing/empty. Always add a link at the bottom of the
  deals card to the latest day page ("Lahat ng 5 rehiyon → / All 5 areas →
  / 全5地域を見る →"). Pure helper `resolveDeals` in
  site/src/dealsSource.ts (vitest-covered).
- i18n strings in tl/en/ja. Vitest tests for the pure helpers.
  The site footer adds `footerPrivacy` in all 3 languages ("Gumagamit ang
  site na ito ng Google Analytics para sa bilang ng bisita. / This site
  uses Google Analytics to count visits. /
  アクセス解析にGoogleアナリティクスを使っています。"), rendered via
  main.ts like the other footer lines.
- `npm run build` and `npm test` must pass. vite copies public/ verbatim, so
  d/ pages are served at /presyo-bantay/d/D/.

## 4. Automation: scripts/daily.sh
After export (and the optional deals step), run
`"$BIN" daypages -db "$DB" -site-public "$REPO/site/public"` unless
PRESYO_DAYPAGES=0. Record `daypages_rc` in the end line; a failure logs an
error and sets fail=1 but must not block the commit/push of the other data.
Include `site/public/d` in the change detection and the `git add`
(alongside data/json and site/public/data). Update scripts/daily_test.sh
(fake BIN) to cover: daypages called, rc recorded, d/ files committed,
PRESYO_DAYPAGES=0 skips. watch.sh needs no change (it runs daily.sh on new
data) — confirm watch_test.sh still passes.

## 5. Tests
Go unit tests: compact encoder; day.json/HTML generation from a fixture
(OG tags present with absolute URLs, GA4 id present, prev/next correct for
first/middle/last day, all 5 regions in order, no-data region note,
deterministic output, no nested parentheses); days.json order (incl.
south title); day title/heading use "Sulit sa palengke / Market deals /
 市場のお買い得" and never the old today-heading; lang-toggle has the
screen-reader aria-label, the hidden-below-520px label and nowrap buttons;
day + archive footers carry the trilingual privacy notice (data-lang
tl/en/ja); archive month headings (Oktubre/October/2026年10月) newest first with
region-prefixed language-specific summaries; FormatMonthLong. Site vitest:
resolveDeals prefers days.json, falls back to deals.json; i18n footerPrivacy
in all languages. go vet, gofmt,
go test ./..., scripts/daily_test.sh, scripts/watch_test.sh.
Update go/README.md (daypages section).
