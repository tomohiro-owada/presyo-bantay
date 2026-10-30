# Presyo Bantay — Go port

One static Go binary (`CGO_ENABLED=0`) replacing the Python daily pipeline.
Module: `github.com/tomohiro-owada/presyo-bantay/go`, rooted at `go/`.

```sh
cd go
go build -o bin/presyo ./cmd/presyo
./bin/presyo parse ../data/raw/Price-Monitoring-October-23-2026.pdf
./bin/presyo ingest --dry-run --listing-file ../tests/fixtures/price-monitoring-listing.html
./bin/presyo ingest-json ../data/json --db /tmp/presyo.db
./bin/presyo fx --db /tmp/presyo.db --since 2026-10-01 --until 2026-10-23
./bin/presyo export --db /tmp/presyo.db --out /tmp/site
./bin/presyo version
```

Common flags: `-db` (default `data/presyo.db`), `-raw` (default
`data/raw`), `-json-dir` (default `data/json`).

| command | behaviour |
|---|---|
| `parse PDF [-out-dir] [-raw] [-source-url] [-markets-of-interest]` | Same JSON as Python `cmd_parse` (`{"date","source_url","layout","markets"}`, indent 2, non-ASCII unescaped). Strict sanity: exit 2 on layout change, 1 on error. |
| `ingest [-max 3] [-lookback 7] [-dry-run] [-listing-file] [-pdf FILE]` | Daily run: scrape `https://www.da.gov.ph/price-monitoring/`, take per-date best PDFs newer than `MAX(prices.date)` plus missing dates within the last 7 days, newest first, at most 3 downloads. Cached PDFs are not re-downloaded (but are still parsed/ingested locally). A cached PDF that fails parse/checks counts as a failure but never blocks the other dates. Download → `.meta.json` → parse → backfill-style checks (hard failure skips the file, warnings printed, non-zero exit at end) → `data/json/<date>.json` → idempotent upsert with alias canonicalization. The final `ingest: ...` summary line names the ingested dates (`dates: 2026-10-23, ...`, or `dates: none`). |
| `ingest-json PATH [-allow-synthetic]` | Load existing JSON snapshots (file or directory) into SQLite. Synthetic payloads are refused unless `--allow-synthetic` is given. |
| `fx [-since] [-until]` | Frankfurter USD/PHP rates (`.../v1/{start}..{end}?from=USD&to=PHP`, yearly chunks, 2 s between chunks), upserted into `fx_rates`. Defaults: since = day after latest stored FX date (else earliest price date), until = today in Asia/Manila. |
| `export [-out site/public/data]` | Byte-for-byte port of `export_site.py` (compact JSON, alias-canonicalized). `PRESYO_GENERATED_AT` pins `meta.json.generated_at` (test hook). |
| `signals [-db] [-date] [-focus] [-nearby] [-out] [-fx-window]` | Deterministic advice numbers from the DB (opened read-only): per-commodity medians, 30d comparison, 7d trends, cheap/expensive flags, multi-day cheapest, substitutions, USD/PHP correlation, summary. See "Actionable advice" below. |
| `region-signals -db DB -region south\|manila\|qc\|north\|east [-date] -out FILE` | Deterministic per-region deal numbers from the DB (opened read-only): per-market daily price series per commodity (price on as-of + ≥ 10 priced days in 30 days), deals (pct ≤ −8, trend ≤ +5, deduped per commodity, top 5) and pricier (majority rule, region medians, top 3). See "Regional Facebook posts" below. |
| `regions -db` | List every market in the DB with its region id, last price date, and "UNMAPPED" when the market is in no region. |
| `chart -signals region.json -out file.png` | Render the 1080x1350 regional PNG chart in pure Go (no network, no fonts on the system: embedded Montserrat + Noto Sans fallback). |
| `newdata -db DB [-state FILE] [-listing-url URL]` | ONE conditional GET of the DA listing page (never PDFs): prints the newest listed report date newer than `MAX(prices.date)` and exits 0 (new data), exits 10 when there is none (incl. HTTP 304 via the cached ETag/Last-Modified in `$PRESYO_HOME/state/newdata.json`), exits 1 on error. Same UA/timeout as ingest. |
| `fbpost -db DB -region R [-date] [-out-dir] [-dry-run] [-state] [-env-file] [-max-age-days 2] [-disable-file] [-graph-base] [-llm]` | Post one region to the Facebook Page: compute region signals, render text + PNG, verify with the region checker (exit 3 on failure, no post), write `<region>.txt/.png/.signals.json/.check.json` into the out dir, then upload once to `{graph-base}/{page-id}/photos` (token as a form field, never in the URL) and record it in the state file. `-dry-run` renders and checks without network or token. Skips stale data, an existing state entry, or a disable file. |
| `fbpost -db DB -auto [-dry-run] [-now RFC3339] [-window-start 05:30] [-window-end 20:00] [-min-gap 2h] [-gap-grace 5m] [...]` | Decide which region is due, at most ONE post per run: D = latest data date in the DB (stale D older than 2 days vs Manila today skips); only between 05:30 and 20:00 Asia/Manila; candidates are regions with no state entry for D, most-overdue first (no state entry sorts first, ties keep south, manila, qc, north, east order; a newer D takes over, dropping unfinished older dates); skip when the last successful post is newer than `-min-gap` minus `-gap-grace`; the first candidate that passes the checker wins (a checker failure tries the next candidate); never repost a region+date; one attempt, no retries. |
| `advise -signals FILE -out-dir DIR [-model] [-opencode] [-timeout] [-retries] [-no-llm]` | Run the opencode CLI over a signals.json file to draft trilingual advice (`advice.md`), then check its numbers (`check.json`). Falls back to the deterministic template when the call/check fails. Never run with the live DB; numbers come only from signals. |
| `advice-check -signals FILE -advice FILE [-out]` | Verify every peso amount / percentage in advice text exists in (or derives from) signals.json. Exit 3 when numbers are flagged. |
| `deals -signals FILE [-advice-dir DIR] -out FILE` | Build the site `deals.json` (texts + deals/pricier + thresholds). Reuses llm texts from an advise dir only when they passed; else the template. |
| `daypages -db DB -site-public site/public [-date D|-from F -to T] [-force] [-base-url URL] [-html-only]` | Publish static daily report pages: for each date render the 5 regional posts through the same pipeline as `fbpost` (same signals, same text, same checker, same chart pixels, compact paletted PNGs) into `site/public/d/D/`, plus `day.json`, `data/days.json`, per-day `index.html` and the `d/` archive index. See "Daily report pages" below. |
| `weekpages -db DB -site-public site/public [-week W\|-from-week A -to-week B] [-force] [-base-url URL] [-html-only] [-now RFC3339]` | Publish the static Sunday weekly report (`internal/weekly` signals + `RenderWeekly` text + `CheckWeekly` + `fbchart.RenderWeekly`, compact PNG) into `site/public/w/W/`, plus `w/index.html` and `data/weeks.json`. Frozen: existing `week.json` never re-renders without `-force`. See "Sunday weekly report" below. |
| `fbweekly -db DB [-week W] [-dry-run] [-out-dir] [-auto] [-site-public DIR] [-state] [-env-file] [-disable-file] [-min-gap 2h] [-window-start 18:00] [-window-end 20:00] [-graph-base] [-now RFC3339]` | Post the Sunday weekly report: one combined trilingual post + 1080x1350 chart. Manual mode reuses the frozen `w/W/` page when present (FB == site), else computes fresh; `-auto` posts only on Sunday 18:00-20:00 Manila when the page exists and spacing/state allow. State entry `{region:"weekly", as_of:"2026-W43"}` in the shared state file (flocked). See "Sunday weekly report" below. |
| `version` | Print the binary version. |

