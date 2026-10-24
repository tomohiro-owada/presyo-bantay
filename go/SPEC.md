# Presyo Bantay — Go port spec (for opencode)

Goal: replace the Python daily pipeline on the VPS with ONE static Go binary
(`CGO_ENABLED=0`), minimal dependencies (supply-chain risk). Module path:
`github.com/tomohiro-owada/presyo-bantay/go`, rooted at `go/` in the repo.

## Hard rules
- Work ONLY inside `/workspace/presyo-bantay/go/`. NEVER modify anything else in
  the repo (Python files, tests/, data/, README, .gitignore). Another worker is
  editing the Python side concurrently.
- NEVER open `/workspace/presyo-bantay/data/presyo.db` for writing, and never
  run anything that writes it (a Python backfill process is writing it). For
  any DB testing use the scratch copy `/workspace/presyo-scratch/presyo.db`
  (or copies of it under /workspace/presyo-scratch/). PDFs in
  `/workspace/presyo-bantay/data/raw/*.pdf` may be READ.
- Do not make network requests to da.gov.ph during development/tests (use
  saved HTML fixtures / httptest). Frankfurter may be hit at most once manually.
- Do NOT run git commit/push; the orchestrator commits.
- Python reference code (read it, port its behaviour exactly):
  `presyo_bantay/{pdfparse,listing,sanity,db,aliases,fx,export_site,backfill,cli}.py`
  and tests in `tests/`. Run Python only as `.venv/bin/python` from the repo
  root, read-only.

## Dependencies
- Go stdlib preferred everywhere (net/http, encoding/json, regexp, flag, ...).
- SQLite: `modernc.org/sqlite` (pure Go) via `database/sql`.
- PDF: pure Go preferred. Candidate: `github.com/ledongthuc/pdf` (no transitive
  deps). Avoid heavy libs (pdfcpu pulls many deps) unless necessary. Shelling
  out to poppler is the last-resort fallback. Any extra dep must be justified
  in go/README.md.
- HTML listing: do NOT add golang.org/x/net/html; a regexp over `<a ... href="...">`
  is sufficient (match bs4 `find_all("a", href=True)` semantics: href attr,
  single/double quoted, case-insensitive, urljoin against base via net/url).

## Layout
```
go/
  go.mod  go.sum  README.md  SPEC.md  deploy.sh
  cmd/presyo/main.go            # subcommands
  internal/pdftext/             # PDF -> chars/words with positions + rects
  internal/table/               # pdfplumber-like "lines" table extraction
  internal/parse/               # port of pdfparse.py (+ sanity.py, backfill checks)
  internal/listing/             # port of listing.py
  internal/store/               # SQLite: schema, ingest_payload (+aliases), queries
  internal/aliases/             # port of aliases.py canonicalize()
  internal/fx/                  # port of fx.py
  internal/export/              # port of export_site.py
  tools/parity/                 # parity harness (Go side) + ref_dump.py (Python side)
  bin/presyo                    # build output (gitignored)
```

## PDF parsing (hardest part)
Python uses pdfplumber `page.extract_tables()` with DEFAULT settings
("lines" strategy) on pages 1-3. Table borders in these PDFs are drawn as
filled/stroked rectangles (`re` ops; pdfplumber sees 0 lines, ~60-210 rects,
0 curves per page). Cells: row 0 title, row 1 commodity headers (multi-line,
joined by "\n"), then market rows. Cells spanning merged areas are `None`.
To reproduce: interpret page content streams (apply CTM `cm`, q/Q, text
matrices, font ToUnicode/encodings), collect chars with bounding boxes and
rect edges, then implement pdfplumber's algorithm: rect edges -> snap
(tolerance 3) -> join (3) -> intersections (3) -> cells -> tables; cell text =
chars whose center is inside the cell, grouped into lines (y tolerance 3),
words split on x gap > 3, lines joined by "\n". Only the cell text and grid
structure need to match for our parser (e.g. "White Onion\n(Local)", empty
string vs None cells, the spurious None column next to Tomato in range PDFs).
Also implement `page.extract_text()` equivalent enough for
`parse_date_from_text` (date hint), and parse_date_from_filename.

Output for a PDF: layout ("single"/"range"), date, markets map
`{market: {commodity: {spec, unit, price, price_low, price_high}}}` exactly
like Python `parse_pdf_with_layout` + filename date.

## Parity (success criterion)
`tools/parity/ref_dump.py` (run with `.venv/bin/python` from repo root, imports
`presyo_bantay.pdfparse`) writes, for each PDF in data/raw, a JSON lines file
of rows `{file, layout, date, market, commodity, spec, unit, price, price_low,
price_high}` (or `{file, error}` when Python raises). The Go harness
(`go run ./tools/parity -raw ../data/raw -ref /workspace/presyo-scratch/ref.jsonl`)
produces the same rows from Go and prints match rate + every diff. Floats
compare exactly after rounding to 2 decimals; null must equal null. Target
100%.

## Subcommands (cmd/presyo)
Common flags: `-db` (default `data/presyo.db`), `-raw` (default `data/raw`),
`-json-dir` (default `data/json`).
- `parse PDF [-out-dir data/json]`: same JSON as Python `cmd_parse`
  (`{"date","source_url","layout","markets"}`, indent 2, non-ASCII unescaped,
  source_url from `<pdf>.meta.json` if present). Strict sanity like Python.
- `ingest` (daily): GET listing https://www.da.gov.ph/price-monitoring/ ->
  find_all_from_html -> candidate dates newer than MAX(prices.date) in DB
  (plus any missing date within the last `-lookback 7` days), newest first,
  at most `-max 3` downloads per run. Cached PDFs in raw dir are not
  re-downloaded. Download -> write PDF + `.meta.json` -> parse -> backfill-style
  checks (hard failure = skip & non-zero exit at end; warnings printed) ->
  write data/json/<date>.json -> ingest_payload (idempotent upsert, alias
  canonicalization exactly like db.py/aliases.py, market city via
  derive_city, commodity category via categorize_commodity). Flags:
  `-dry-run`, `-listing-file` (offline HTML), `-pdf FILE` (ingest one local PDF,
  no network). Also `ingest-json PATH` to load existing JSON snapshots.
- `fx`: port fx.py (Frankfurter `https://api.frankfurter.dev/v1/{start}..{end}?from=USD&to=PHP`,
  yearly chunks, 2s between chunks, source 'ECB via Frankfurter', table
  fx_rates exactly as schema.sql; default since = day after latest fx date).
- `export [-out site/public/data]`: port export_site.py byte-for-byte
  (compact JSON, same keys/ordering/rounding). Verify by diffing against the
  Python output on the scratch DB.
- Schema: embed a copy of presyo_bantay/schema.sql (go:embed) and apply with
  CREATE IF NOT EXISTS; also the price_low/price_high column migration.

## HTTP politeness (DA)
- UA: `PresyoBantay/0.1 (non-profit community price watch; https://github.com/tomohiro-owada/presyo-bantay)`
- >= 10 s (+ random 0-5 s jitter) between ANY two requests to da.gov.ph;
  timeout 30 s; honor Retry-After (cap 600 s); backoff on 429/5xx; at most 3
  attempts per file. robots.txt disallows *.pdf, so only fetch the newest
  missing dates; never crawl.

## Tests
Port key pytest cases to Go `_test.go` (parse_price, parse_range_price,
normalize_commodity, infer_spec, infer_unit, filename/text dates, listing
selection incl. Revised/suffix/exclusions, sanity checks, aliases
canonicalize, db idempotent ingest, fx chunking/upsert via httptest, export
format). `go vet ./... && go test ./...` must pass.
