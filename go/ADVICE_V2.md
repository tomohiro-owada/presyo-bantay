# Advice prototype v2 — "Ano ang sulit ngayon?" (deals), spec for opencode

User feedback on v1: NO substitution suggestions, NO weekly ulam plan. They
want to know what is a good deal RIGHT NOW (何がお買い得か), on a LOCAL basis.
This supersedes the substitution/ulam parts of ADVICE_SPEC.md; update
ADVICE_SPEC.md so it describes the v2 behaviour (keep it the single spec).

Hard rules: same as ADVICE_SPEC.md (only edit inside go/ — you may also
remove/replace test fixtures under go/; never open ../data/presyo.db, use
/tmp/presyo-snap.db read-only; no new deps; no git commits; do NOT run the
`advise` subcommand; do not modify ../prototype/).

## 1. signals (rewrite the output; drop substitutions entirely)
Remove `substitutions` and every substitution group/code/test. Remove the
NCR-wide fields from the output (ncr_median_latest, focus_vs_ncr_pct, NCR
trends, NCR flags, summary based on NCR). The JSON must contain ONLY
local-basis numbers so the LLM cannot mix bases. (Internally you may still use
all markets for nothing else; simply don't emit NCR numbers.)

Markets: focus = "New Las Piñas City Public Market" (`-focus`), nearby =
Alabang Central Market, La Huerta Market/Parañaque, Bicutan Market,
Pasay City Market (`-nearby`). Price per row = price, else midpoint of
low/high, else missing (same as now).

Per canonical commodity (skip "other" category as now):
- `basis`: "las_pinas" if the focus market has a price on the as-of date AND
  has >= 10 dates with a price in the 30-day window; otherwise
  "nearby_median" if at least 2 nearby markets have a price on the as-of date
  (daily series = median across the nearby markets that reported that day,
  requiring >= 2 markets that day); else the commodity is omitted.
- `basis_label`: human label — "Las Piñas" or "nearby markets (Alabang,
  La Huerta, Bicutan, Pasay)". This is the only basis wording.
- `price`: basis price on the as-of date.
- `median_30d`: median of the basis daily series over the 30 calendar days
  ending on as-of date (inclusive); `n_days_30d`.
- `pct_vs_median_30d` = (price - median_30d)/median_30d*100 (1 dp).
- `diff_vs_median_30d` = price - median_30d (pesos, 2 dp; negative = cheaper).
- `trend_7d_pct`: % change of basis series as-of vs the latest basis date
  <= as-of-7 days (null if none within 3 days of that target); include
  `trend_7d_base_date`, `trend_7d_base_value`.
- `market_prices`: map market short name -> price on as-of date for the 5
  markets (focus + nearby) that have one. Short names: "Las Piñas",
  "Alabang", "La Huerta", "Bicutan", "Pasay".
- `cheapest_nearby`: {market (short name), price, diff_vs_basis =
  price - basis price} = the minimum of market_prices, only if at least 3 of
  the 5 markets reported AND it is strictly cheaper than the basis price;
  else null.
- `unit` as now (per kg / per piece). Money 2 dp, pct 1 dp.

Deals: `deals` = commodities with pct_vs_median_30d <= -DEAL (default 8.0,
flag `-deal-threshold`) AND n_days_30d >= 10 AND (trend_7d_pct is null OR
trend_7d_pct <= +5 — i.e. not bouncing back up sharply). Rank by
pct_vs_median_30d ascending (most below usual first), ties by name; keep top 8
(`-max-deals`). Each deal entry: {rank, name, basis_label, unit, price,
median_30d, pct_vs_median_30d, diff_vs_median_30d, trend_7d_pct,
cheapest_nearby}.
Pricier: `pricier` = pct_vs_median_30d >= +DEAL, ranked descending, top 5:
{rank, name, basis_label, unit, price, median_30d, pct_vs_median_30d,
diff_vs_median_30d, trend_7d_pct}.
Also `thresholds`: {deal_pct, min_days, max_trend_7d_pct}.
Keep the fx object only as context (latest rate, 30d change); drop the
per-commodity fx correlations (not useful for deals).

Top level: {as_of, generated_by, focus_market, nearby_markets (short names),
thresholds, deals, pricier, commodities, fx}. Compact (< 20 KB).

Tests: update signals_test.go: local basis selection incl. fallback to nearby
median (focus missing, or focus with < 10 days), nearby median needs >= 2
markets, pct/diff, trend, cheapest_nearby (needs >= 3 markets and strictly
cheaper), deal threshold boundary (-8.0 included, -7.9 excluded), trend
filter, ranking & top-N, pricier list, no NCR fields emitted.

## 2. prompt (prompt.tmpl) — short daily Telegram post
Audience: household shoppers in Las Piñas; readable in 30 seconds.
Three sections, headers exactly `## Taglish`, `## English`, `## 日本語`,
output must start with `## Taglish`, no preamble. Each section:
- one line: title with date, e.g. "Sulit ngayon sa palengke (2026-10-23)".
- "Sulit ngayon" / "Good deals today" / "今日のお買い得" list from `deals`
  in rank order: one short line each: item, price per unit, how far below its
  usual 30-day price (pct and/or pesos), and if cheapest_nearby is present
  "pinakamura sa <market>: ₱X". If deals is empty say so plainly.