Exit codes: `0` ok, `1` error, `2` layout changed.

HTTP politeness (DA): `PresyoBantay/0.1 (non-profit community price watch;
https://github.com/tomohiro-owada/presyo-bantay)` UA (same string on
Frankfurter requests), 30 s timeout, ≥ 10 s (+ up to 5 s jitter) between
any two da.gov.ph requests, `Retry-After` honored (capped at 600 s),
exponential backoff from 60 s on 429/5xx, at most 3 attempts per file.

## Dependencies

Direct (`go.mod`):

- `github.com/ledongthuc/pdf v0.0.0-20261020125853-5e46805f718e`
- `modernc.org/sqlite v1.61.0` (pure Go, `CGO_ENABLED=0`-safe) via
  `database/sql`
- `golang.org/x/image` (official Go sub-repo, pure Go, no C) — used
  **only** for font rasterisation/scaling in `internal/fbchart`
  (`font/sfnt` + `font/opentype` glyph lookup/drawing faces, `draw`
  image scaling for the `tools/mkicon` icon downscale) and PNG
  encode/decode. Everything else in the chart renderer (layout, shapes,
  per-rune fallback, text shaping) is hand-written in this repo, so the
  binary stays static (`CGO_ENABLED=0`) with no system fonts or native
  libraries. This is the single new dependency allowed by REGIONS_SPEC.md.

Everything else is an indirect dependency pulled in by the two above
(everything pure Go, no C). Full module list (`go list -m all`):

