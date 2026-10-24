# Presyo Bantay

A non-profit civic tool for Las Piñas that tracks retail prices published by
the Philippine Department of Agriculture (DA) in its daily Price Monitoring
reports. It downloads the latest per-market price table, parses it into JSON,
and keeps a growing history under `data/json/`.

Data source: <https://www.da.gov.ph/price-monitoring/> (DA Agribusiness and
Marketing Assistance Service). This project is not affiliated with the DA.

## Requirements

- Python 3.12+
- See `requirements.txt` (runtime) and `requirements-dev.txt` (tests)

```bash
python -m venv .venv
.venv/bin/pip install -r requirements.txt -r requirements-dev.txt
```

## Usage

Fetch the latest Price-Monitoring PDF (downloads only if not cached):

```bash
python -m presyo_bantay fetch
python -m presyo_bantay fetch --parse          # fetch, then parse to JSON
python -m presyo_bantay fetch --listing-file tests/fixtures/price-monitoring-listing.html
```

Parse a PDF to JSON:

```bash
python -m presyo_bantay parse data/raw/Price-Monitoring-October-23-2026.pdf
python -m presyo_bantay parse file.pdf --source-url https://www.da.gov.ph/.../file.pdf
python -m presyo_bantay parse file.pdf --markets-of-interest   # only the 4 focus markets
python -m presyo_bantay parse file.pdf --out-dir data/json
```

Output JSON shape:

```json
{
  "date": "2026-10-23",
  "source_url": "https://www.da.gov.ph/.../Price-Monitoring-October-23-2026.pdf",
  "markets": {
    "New Las Piñas City Public Market": {
      "Premium Rice (Imported)": {"spec": "Imported", "unit": "per kg", "price": 59.0},
      "Bangus": {"spec": null, "unit": "per kg", "price": 270.0},
      "Egg (Medium)": {"spec": "Medium", "unit": "per piece", "price": 8.5}
    }
  }
}
```

## SQLite store

Snapshots are also kept in a plain-SQLite database (`data/presyo.db`,
schema in `presyo_bantay/schema.sql`) with `markets`, `commodities`, and
`prices` tables (`NULL` price = NOT AVAILABLE).

```bash
python -m presyo_bantay ingest data/json                 # ingest a dir (or one file)
python -m presyo_bantay ingest data/json --db data/presyo.db
python -m presyo_bantay ingest tests/fixtures/synthetic --allow-synthetic  # tests only
python -m presyo_bantay trend --market 'Las Piñas' --item Bangus
python -m presyo_bantay trend --market 'Las Piñas' --item Bangus --days 90 --json
```

`ingest` is idempotent (re-ingesting overwrites via upsert) and refuses
payloads flagged `"synthetic": true` unless `--allow-synthetic` is given.
`trend` matches market/item by case-insensitive substring and lists
candidates when the match is ambiguous or missing.

Commodity names are canonicalized on ingest (`presyo_bantay/aliases.py`):
typos and qualifier variants that clearly mean the same item and spec
collapse to one series (`Premium Rice (Loca)` → `Premium Rice (Local)`,
`PREMIUM RICE (Imported, 5% broken)` → `Premium Rice (Imported)`, pure
case/whitespace/`*` variants generally). Names that differ in spec stay
separate but carry a `group` (`pork_kasim`, `pork_liempo`, `sugar`) so the
site can show them as related series.

```bash
python -m presyo_bantay normalize-commodities --db data/presyo.db --dry-run
python -m presyo_bantay normalize-commodities --db data/presyo.db
```

`normalize-commodities` is an idempotent DB migration for snapshots
ingested before canonicalization existed: it moves each alias row's prices
to the canonical commodity row (an existing non-null price wins, otherwise
the alias price is taken), deletes the alias row, and commits per alias
with a 30 s busy timeout. `--dry-run` only prints the merges.

## USD/PHP reference rates

```bash
python -m presyo_bantay fx-update --db data/presyo.db
python -m presyo_bantay fx-update --db data/presyo.db --since 2026-10-01 --until 2026-10-23
```