- "Mas mahal ngayon" / "Pricier than usual" / "いつもより高め" list from
  `pricier` (max 3 lines), shorter.
- one closing line: prices are DA price-monitoring prices for the date; local
  basis is Las Piñas market (or nearby markets when Las Piñas has no data for
  an item — say "(kalapit na palengke)" / "(nearby markets)" / "（近隣市場）"
  only on those items).
Rules: use only numbers from the JSON, copied exactly (₱ + number, pct with
%); "usual" always means the 30-day median; 7-day trend only if it adds
something. NEVER mention NCR, Metro Manila, basis, focus_market, median
jargon field names, or internal words. No substitutions, no meal plans, no
emojis overload (max 1 emoji per header line), no markdown tables.
Japanese: natural, concise Japanese for a Japanese resident in Manila.
Item names: use common Japanese where natural (トマト, ナス, キャベツ, 玉ねぎ,
じゃがいも, にんじん, 卵, 鶏肉(丸鶏), 豚バラ(リエンポ), 豚肩ロース(カシム), 米,
砂糖, 青唐辛子, 赤唐辛子, カラマンシー, ゴーヤ); for unfamiliar Filipino items give
the local name with a very short gloss on first mention only, e.g.
ガルンゴン（アジの一種）, バングス（ミルクフィッシュ）, ティラピア, サヨテ（ハヤトウリ）,
ペチャイ・バギオ（白菜の一種）. Use 1kg ₱X / 1個 ₱X. No English words left
in the Japanese section except market names (write them in katakana:
ラス・ピニャス, アラバン, ラ・ウエルタ, ビクタン, パサイ). Taglish should be natural
Manila Taglish. Keep each section under ~12 lines.

## 3. advise
Same flow (one opencode call, retry once on failure/missing sections, strip
preamble, write prompt.txt/advice.md/run.json/check.json). Additionally run a
"jargon lint" over the text: flag any of: NCR, Metro Manila, basis,
focus_market, nearby_median, median_30d, pct_vs, las_pinas (lowercase
identifier), "NCR-wide", "う lam" style mixed garbage = any Latin letter run
inside the 日本語 section other than allowed tokens (₱, kg, digits, %, and
nothing else). Lint results go into check.json under `lint` and make the
status "fail" (exit 3) too.

## 4. checker: keep strict binding + add basis/market/metric checks
Keep kind-aware + commodity binding (update the alias table with the Japanese
names above: 豚バラ, 豚肩ロース, 丸鶏/鶏肉, 玉ねぎ, じゃがいも, にんじん, 青唐辛子,
赤唐辛子, サヨテ, ゴーヤ, 米 etc. and Taglish: sibuyas, patatas, karot, sayote,
talong, kamatis, manok, itlog, bigas, asukal, sili).
Values allowed for a commodity: its price, median_30d, pct_vs_median_30d,
diff_vs_median_30d (and its abs), trend_7d_pct, trend_7d_base_value,
market_prices values, cheapest_nearby price/diff (abs), and derived
price - median_30d / price - market price differences.
New checks (flag reason in parentheses):
- Market binding ("market mismatch"): if a sentence/bullet names a market
  (Las Piñas/ラス・ピニャス, Alabang/アラバン, La Huerta/ラ・ウエルタ,
  Bicutan/ビクタン, Pasay/パサイ) next to a peso amount — specifically the peso
  number nearest to (immediately after, within ~20 chars) the market name —
  that number must equal that market's market_prices value for a commodity in
  context.
- Basis mismatch ("ncr basis not in signals"): any sentence mentioning
  NCR / Metro Manila / Kamaynilaan / 首都圏 / マニラ首都圏 that contains a
  number is flagged (signals contain no NCR numbers).
- Metric mismatch ("metric mismatch"): a percentage in a context with 7-day
  words (7 araw, isang linggo, 7-day, week, 1週間, 7日, 先週) must match
  trend_7d_pct; in a context with usual/30-day words (karaniwan, usual,
  30-day, 30 araw, いつも, 普段, 30日) and no 7-day words it must match
  pct_vs_median_30d. Determine the window around each number (the words
  between the previous number and this one within the same bullet/sentence)
  to decide.
Tests: real-file style fixtures — create testdata/signals_v2.json by running
the new signals on /tmp/presyo-snap.db, a hand-written testdata/advice_ok_v2.md
that follows the prompt format using numbers from it (must pass), and
testdata/advice_bad_v2.md with: a wrong market price next to a market name,
an NCR sentence with a number, a 7-day phrase carrying the 30-day pct, a
number belonging to another commodity, and a Latin-garbage token in 日本語 —
each must be flagged exactly once. Remove obsolete v1 fixtures.

Finally: gofmt, go vet ./..., go test ./..., rebuild bin/presyo, run
`./bin/presyo signals -db /tmp/presyo-snap.db -out /tmp/signals-v2.json` and
print the deals and pricier lists. Update go/README.md section.