```
github.com/tomohiro-owada/presyo-bantay/go
github.com/dustin/go-humanize v1.1.0
github.com/google/pprof v0.0.0-20261016150608-1facf8f91b50
github.com/google/uuid v1.6.0
github.com/hashicorp/golang-lru/v2 v2.0.7
github.com/ledongthuc/pdf v0.0.0-20261020125853-5e46805f718e
github.com/mattn/go-isatty v0.0.24
github.com/ncruces/go-strftime v1.1.0
github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec
golang.org/x/image v0.47.0
golang.org/x/mod v0.42.0
golang.org/x/sync v0.24.0
golang.org/x/sys v0.49.0
golang.org/x/text v0.43.0
golang.org/x/tools v0.52.0
modernc.org/cc/v4 v4.30.3
modernc.org/ccgo/v4 v4.37.0
modernc.org/fileutil v1.4.0
modernc.org/gc/v2 v2.6.5
modernc.org/gc/v3 v3.2.0
modernc.org/goabi0 v0.2.0
modernc.org/libc v1.79.0
modernc.org/mathutil v1.7.1
modernc.org/memory v1.12.1
modernc.org/opt v0.2.0
modernc.org/sortutil v1.2.1
modernc.org/sqlite v1.61.0
modernc.org/strutil v1.2.1
modernc.org/token v1.1.0
```

(Note: the LRU package is `github.com/hashicorp/golang-lru/v2`, not
`github.com/hashicorp/golang-lru` — there is no v1 dependency.)

- PDF: `github.com/ledongthuc/pdf` (pure Go, **zero transitive
  dependencies**) — used **only for low-level access**: file/xref parsing
  (`pdf.Open`, `Reader`, `Page.V`), stream decoding (`Value.Reader`,
  including `FlateDecode`), the content-stream lexer (`pdf.Interpret` with
  `Stack`/`Value`), and font helpers (`Font.Width`, `Font.Encoder`, which
  cover `WinAnsiEncoding`, `Differences` dicts and `ToUnicode` CMaps).

### Why not `Page.Content()` (the high-level API)?

Evaluated `Page.Content()`, which returns `Content{Text, Rect}`, and it
cannot reproduce pdfplumber output for these PDFs:

1. **`re` rectangles ignore the CTM.** `Content()` appends
   `Rect{Point{x,y}, Point{x+w,y+h}}` in raw user space; the `cm` operator
   updates `g.CTM` but it is never applied to rectangles (pdfminer applies
   the CTM to every painted path point). Any page that draws table borders
   under a non-identity CTM would silently misplace every cell.
2. **Only `re` is captured.** Stroked paths (`m`/`l`/`h` + `S`), curves
   (`c`/`v`/`y`), and even-odd fills are dropped, so `lines`/`curves`
   cannot be reproduced.
3. **Text advance is single-byte only.** Glyph width is looked up as
   `Width(int(s[n]))` while iterating decoded runes, which is wrong for
   multi-byte (`Identity-H`) fonts; word spacing (`Tw`) is tracked but never
   applied to advances.
4. **No access to graphics-state details** (line width scaling, clipping vs
   painting) needed to distinguish real table borders from clip paths.

So `internal/pdftext` implements its own content-stream interpreter on top
of the low-level API, replicating pdfminer.six semantics exactly:

- graphics-state stack (`q`/`Q`), CTM (`cm`), line width (`w`, scaled by
  CTM), path construction (`m`/`l`/`c`/`v`/`y`/`h`/`re`) with painting
  (`S`/`s`/`f`/`F`/`f*`/`B`/`B*`/`b`/`b*` → rect/line/curve
  classification identical to pdfminer's `paint_path`; `n` discards,
  `W`/`W*` clipping paths are ignored, obsolete `F` is a no-op);
- text state (`Tc`/`Tw`/`Tz`/`TL`/`Ts`/`Tr`/`Tf`/`Tm`/`Td`/`TD`/`T*`/`Tj`/
  `TJ`/`'`/`"`) with per-glyph advances from the font `Widths` array,
  WinAnsi/ToUnicode decoding, and `LTChar` bounding boxes
  (`descent` from `FontDescriptor`, `adv = width * fontsize * scaling`,
  translated text matrix × CTM).

`internal/table` ports pdfplumber's default `"lines"` `extract_tables`
(rect edges → snap 3 → join 3 → intersections 3 → cells → tables; cell text
= chars whose center falls in the cell, line tolerance 3, word gap 3,
lines joined with `"\n"`), and `internal/parse` ports
`presyo_bantay/pdfparse.py` (single + range layouts, date hint).