`fx-update` fetches daily USD/PHP rates from the keyless Frankfurter API
(ECB reference rates) into the `fx_rates` table (`source='ECB via
Frankfurter'`). Defaults: `--since` is the day after the latest stored FX
date (or the earliest price date when empty), `--until` is today
(Asia/Manila). One request per run; ranges over a year are split into
yearly chunks with a 2 s delay. If `--since` is after `--until` nothing is
downloaded. ECB publishes business days only, so weekends resolve as-of the
previous business day (`fx.fx_asof`).

Attribution: "USD/PHP: European Central Bank reference rates via
Frankfurter (frankfurter.dev)".

## Price/FX correlation

```bash
python -m presyo_bantay correlate --market 'Las Piñas' --item Bangus
python -m presyo_bantay correlate --market 'Las Piñas' --item Bangus --days all --lags 0,7,14,30 --json
```

`correlate` prints the Pearson `r` and sample count `n` per lag: the price
on date *d* is paired with the FX rate as-of *d − lag* (last available
rate, skipped when more than 5 days stale). `r` is `None` with fewer than 3
pairs or zero variance. Market/item matching is fuzzy, like `trend`.

Note: correlation ≠ causation — a high `r` only says the two series moved
together over the window, not that FX moves caused the price moves.

## Site data export

```bash
python -m presyo_bantay export-site --db data/presyo.db --out site/public/data
```

`export-site` writes compact JSON for the static site (no indentation,
short keys, prices rounded to 2 decimals; aliases canonicalized at export
so the output is correct even before `normalize-commodities` has run):

- `meta.json`: latest price date, generation time, markets (`id`, `name`,
  `short`, `city`), commodities (`id`, `name`, `unit`, `category`,
  `group`), the 4 default market ids (Las Piñas, Alabang, La Huerta,
  Bicutan), FX/price attributions, date range.
- `series/c<commodity_id>.json`: `{d: [...dates...], m: {market_id: [...]}}`
  per commodity (all markets; the site fetches only the selected item).
- `fx.json`: `{d: [...], r: [...]}` USD/PHP series.
- `week.json`: per commodity, per market latest price, ~7-day-earlier
  baseline (5–10 day window), change %, and cheapest market among the
  default 4 and among all (reuses `summary.py` logic).

## Website

The public static site lives in `site/` (plain Vite + vanilla TypeScript +
uPlot for charts: small bundle, no frameworks, no web fonts, no external CDNs
at runtime, no tracking). Chart data (`site/public/data/`, output of
`presyo export`, ~3 MB compact JSON) is committed to git: the VPS exports
and pushes it daily, and both local builds and GitHub Pages build straight
from it.

Local dev (never write to `data/`; use the scratch copy):

```bash
cp data/presyo.db .scratch/x.db   # refresh the scratch copy when needed
.venv/bin/python -m presyo_bantay export-site --db .scratch/x.db --out site/public/data
cd site && npm install && npm run dev
```

Build and test:

```bash
make site        # npm ci + vite build -> site/dist (uses committed site/public/data)
make test        # pytest + site vitest suite (stats.ts unit tests)
```

## Publishing / GitHub Pages

Live site: <https://tomohiro-owada.github.io/presyo-bantay/>

Repo: <https://github.com/tomohiro-owada/presyo-bantay> (default branch `main`).

In the repo, go to Settings > Pages > Source and select "GitHub Actions".

`.github/workflows/pages.yml` (workflow "pages") builds `site/dist` with
plain Node (`cd site && npm ci && npm run build`) from the committed
`site/public/data/` and deploys it with `actions/deploy-pages`. It runs on
push to `main` touching `site/**`, `data/json/**` or the workflow itself,
plus manual `workflow_dispatch`. No Python, no DB: the VPS already exported
and committed the site data.

## Weekly summary

```bash
python -m presyo_bantay summarize --week-ending 2026-10-24 --db data/presyo.db --out summary.html
python -m presyo_bantay summarize --week-ending 2026-10-24 --db data/presyo.db --format markdownv2
python -m presyo_bantay summarize --week-ending 2026-10-24 --data-dir data/json --out summary.html
python -m presyo_bantay summarize --week-ending 2026-10-24 --db data/presyo.db --out summary.html --llm
```

