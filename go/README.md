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
golang.org/x/mod v0.42.0
golang.org/x/sync v0.24.0
golang.org/x/sys v0.49.0
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

## Deploy

`deploy.sh` builds the static binary and ships it plus `scripts/daily.sh`
to the VPS; it never touches crontab and never runs as root:

```sh
cd go
./deploy.sh              # go test ./... first, then build + upload + verify
DRY_RUN=1 ./deploy.sh    # print every command without running anything
VPS_HOST=my-vps ./deploy.sh   # override the ssh host (default: <VPS_HOST>)
```

Build (same flags the script uses; `main.version` is stamped from
`git describe`):

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty)" \
  -o bin/presyo ./cmd/presyo
```

The script checks `ssh <VPS_HOST> uname -m` is `x86_64`, copies the binary
to `<VPS_HOST>:$HOME/bin/presyo.new`, atomically `mv`s it to
`$HOME/bin/presyo`, copies `scripts/daily.sh` to
`$HOME/bin/daily.sh` (chmod 755), then prints the remote
`presyo version`.

Suggested crontab on the VPS (TZ <SERVER_TZ> = UTC+9; 07:15 and 23:15 server-TZ =
06:15 and 22:15 Manila time, catching the DA evening posts — install with
`crontab -e`; `deploy.sh` prints this too and never edits crontab itself):

```
<CRON_LINE_REMOVED>
<CRON_LINE_REMOVED>
```

`scripts/daily.sh` is the daily job: log rotation, `flock`, pull, `presyo
ingest` / `fx` / `export`, then commit + push `data/json` and
`site/public/data` (skipping the commit when only `generated_at` changed).
Test it with `scripts/daily_test.sh` (fake binary + temp bare repo, no
network).