Alternatives rejected: pdfcpu (dozens of transitive deps — supply-chain
risk the spec rules out), poppler shell-out (not static, not pure Go).

### Why `internal/pyjson` (no new dependency)?

Python `json.dump(indent=2, ensure_ascii=False)` vs Go `encoding/json`
differ in two spots: Go escapes `<`/`>`/`&` by default (disabled) and
prints integral floats as `45` where Python prints `45.0` (plus
shortest-round-trip exponent style). `internal/pyjson.PyFloat` mirrors
CPython's `float_repr_style short` exactly (verified against the repo
`.venv` for the value ranges we emit), and `Obj` preserves insertion order
(market/commodity order matters for byte-identical snapshots).

### Why a regexp for the listing page (no `golang.org/x/net/html`)?

Per the spec: a regexp over `<a ... href="...">` (single/double quoted,
case-insensitive, entity-unescaped, resolved with `net/url` like
`urljoin`) covers `bs4` `find_all("a", href=True)` semantics for the DA
listing page (verified against the saved fixture: same selected PDF as
Python).

## Verification

- **Parity**: `go run ./tools/parity -raw /workspace/presyo-scratch/raw-snap2 -ref /workspace/presyo-scratch/ref-snap2.jsonl`
  → `170567/170567 rows match (100.00%)`.
- **Parse**: Go `parse` vs Python `cmd_parse` on 5 PDFs (single + range,
  with/without `.meta.json`) → `diff -r` empty.
- **Store**: fresh Go DB from all `data/json` snapshots (`ingest-json`)
  vs fresh Python DB (`presyo_bantay.db.connect`/`ingest_file`) →
  208,698 price rows joined with market/commodity names identical
  (names, categories, specs, units, cities, prices, lows, highs, urls).
- **Export**: Python `export-site` vs Go `export` on the same scratch DB
  copy → `diff -r` empty (34 files, 3.3 MB; `generated_at` pinned via
  `PRESYO_GENERATED_AT` since it is wall-clock by design).
- `gofmt -l .`, `go vet ./...`, `go test ./...` all clean.

## Ported vs not ported

Ported: `pdfparse` (single + range layouts, dates), `listing`
(`find_all_*`, Revised/Updated/`-N` ordering, exclusions), `sanity`
(`run_sanity_checks`), `backfill` checks (`run_backfill_checks`,
`infer_layout`, Retry-After/backoff politeness rules inside the daily
`ingest`), `aliases` (`canonicalize`, groups, tables), `db` (schema,
migration, `derive_city`, `categorize_commodity`, `ingest_payload`,
`list_dates`, `match_names`, alias `normalize_commodities`), `fx`
(chunking, upsert, as-of), `export_site` (byte-for-byte), and the CLI
surface needed for the daily run (`parse`, `ingest`, `ingest-json`,
`fx`, `export`, `version`).

Not ported (out of scope for the daily pipeline): `summarize`/`trend`
(`summary.py`, `stats.py`), Telegram posting (`telegram.py`), LLM
rephrase (`llm.py`), the one-off `backfill`/`reparse-failed` drivers and
`normalize-commodities` CLI (the underlying `NormalizeCommodities` logic
lives in `internal/store` and is tested there), `render.py` except the
`short_market` rule needed by export.

## Actionable advice (`signals` / `advise` / `advice-check` / `deals`)

Deterministic Go code computes ALL numbers (`internal/signals` →
`signals.json`); the LLM only interprets them as a short daily deals card
(FB-ready text for the static site), and `internal/advice` verifies every
peso amount / percentage in the text exists in (or is derivable from)
`signals.json`. A deterministic template (`internal/advice/template.go`)
is the fallback when the LLM call or check fails.

```sh
./bin/presyo signals -db /tmp/presyo-snap.db -out /tmp/signals.json
./bin/presyo advise -signals /tmp/signals.json -out-dir /tmp/adv
./bin/presyo advise -no-llm -signals /tmp/signals.json -out-dir /tmp/adv
./bin/presyo advice-check -signals /tmp/signals.json -advice advice.md
./bin/presyo deals -signals /tmp/signals.json -out /tmp/deals.json
```

- `signals`: local-basis deal numbers from the DB (opened read-only):
  per-commodity Las Piñas price or nearby-markets median basis with 30-day
  median and comparison, 7-day trend with baseline, per-market prices and
  multi-day cheapest market (cheaper on 2 of the last 3 basis dates),
  sharp drop/rise flags, top deals (≥ 8% below usual) and pricier lists
  (pricier never carries cheapest_nearby), `uses_nearby_basis`, USD/PHP
  context only. Never point it at the live `data/presyo.db` while a
  backfill is writing — use a snapshot copy.