`summarize` picks the newest snapshot on or before `--week-ending` and
compares it with the snapshot closest to 7 days earlier (5–10 day window;
without one it reports prices with no week-over-week changes). It prints a
short Taglish message (< 1500 characters) headed
`🛒 Presyo Bantay — linggo hanggang Okt 24, 2026` with Las Piñas prices
(unavailable items show `wala`), cheapest market per item (short labels:
Las Piñas, Alabang, La Huerta, Bicutan), notable moves (≥ 5 %), the
`Source: DA Bantay Presyo (da.gov.ph)`
line, and a unit/stall disclaimer. A `⚠️ SYNTHETIC TEST DATA` line is shown
when either the latest or the baseline snapshot is synthetic. `--data-dir`
ingests JSON files into an in-memory DB instead of reading `--db`. `--llm`
enables the optional
OpenAI-compatible rephrase hook (needs `PRESYO_LLM_BASE_URL`,
`PRESYO_LLM_API_KEY`, `PRESYO_LLM_MODEL`); its output is number-validated and
falls back to the template on any failure. Without `--llm` (the default) no
network request is ever made.

## Historical backfill

A polite one-time backfill of older per-market PDFs (owner-approved):

```bash
python -m presyo_bantay backfill --dry-run --limit 5   # preview, no PDF requests
python -m presyo_bantay backfill --listing-file tests/fixtures/price-monitoring-listing.html --dry-run
python -m presyo_bantay backfill --since 2024-01-01 --until 2024-12-31
python -m presyo_bantay backfill --limit 50 --delay 10
```

Politeness: the listing page is fetched fresh ONCE per run, requests are
strictly sequential with `--delay` seconds between them (default 10, minimum
5 enforced) plus a random 0–5 s jitter between ANY two HTTP requests, files
already cached under `data/raw/` are skipped without any request, and each
downloaded PDF is parsed, sanity-checked, written to `data/json/` and
ingested into `data/presyo.db` immediately. Only one PDF per report date is
kept (best variant: `Revised`/`Updated` beats plain, higher `-N` wins),
newest first.

Resume / progress: per-file status (`ok` / `failed` / `skipped`, timestamp,
error) is saved to `data/backfill/state.json` after EVERY file, so
re-running the same command resumes where it left off (files marked `failed`
are skipped too unless `--retry-failed` is given). HTTP 429/5xx responses
honor `Retry-After` (capped at 10 min) with exponential backoff from 60 s;
after 3 consecutive errors a file is marked failed, and after 5 consecutive
failed files the run stops with a non-zero exit. Unparseable PDFs are
recorded in `data/backfill/failures.jsonl` (with page/header debug info) and
never written as JSON nor ingested; old files missing a market of interest
are only a warning in the state file. Every run prints a per-file progress
line plus a final `listed / selected / skipped-cached / downloaded / ok /
failed` summary, and appends both to `data/backfill/backfill.log`
(ignored by git; `state.json` and `failures.jsonl` may be committed).

## After the historical backfill finishes

The backfill runs on this box (writing `data/json/*.json` plus the local
`data/presyo.db`). The VPS cron keeps running meanwhile: it only ingests
dates newer than its DB, while the backfill fills older dates into the box's
own DB, so they never touch the same file. Pause cron only for the merge:

1. On the VPS, pause cron (keep a backup to resume later):
   `crontab -l > ~/crontab.bak && crontab -r`
2. Copy the backfilled snapshots from this box to the VPS repo, e.g.
   `scp data/json/*.json <VPS_HOST>:repo/data/json/` (or a staging dir).
3. On the VPS, load them into the authoritative DB (idempotent upsert):
   `presyo ingest-json -db $HOME/data/presyo.db <dir>`
   (alternatively `sqlite3` with `ATTACH` + `INSERT OR IGNORE`; note the VPS has
   no `sqlite3` CLI installed and installing it needs root, so prefer `ingest-json`).
4. Run `$HOME/bin/daily.sh` once on the VPS so it exports the full
   site data and pushes one `data: YYYY-MM-DD` commit.
5. On the VPS, resume cron: `crontab ~/crontab.bak`.

## Telegram posting

```bash
python -m presyo_bantay post-telegram --file summary.html --dry-run   # no network, no token needed
python -m presyo_bantay post-telegram --file summary.html --parse-mode HTML
python -m presyo_bantay post-telegram --file summary.html --parse-mode MarkdownV2
```

Setup (do this once; never commit the token):

