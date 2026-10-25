# Presyo Bantay — "what is a good deal right now?" (advice v3, single spec)

Goal: tell household shoppers in Las Piñas what is a good deal RIGHT NOW,
on a LOCAL basis. Deterministic Go code computes ALL numbers
(signals.json). An LLM (opencode, model
`opencode-go/muse-spark-1.3-contributor`) rewrites a deterministic template
into a more natural short daily text. A Go checker verifies every peso
amount / percentage in the text exists in (or is derivable from)
signals.json, plus a jargon lint over the text.

Distribution: (1) a "Sulit ngayon / Today's deals / 今日のお買い得" card on
the static site backed by `site/public/data/deals.json`, (2) later
auto-posting to a Facebook Page (NOT now — only make the text FB-ready).

There are no substitution suggestions and no weekly ulam plans in v3.

## Hard rules
- Work ONLY inside `go/` AND `site/` (`src`,
  `index.html`, `public/data/deals.json` only — do NOT regenerate or modify
  other files under `site/public/data`, they are tracked data). Never open
  `../data/presyo.db`; use the read-only snapshot `/tmp/presyo-snap.db`.
  Open SQLite read-only (`file:...?mode=ro` DSN with modernc.org/sqlite).
- No new Go deps; no new npm deps; no CDN, no tracking, no external
  requests from the site. No git commits. Do not run `advise` (the
  orchestrator runs it). Do not touch the VPS or deploy anything.
- Existing tests must keep passing: `cd go && go test ./... && go vet
  ./...`; `bash go/scripts/daily_test.sh`; `cd site && npm test &&
  npm run build`.
- Reuse `internal/aliases.Canonicalize` to merge commodity name aliases;
  skip the "other" category.

## DB facts (/tmp/presyo-snap.db)
Tables: markets(id,name,city), commodities(id,name,category,spec,unit),
prices(date,market_id,commodity_id,price,price_low,price_high),
fx_rates(date,base,quote,rate,source) with base='USD', quote='PHP'.
`price` may be NULL; then use (price_low+price_high)/2 if both present, else
skip. Dates are 'YYYY-MM-DD' text. Latest price date 2026-10-23.
Focus market: "New Las Piñas City Public Market". Nearby markets: Alabang
Central Market, La Huerta Market/Parañaque, Bicutan Market, Pasay City Market.
Price per row = price, else midpoint of low/high, else missing.

## 1. `presyo signals` subcommand + `internal/signals` package
Flags: `-db`, `-date` (default latest), `-focus`, `-nearby` (comma list),
`-deal-threshold` (default 8.0), `-max-deals` (default 8), `-out`.
(`-fx-window` accepted but unused: fx is context only.)

Per canonical commodity (skip "other"): basis `las_pinas` / `nearby_median`
(same rule as v2), `price`, `median_30d`, `pct_vs_median_30d` (1 dp),
`diff_vs_median_30d` (2 dp), `trend_7d_pct` + base, `market_prices`
(short names Las Piñas/Alabang/La Huerta/Bicutan/Pasay), `unit`.
- `cheapest_nearby` {market, price, diff_vs_basis, days_cheaper,
  days_checked}: the as-of cheapest among the 5 (needs >= 3 markets on
  as-of, strictly cheaper than basis on as-of) must ALSO be strictly cheaper
  than the basis price on >= 2 of the last 3 basis report dates
  (dates <= as-of with a basis price; as-of counts). `days_checked` = 3 or
  fewer (require >= 2), else null.
- `sharp_drop_this_week` (commodity + deal): pct < 0 and trend_7d_pct <= -10
  and <= 0.8 * pct_vs_median_30d. `sharp_rise_this_week` analogously for
  pct > 0 / trend >= +10 / >= 0.8 * pct.
- Top-level `uses_nearby_basis`: true iff any item in `deals` or `pricier`
  has basis `nearby_median`. Pricier entries never carry `cheapest_nearby`.
Deals/pricier selection, ranking, top-N and thresholds as in v2.

## 2. Deterministic template texts (`internal/advice/template.go`)
`RenderTemplate(signals) -> Texts{TL, EN, JA}` from signals only (the LLM
fallback; must itself pass Check+lint, incl. empty deals). Plain FB-ready
text, title with one emoji, `• ` bullets, per-piece units (`/pc`, `/piraso`,
`1個`; JA unit first `1kg ₱130`), sharp wordings, cheapest and
`(kalapit na palengke)` / `(nearby markets)` / `（近隣市場）` markers only
on nearby-basis items, pricier max 3 lines, nearby footnote only when
`uses_nearby_basis`, empty-deals lines. EN = canonical without `(Local)`;
TL/JA from the single name table in Go shared by template, prompt hints
and checker aliases. Market JA: ラス・ピニャス, アラバン, ラ・ウエルタ,
ビクタン, パサイ.