- `advise`: renders `internal/advice/prompt.tmpl` with the signals JSON
  plus the template skeleton inlined and runs `opencode run -m MODEL
  --format json` in a fresh empty temp dir (no repo files). Writes
  `prompt.txt`, `advice.md`, `run.json` (`text_source` "llm"|"template" +
  `fallback_reason`),   `check.json` and `deals.json`; exit 0 in both llm
  and template-fallback cases (fallback is a warning, not an error), 1
  only on I/O errors. `-no-llm` skips the call and uses the template.
  `OPENCODE_API_KEY` must be present in the cron environment (e.g. an env
  file with mode 600 sourced by the cron line) — without it opencode fails
  with an opaque "UnknownError", in which case the template fallback is used.
  Also writes `fb_post.txt` (combined TL/EN/JA Facebook post, at most 5
  deals + 2 pricier lines per language plus the shared site-link footer)
  and records its check under check.json's `fb_post` (template fallback
  when it fails).
- `advice-check`: standalone number checker used by `advise`, plus the
  jargon lint (no NCR/basis identifiers, no Latin in the Japanese section
  except `kg`/`DA`, no still-falling phrases, no markdown, no nearby
  footnote when `uses_nearby_basis` is false).
- `deals`: builds the site `deals.json` (`as_of`, `generated_at`
  Asia/Manila RFC3339, `PRESYO_GENERATED_AT` overrides for tests,
  `text_source`, `model` when llm, `texts: {tl,en,ja}`, `fb_post`,
  `deals`, `pricier`, `uses_nearby_basis`, `thresholds`; indent 2,
  non-ASCII unescaped). With `-advice-dir` it reuses llm texts only when
  run.json says "llm" with a passing check.json and advice.md; else the
  template. Also writes `fb_post.txt` next to `-out`.

## Regional Facebook posts (one region per post, 5 regions per day)

Deterministic Go code computes ALL numbers (`internal/signals.ComputeRegion`
→ `region-signals.json`); an optional LLM rewrite (`-llm`, off by default,
reusing the opencode runner) only rephrases connecting words and is used
only when the region checker passes, else the template is used.
`internal/fbregion.CheckPost` verifies every peso amount / percentage in
the final post text binds to the deal/pricier line or segment naming its
item (deal lines must carry the deal's own `· <market_short>`; integer
percents match `pct_rounded` exactly; U+2212 minus handled), plus the lint
(no `$`, no NCR/basis jargon, no Latin in the Japanese block).
`internal/fbchart` renders the 1080x1350 PNG in pure Go (cream background,
shared zero line, common scale, green/red bars, dark-green footer band;
Montserrat statics embedded under `internal/fbchart/fonts/` with per-rune
Noto Sans fallback; the ~96px basket icon is generated once with
`go run ./tools/mkicon` from `branding/presyo-bantay-icon.png` — the 780 KB
original is never embedded).

```sh
./bin/presyo regions -db /tmp/presyo-snap.db
./bin/presyo region-signals -db /tmp/presyo-snap.db -region south -out /tmp/south.json
./bin/presyo chart -signals /tmp/south.json -out /tmp/south.png
./bin/presyo fbpost -db /tmp/presyo-snap.db -region south -dry-run -out-dir /tmp/fbdry -max-age-days 9999
```

- `fbpost`: `PRESYO_HOME` (default `$HOME`) locates the default
  out dir (`$PRESYO_HOME/fbpost/<date>/`, else `./fbpost/<date>/`), state
  file (`$PRESYO_HOME/state/fbpost.json`), env file
  (`$PRESYO_HOME/.config/presyo/fb.env` with `PRESYO_FB_PAGE_ID` and
  `PRESYO_FB_PAGE_TOKEN`; same-named env vars override) and disable file
  (`$PRESYO_HOME/.config/presyo/fbpost.disabled`). The token is never
  logged (scrubbed from error text) and never appears in the URL.
  Exit codes: `0` ok/skip, `1` error, `3` check failed (no post).

Schedule (Asia/Manila; the exact crontab lives in private ops notes, not here;
install it commented out and enable deliberately): `scripts/watch.sh` every
15 min 05:00–08:59, hourly 09:00–22:00 and once at 23:00.

`scripts/watch.sh` (single pass, never loops; its own flock, log
`$PRESYO_HOME/logs/watch.log`): `presyo newdata` (exit 0 = a report date
newer than `MAX(prices.date)` in the DB) → run
`$PRESYO_HOME/bin/daily.sh` once (it has its own lock; the 06:15/22:15
daily runs stay as a safety net) → run `presyo fbpost -auto` unless
`$PRESYO_HOME/.config/presyo/fbpost.disabled` exists. Test it with
`scripts/watch_test.sh` (fake BIN, no network).