1. Talk to `@BotFather` on Telegram, run `/newbot`, and copy the bot token.
2. Create a channel (e.g. `@presyobantay`), add the bot as a member, then
   promote it to admin so it can post.
3. Get the chat id: forward a channel post to `@userinfobot`, or use
   `@getmyid_bot`; channel ids look like `-1001234567890` (or use
   `@channelname`).
4. For local runs, export `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID`.
5. For GitHub Actions, store them as repository secrets
   (`Settings → Secrets and variables → Actions`): `TELEGRAM_BOT_TOKEN`,
   `TELEGRAM_CHAT_ID` (optional LLM secrets: `PRESYO_LLM_BASE_URL`,
   `PRESYO_LLM_API_KEY`, `PRESYO_LLM_MODEL`). Workflows reference them via
   `${{ secrets.… }}` so the values never appear in logs or the repo.

Notes:

- Only `Price-Monitoring-{Month}-{D}-{YYYY}.pdf` links are considered (with an
  optional `Revised-`/`Updated-` prefix, `-N` suffix, or `-Revised` suffix).
  `Daily-Cigarette-Price-Monitoring-*`, `Daily-Price-Index-*`,
  `Weekly-Average-Prices-*`, `Price-Watch-*`, and `RETAIL-*` links are
  excluded.
- The source URL is never guessed: `parse` uses `--source-url` when given,
  otherwise it reads `data/raw/<filename>.meta.json` written by `fetch`,
  otherwise it stores `null`.
- Egg is reported `per piece` (the PDF footnotes `* per piece`); everything
  else in the per-market tables is `per kg`. `NOT AVAILABLE`/blank becomes
  `null`.
- After parsing, sanity checks verify at least 20 markets with at least 20
  commodities each, that all 4 markets of interest resolve
  (New Las Piñas City Public Market, Alabang Central Market, La Huerta,
  Bicutan), and that core commodities (rice types, Bangus, Egg) exist.
  Failures print `Layout may have changed: ...` to stderr and exit with code 2.

## Markets of interest

- New Las Piñas City Public Market
- Alabang Central Market
- La Huerta (Parañaque)
- Bicutan

Names are matched by case-insensitive substring since the PDF wording varies
slightly (e.g. `La Huerta Market/Parañaque`, `Bicutan Market`).

## Tests

```bash
.venv/bin/python -m pytest -q
```

Tests use only the saved fixtures in `tests/fixtures/` (no network; HTTP is
mocked in fetch and FX tests).

## Daily automation (VPS)

Ingest runs on the VPS (user `presyo`, non-root, Ubuntu x86_64,
TZ <SERVER_TZ>), which is the ONLY writer of data. Layout:
`$HOME/bin/presyo` (Go binary), `$HOME/bin/daily.sh`,
`$HOME/data/presyo.db` (authoritative DB, NOT in git),
`$HOME/data/raw` (PDF cache), `$HOME/logs`,
`$HOME/repo` (git clone of this repo over SSH via the
`git@<DEPLOY_KEY_HOST>:tomohiro-owada/presyo-bantay.git` remote, using a deploy
key scoped to this repo only).

Cron (07:15 and 23:15 server-TZ = 06:15 / 22:15 Manila, catching the DA evening
posts):

```cron
<CRON_LINE_REMOVED>
<CRON_LINE_REMOVED>
```

`daily.sh` (see `go/scripts/daily.sh`, covered by
`go/scripts/daily_test.sh`): rotates `logs/daily.log` past 5 MB, takes a
`flock`, `git pull --rebase` (continues on failure), then runs
`presyo ingest`, `presyo fx`, `presyo export`, recording each exit code
without aborting. Change detection: if the only diff is `generated_at` in
`site/public/data/meta.json` (and nothing under `data/json`), it restores
`meta.json` and treats the run as no-change; otherwise it commits
`data/json` + `site/public/data` as `data: YYYY-MM-DD` (Manila date),
`git pull --rebase`s and pushes (one retry). It exits non-zero if any step
failed, but always pushes whatever succeeded. It never adds
`data/presyo.db` or raw PDFs, never uses sudo, and never writes outside
`$PRESYO_HOME`.