## 3. Prompt + advise
Prompt: same three `## Taglish` / `## English` / `## 日本語` headers
(headers only for splitting); bodies are plain FB-ready text in exactly the
template line structure (the rendered template is the skeleton; the model
may only reword connecting words, never add/remove/change numbers, items
or markets). Japanese uses 先週比 for 7-day numbers, 今週大きく下がった
for sharp drops, never "still falling". Omit the nearby footnote when
`uses_nearby_basis` is false.
advise: split into tl/en/ja. `text_source` = "llm" only when the call
succeeded, all 3 sections present and Check+lint passes; else "template"
with `fallback_reason`. Exit 0 in both cases (fallback warns); exit 1 only
on I/O errors. Flag `-no-llm` skips the call.
Lint additions: "still falling"/"still down"/"tuloy-baba"; nearby footnote
words (近隣市場, kalapit na palengke, nearby markets) when
`uses_nearby_basis` is false; markdown bodies (leading `#`/`|`, `**`,
`|` tables; `• ` and `- ` bullets allowed). JA latin allowance: `kg`, `DA`.
advise writes: prompt.txt, advice.md, run.json (+ text_source,
fallback_reason), check.json (incl. `fb_post` results), deals.json,
fb_post.txt.

## 7. Combined Facebook post
One combined FB Page post text with all three languages: Taglish first,
`———` separator, English, separator, Japanese, separator, then a final
block with `https://abalol.xyz/presyo-bantay/` and
`Source/Pinagmulan/出典: DA (Department of Agriculture) price monitoring`.
Plain text, emoji sparingly as section markers only (🛒 deals title,
📈 pricier header). Per language at most 5 deals + 2 pricier lines; the
per-language closing source line is replaced by the shared footer.
- Go: `RenderFBPost(texts)` in `internal/advice/fbpost.go`, deterministic
  trim/join of the chosen texts (LLM or template), never inventing content.
- Written as `fb_post.txt` by advise (out-dir) and `deals` (next to -out),
  and as field `fb_post` in deals.json.
- Checked: `CheckFBPost` wraps each language block as its section and runs
  Check + lint (the number-free shared footer is excluded — it would trip
  the Japanese-section Latin lint); results live under check.json's
  `fb_post`. `FBPostAndCheck` falls back to the template-derived post when
  the check fails.

## 4. deals.json for the site
`presyo deals -signals FILE [-advice-dir DIR] -out FILE` (advise also
writes it): {as_of, generated_at (RFC3339 Asia/Manila;
PRESYO_GENERATED_AT overrides), text_source, model (llm only),
texts: {tl,en,ja}, deals, pricier, uses_nearby_basis, thresholds}.
With `-advice-dir` reuse llm texts only when run.json says "llm" with a
passing check.json and advice.md; else RenderTemplate. Stable key order,
indent 2, non-ASCII unescaped.
Cron hook in `go/scripts/daily.sh` after export, gated by `PRESYO_DEALS=1`
(default off): signals + advise (`-no-llm` unless `PRESYO_DEALS_LLM=1`) into
`$PRESYO_HOME/advice/<date>`, copy deals.json to
`$REPO/site/public/data/deals.json` for the existing commit step. Deals
failures never fail ingest/export (log + record rc). `OPENCODE_API_KEY`
must be in the cron environment (mode-600 env file sourced by the cron
line); without it opencode fails opaquely and the template is used.

## 5. Site card (site/)
- Load `data/deals.json` (same fetchJson helper). Missing/invalid hides the
  card silently.
- Card after header/intro, before charts: title "Sulit ngayon" /
  "Today's deals" / "今日のお買い得", as-of date, current-language text as
  plain lines (textContent, never innerHTML), `• ` lines as a list. Muted
  footer "Awtomatikong buod ..." + "(AI-assisted, numbers checked)" variant
  when text_source is llm.
- Language toggle adds 日本語; full ja STRINGS table (dates ja-JP); tl
  default; Lang = "tl"|"en"|"ja". `npm test` + `npm run build` pass.
  Styling matches existing CSS, works at 390px. Vitest test for the
  text-to-lines function.

## Docs
`go/README.md` documents signals/advise/advice-check/deals, the template
fallback, and the `OPENCODE_API_KEY` cron note.