## Daily report pages (`daypages`)

Every data date's 5 regional posts are published on the static site as a
blog-like archive (`site/public/d/D/`), reusing the exact `fbpost`
pipeline (`internal/regionrender`: same region signals, same
`fbregion.RenderPost` text split per language by `fbregion.SplitPost`,
same checker, same `fbchart.Render` pixels) with a compact PNG encoder
(`fbchart.EncodeCompact`: exact-colour histogram, top-256 palette else,
nearest-colour mapping, no dithering, `png.BestCompression`).

```sh
./bin/presyo daypages -db /tmp/presyo-snap.db -site-public ../site/public -date 2026-10-23
./bin/presyo daypages -db /tmp/presyo-snap.db -site-public ../site/public -from 2026-10-01 -to 2026-10-23
./bin/presyo daypages -db /tmp/presyo-snap.db -site-public ../site/public -html-only
```

- A region "has data" when any of its markets has a price on D; D is
  skipped (fewer than 3 of 5 regions) and regions failing the checker
  are unpublished (never publish unchecked numbers).
- Per date: `d/D/<region>.png`, `d/D/day.json` (regions with
  per-language title/alt/text, image bytes, check status; shared footer
  without the site link), then always rebuilt from all `day.json`
  files: `data/days.json` (newest first), every `d/D/index.html`
  (prev/next, OG/Twitter meta, GA4) and the `d/index.html` archive.
  HTML carries no timestamps, so re-runs are byte-identical.
- `scripts/daily.sh` runs `daypages` after export/deals unless
  `PRESYO_DAYPAGES=0`, records `daypages_rc`, then runs `weekpages`
  unless `PRESYO_WEEKPAGES=0` (records `weekpages_rc`), and commits
  `site/public/d` and `site/public/w` with the other data (a daypages or
  weekpages failure still commits/pushes everything else).

## Sunday weekly report (`weekpages` / `fbweekly`)

The Sunday report covers ISO week `YYYY-Www` (e.g. `2026-W43` = Mon Oct
19..Sun Oct 25): report dates are the DB price dates Mon..Sat of that
week (Sunday excluded). A week is reportable with >= 3 covered dates
this week and >= 3 last week. Only mapped markets are used.

Deterministic Go code computes ALL numbers (`internal/weekly`):
Metro Manila-wide per commodity (weekly average = mean of daily medians
across mapped markets; week-over-week pct; drops <= −5 top 5, rises >=
+5 top 3) plus best area deals per region (weekly pct = rounded mean of
daily pcts vs each market's own 30-day median; <= −10, up to 2 per
region, one entry per commodity). `internal/weekly.RenderWeekly` renders
the one combined trilingual post; `internal/weekly.CheckWeekly` binds
every peso amount / percentage to its commodity (and market/region for
area deals) with the right sign and metric (week-over-week vs
area-deal); `internal/fbchart.RenderWeekly` renders the 1080x1350 PNG.

```sh
./bin/presyo weekpages -db /tmp/presyo-snap.db -site-public ../site/public
./bin/presyo weekpages -db /tmp/presyo-snap.db -site-public ../site/public -week 2026-W43
./bin/presyo weekpages -db /tmp/presyo-snap.db -site-public ../site/public -from-week 2026-W40 -to-week 2026-W43
./bin/presyo fbweekly -db /tmp/presyo-snap.db -week 2026-W43 -dry-run -out-dir /tmp/fbweek
```

- `weekpages`: default target is the latest reportable week whose
  Sunday is on or before today (Manila). Writes
  `SITE/w/W/{week.png,week.json,index.html}`, `SITE/w/index.html`
  (newest first) and `SITE/data/weeks.json`. A published week is FROZEN:
  an existing `week.json` is never re-rendered without `-force`. Also
  refreshes the `d/` archive link to the weekly reports.
- `fbweekly`: posts the combined text + chart. Manual mode reuses the
  FROZEN page output when `SITE/w/W/week.json` exists (so FB == site),
  else computes fresh. `-auto` posts only on Sunday 18:00-20:00 Manila
  when the frozen page exists, the week is reportable, no `weekly`
  state entry exists for the week, and the last post of any kind is at
  least 2h old. State entries are `{region:"weekly", as_of:"2026-W43",
  ...}` in the same state file as `fbpost`; both commands hold an
  exclusive flock on `<state>.lock` around read-check-post-write.
- `fbpost -auto` ignores `weekly` state entries when choosing the next
  region, counts weekly posts in the min-gap spacing, and on Sundays
  from 16:00 holds daily posts until the weekly post for that week
  exists or 20:00 ("holding for weekly post").