`data/presyo.db` is NOT committed: it is a ~20-25 MB binary blob rewritten
daily, and git history keeps every old copy forever, so tracking it would
bloat the repo. The authoritative DB lives on the VPS; what IS committed is
the exported site data (`site/public/data/`, ~3 MB compact JSON) plus the
daily snapshots (`data/json/*.json`), which GitHub Pages builds from.

Ship binary + script updates with `cd go && ./deploy.sh` (`DRY_RUN=1` to
preview; it never touches crontab itself).

## Weekly automation

`.github/workflows/weekly.yml` runs every Sunday at 08:00 Asia/Manila
(`0 0 * * 0` UTC, plus `workflow_dispatch`): it installs dependencies, runs
pytest, builds the summary with
`python -m presyo_bantay summarize --week-ending $(TZ=Asia/Manila date +%F) --data-dir data/json --out summary.html`,
and posts it with `post-telegram --file summary.html` using the
`TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` secrets (LLM secrets optional,
commented out). `--data-dir` ingests the committed JSON snapshots into an
in-memory DB, so no `data/presyo.db` is needed. Permissions:
`contents: read`.

## Synthetic test fixtures

`tests/fixtures/synthetic/` holds a self-contained 2-snapshot data dir used
by the offline test suite: the real `2026-10-23.json` copy plus a clearly
labelled synthetic `2026-10-16.json` (`"synthetic": true`,
`source_url: null`). Regenerate it with:

```bash
.venv/bin/python tests/fixtures/synthetic/make_synthetic.py
```

Synthetic files must never be placed under `data/json/`.

## Polite fetching

The DA site's `robots.txt` disallows `*.pdf` crawling, so be considerate:
this tool makes one listing-page request plus at most one PDF request per
run, skips the PDF download when the file is already cached under
`data/raw/`, and identifies itself with a descriptive `User-Agent`
(`PresyoBantay/0.1 ...`). Prefer reusing the saved fixtures in
`tests/fixtures/` for development instead of repeatedly hitting the site.

## License

MIT — see `LICENSE`. Copyright 2026 Presyo Bantay contributors.

## 日本語概要

Presyo Bantay（プレショ・バンタイ）は、フィリピン農業省（DA）が公表する日次価格モニタリング PDF を収集・解析し、ラス・ピニャス周辺の小売価格を JSON として記録する非営利の市民ツールです。最新の `Price-Monitoring` PDF を取得し、主要4市場（New Las Piñas、Alabang、La Huerta、Bicutan）のコメ・バングス・卵などの価格を抽出します。レイアウト変化を検出する sanity check と、VPS 上で cron（日本時間 07:15／23:15＝マニラ時間 06:15／22:15）により毎日実行される Go バイナリ＋daily.sh パイプラインを備えています。SQLite の DB（`data/presyo.db`）はリポジトリ肥大化を避けるため git 管理外とし、VPS 上の正本と、コミットされる JSON スナップショット（`data/json/`）＋サイト用データ（`site/public/data/`）で運用します。ライセンスは MIT です。

第2週の追加機能：スナップショットを SQLite（`data/presyo.db`）に取り込む `ingest`、価格推移を表示する `trend`、週次タガログ語サマリーを作成する `summarize`（HTML／MarkdownV2 対応、数値検証つきの任意 LLM 言い換えフックあり）、Telegram チャンネルへ投稿する `post-telegram`、毎週日曜 08:00（マニラ時間）にサマリーを投稿する `weekly.yml` ワークフローを追加しました。公開用ウェブサイト（`site/`、Vite + 素の TypeScript + uPlot、Taglish／英語切替つき）は `make site` で `site/dist` にビルドします。デプロイ先（Cloudflare Pages／GitHub Pages）は検討中です。過去の `Price-Monitoring` PDF を丁寧に一括取得する `backfill`（順次取得・間隔確保・再開可能・`data/backfill/state.json` に進捗記録）もあります。Telegram のセットアップは BotFather でボットを作成し、チャンネルの管理者に登録して、トークンとチャット ID を GitHub Secrets（`TELEGRAM_BOT_TOKEN`、`TELEGRAM_CHAT_ID`）に保存します。トークンをリポジトリにコミットしてはいけません。テストは `tests/fixtures/synthetic/` の合成データのみを使い、ネットワークには一切接続しません。
