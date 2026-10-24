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
mocked in fetch tests).

## Daily automation

`.github/workflows/daily.yml` runs every day at 06:00 Asia/Manila
(`0 22 * * *` UTC): it installs dependencies, runs pytest, runs
`python -m presyo_bantay fetch --parse`, ingests snapshots with
`python -m presyo_bantay ingest data/json`, and commits new
`data/json/*.json` files plus `data/presyo.db` with the github-actions bot
identity.

## Weekly automation

`.github/workflows/weekly.yml` runs every Sunday at 08:00 Asia/Manila
(`0 0 * * 0` UTC, plus `workflow_dispatch`): it installs dependencies, runs
pytest, builds the summary with
`python -m presyo_bantay summarize --week-ending $(TZ=Asia/Manila date +%F) --db data/presyo.db --out summary.html`,
and posts it with `post-telegram --file summary.html` using the
`TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` secrets (LLM secrets optional,
commented out). Permissions: `contents: read`.

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

Presyo Bantay（プレショ・バンタイ）は、フィリピン農業省（DA）が公表する日次価格モニタリング PDF を収集・解析し、ラス・ピニャス周辺の小売価格を JSON として記録する非営利の市民ツールです。最新の `Price-Monitoring` PDF を取得し、主要4市場（New Las Piñas、Alabang、La Huerta、Bicutan）のコメ・バングス・卵などの価格を抽出します。レイアウト変化を検出する sanity check と、毎日 06:00（マニラ時間）に実行される GitHub Actions ワークフローを備えています。ライセンスは MIT です。

第2週の追加機能：スナップショットを SQLite（`data/presyo.db`）に取り込む `ingest`、価格推移を表示する `trend`、週次タガログ語サマリーを作成する `summarize`（HTML／MarkdownV2 対応、数値検証つきの任意 LLM 言い換えフックあり）、Telegram チャンネルへ投稿する `post-telegram`、毎週日曜 08:00（マニラ時間）にサマリーを投稿する `weekly.yml` ワークフローを追加しました。過去の `Price-Monitoring` PDF を丁寧に一括取得する `backfill`（順次取得・間隔確保・再開可能・`data/backfill/state.json` に進捗記録）もあります。Telegram のセットアップは BotFather でボットを作成し、チャンネルの管理者に登録して、トークンとチャット ID を GitHub Secrets（`TELEGRAM_BOT_TOKEN`、`TELEGRAM_CHAT_ID`）に保存します。トークンをリポジトリにコミットしてはいけません。テストは `tests/fixtures/synthetic/` の合成データのみを使い、ネットワークには一切接続しません。
