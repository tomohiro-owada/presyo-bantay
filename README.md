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

Notes:

- Only `Price-Monitoring-{Month}-{D}-{YYYY}.pdf` links are considered (with an
  optional `Revised-` prefix, `-N` suffix, or `-Revised` suffix).
  `Daily-Cigarette-Price-Monitoring-*`, `Daily-Price-Index-*`, and
  `Weekly-Average-Prices-*` links are excluded.
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
`python -m presyo_bantay fetch --parse`, and commits new
`data/json/*.json` files with the github-actions bot identity.

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