- `scripts/weekly.sh` (own flock, log `$PRESYO_HOME/logs/weekly.log`,
  respects `fbpost.disabled`) runs
  `$BIN fbweekly -db $DB -auto -site-public $REPO/site/public`.
  Test it with `scripts/weekly_test.sh` (fake BIN, no network).

## Household utilities (prototype)

Household utility costs (fuel / LPG / Meralco / Maynilad water):
fetch + parse official public sources, render trilingual captions +
1080x1350 charts locally, publish frozen pages on the static site, and
post them with the same state/gap machinery as the daily/weekly posts.
Every post, image and site page shows the trend vs the previous period.

```sh
./bin/presyo utilities fetch -cache /tmp/ucache -out /tmp/snapshot.json [-city "Las Piñas"] [-max-oil 6] [-sections fuel,pump,lpg,meralco,power,water,fcda,meralcox]
./bin/presyo utilities render -snapshot /tmp/snapshot.json -kind fuel|bills|water|power -out-dir /tmp/uout [-alert-id ID] [-maintenance-json FILE -city C -past-sample] [-meralco-ms-dir DIR -maynilad-feed-file FILE -now RFC3339]
./bin/presyo utilpages -snapshot /tmp/snapshot.json -site-public ../site/public [-force] [-base-url URL] [-now RFC3339]
./bin/presyo fbutil -db /tmp/presyo-snap.db -site-public ../site/public -auto [-dry-run] [-now RFC3339]
```

- `utilities fetch` is the ONLY network-touching fetch mode (DOE Oil
  Monitor + NCR pump/LPG pages, Meralco news releases, Maynilad
  interruption/maintenance/FCDA endpoints; same UA as the daily
  pipeline, 60 s timeout, one attempt per URL, ≥ 5 s between two
  requests to the same host, PDFs cached by file name).
  `-sections` limits the run and MERGES into the existing `-out`
  snapshot (untouched sections keep old data; per-section fetched_at
  recorded). Exit 0 even with partial source errors (to stderr), 1
  if nothing parsed.
- `utilities render` is offline: writes `<kind>.txt`, `<kind>.png`
  (compact) and `<kind>.check.json` (`{ok, problems}`); exit 3 when
  the checker fails. Kinds: `fuel` (this + last week, 6-week
  cumulative, NCR/nearby pump now-vs-prev), `bills` (Meralco/LPG/
  water old → new + family total), `water` (one alert; `-alert-id`
  selects it, default first; `-maintenance-json` builds a water-only
  snapshot offline, `-past-sample` marks sample output), `power`
  (one Las Piñas planned-interruption alert; `-alert-id` selects it,
  default first; `-meralco-ms-dir` / `-maynilad-feed-file` / `-now`
  build offline samples from fixtures, `-past-sample` marks sample
  output as `power-<id>` / `power-alert_PAST-SAMPLE_<date>`).
- `utilpages` publishes `site/public/u/`: frozen per-post pages
  (`u/fuel/<date>/`, `u/bills/<YYYY-MM>/`, `u/water/<id>/`,
  `u/power/<id>/`, each with `index.html`, `post.json`,
  `<kind>.png` — the SAME text/PNG used for FB), `u/index.html`
  (latest fuel/bills, pump table, water interruptions + Las Piñas
  weekly schedule rows + Parañaque/Muntinlupa/Bacoor nearby list from
  `Water.Nearby`, power `Kuryente / Power` section with Las Piñas
  now+upcoming rows plus Parañaque/Muntinlupa/Bacoor next-14d nearby
  list, sources) and `data/utilities.json` (additive `power` key).
  Frozen pages are never re-rendered without `-force`. No frozen
  power pages are written while the snapshot carries a layout alarm
  (the alarm stays in the snapshot/logs only; the site shows "no data
  right now").
- `fbutil` posts frozen pages only (FB == site), sharing the fbpost
  state file, flock, uploader and token handling. `-auto` posts at
  most ONE per run, priority water > power emergency > power planned
  > fuel > DA-first gate > bills (a bills page older than
  `-bills-max-wait`, default 24 h, bypasses the gate):
  water/power window 05:30–20:00 Manila, fuel/bills routine window
  12:00–19:00 Manila (`-routine-window-start`/`-routine-window-end`),
  Sunday 16:00–20:00 hold gates only non-emergency candidates
  (water `emergency` and power `emergency` bypass it), global 2 h
  min-gap (minus `-gap-grace`, default 5 m) over every entry with a
  short `-emergency-gap` (default 30 m, same grace) that urgent
  alerts bypass, `-publish-delay` (default 10 m) for the GitHub Pages
  deploy lag. DA first: fuel waits until the DB's latest price date
  has all 5 regions posted (water and power are exempt); fuel posts
  any weekday while its page is 0–6 days old, bills once per current
  month, water when not yet ended and its start is within 72 h ahead
  (open-ended emergencies while posted within the last 24 h) at most
  one per day, planned power when not yet ended and its start is
  within 72 h ahead at most one per day (emergency `x-` posts never
  count), emergency power when `End` is after now or `OpenEnded`
  within 24 h of `GeneratedAt` (no 72 h rule), tried before planned,
  capped at 3 per day, with >=50% window-overlap dedupe vs posted
  power pages. Schedule (Manila time, exact
  crontab kept in private ops notes; `deploy.sh` never edits crontab):
  `scripts/utilities.sh` hourly at :20 from 06:20 to 19:20, plus
  `scripts/utilities.sh --meralcox-only` at :10 and :40 from 05:10 to 20:10.
  `scripts/utilities.sh` runs fetch (hourly fuel+pump on Tue/Wed,
  daily otherwise; meralco days 5–20; LPG days 1–15; water + meralcox
  every hour with the feed inside water, FCDA every 3rd hour, power
  on Manila hours divisible by 6) → `utilpages` → commits
  `site/public/u` and `site/public/data/utilities.json` → `fbutil
  -auto` (dry-run with `PRESYO_UTIL_DRY_RUN=1`). Emergency power
  comes from the @meralco X inbox (`-meralco-x`, section `meralcox`,
  see `UTILITIES_MERALCO_X.md`).
- Water wording policy: interruption posts use OUR OWN short wording
  (`ReasonKey`: maintenance ng pasilidad / pipe network maintenance /
  pipe repair / limited supply / generic Maynilad work) plus a link
  to the Maynilad advisory — never copy their sentences, locations
  aside from barangay names, or images. Barangay names and times are
  relayed as facts.
- Sources: DOE Oil Monitor / NCR pump / NCR LPG PDFs (parsed with the
  existing pure-Go `internal/pdftext`), Meralco rate releases,
  Maynilad advisories + FCDA notices, Maynilad weekly-schedule RSS
  feed (`{MayniladBase}/feed/`, primary for weekly rows) and the
  Meralco maintenance-schedule pages (`{MeralcoCompanyBase}/news-and-
  advisories/maintenance-schedule`, section `power`) plus the @meralco
  X emergency inbox (section `meralcox`, see
  `UTILITIES_MERALCO_X.md`). Every number in
  text/charts comes from parsed data or a documented derivation (10×
  fuel amounts, LPG month-on-month diff, 200 kWh impact, pump deltas,
  family totals, water/power Nth-this-month trends); a missing
  previous value is stated, never guessed. See `internal/utilities`,
  `UTILITIES_SPEC_V2.md` and `UTILITIES_MAINTENANCE.md` (Part C:
  power posts, images, site, posting rules).

## Deploy

`deploy.sh` builds the static binary and ships it plus the job scripts
to the server; it never touches crontab and never runs as root. The target
is NOT hard-coded: set `VPS_HOST` (an ssh host/alias) in the environment or in
an untracked `go/.deploy.env` (git-ignored, `VPS_HOST=...`; optionally
`REMOTE_DIR=...`, default the remote home directory):

```sh
cd go
VPS_HOST=<ssh-host> ./deploy.sh           # go test ./... first, then build + upload + verify
DRY_RUN=1 VPS_HOST=<ssh-host> ./deploy.sh # print every command without running anything
```

Build (same flags the script uses; `main.version` is stamped from
`git describe`):

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty)" \
  -o bin/presyo ./cmd/presyo
```

The script checks the remote is `x86_64`, uploads the binary atomically
(`presyo.new` then `mv`), copies the four job scripts (`daily.sh`,
`watch.sh`, `weekly.sh`, `utilities.sh`) with mode 755, then prints the
remote `presyo version`. Cron lines are not printed or installed; the daily
safety net runs `scripts/daily.sh` at about 06:15 and 22:15 Manila time.

`scripts/daily.sh` is the daily job: log rotation, `flock`, pull, `presyo
ingest` / `fx` / `export`, then commit + push `data/json` and
`site/public/data` (skipping the commit when only `generated_at` changed).
With `PRESYO_DEALS=1` it also runs `signals` + `advise` (template unless
`PRESYO_DEALS_LLM=1`) and stages `site/public/data/deals.json`; deals
failures are logged but never fail the ingest/export result.
Test it with `scripts/daily_test.sh` (fake binary + temp bare repo, no
network).
