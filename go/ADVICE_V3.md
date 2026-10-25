# Advice prototype v3 — deals card on the static site (+ FB-ready text)

Decisions: Telegram dropped. Distribution = (1) a "Sulit ngayon / Today's
deals / 今日のお買い得" card on the static site, (2) later auto-posting to a
Facebook Page (NOT now — only make the text FB-ready: plain text, no markdown
headers/tables/bold inside the post bodies, emoji sparingly: at most one on
the title line and none elsewhere).
Update go/ADVICE_SPEC.md to describe v3 (single spec).

Hard rules: same as before. You may edit go/ AND site/ (src, index.html,
public/data/deals.json only — do NOT regenerate or modify other files under
site/public/data, they are tracked data). Never open ../data/presyo.db; use
/tmp/presyo-snap.db read-only. No new Go deps; no new npm deps; no CDN, no
tracking, no external requests from the site. No git commits. Do not run
`advise` (the orchestrator runs it). Do not touch the VPS or deploy anything.

## 1. signals polish (Go)
- `cheapest_nearby` multi-day check: consider the last 3 report dates
  (dates <= as-of where the basis has a price, most recent 3). The candidate
  market (cheapest on as-of date among the 5, needs >= 3 markets reporting
  on as-of, strictly cheaper than basis on as-of) must ALSO be strictly
  cheaper than the basis price on at least 2 of those 3 dates (as-of counts).
  Emit `days_cheaper` (int) and `days_checked` (int, = 3 or fewer if fewer
  dates exist; require days_checked >= 2). Otherwise null.
- `sharp_drop_this_week` (bool) per commodity & deal: true when
  trend_7d_pct <= -10 AND trend_7d_pct <= 0.8 * pct_vs_median_30d (i.e. most
  of the drop below usual happened within the last 7 days). Also
  `sharp_rise_this_week` analogously for pricier (trend_7d_pct >= +10 and
  >= 0.8 * pct_vs_median_30d).
- Pricier entries: do NOT include cheapest_nearby.
- Top-level `uses_nearby_basis` (bool): true iff any item in deals or
  pricier (the lists that are shown) has basis "nearby_median".
- Tests for all of the above (multi-day: 3/3, 2/3 ok; 1/3 rejected; only 1
  date rejected; sharp flags boundaries; uses_nearby_basis).

## 2. Deterministic template texts (Go, `internal/advice/template.go`)
`RenderTemplate(signals) -> Texts{TL, EN, JA string}` producing the same
structure the LLM is asked for, from signals only. This is the fallback when
the LLM call fails or the check fails, and must itself pass the checker
(test it: run Check on the 3 template texts for the real fixture and for
synthetic cases incl. empty deals). Format per language (plain text, FB-ready):
  line 1: title with one emoji, e.g. "🛒 Sulit ngayon sa palengke (2026-10-23)"
          EN "🛒 Today's market deals (2026-10-23)", JA "🛒 今日のお買い得（2026-10-23）"
  then one line per deal: "• <item>: ₱<price>/kg — <pct>% vs usual (₱<diff>)"
     + ", dropped sharply this week" / ", biglang bumaba ngayong linggo" /
       "、今週大きく下がった" when sharp_drop_this_week
     + "; cheapest at <Market>: ₱<p>" / "; pinakamura sa <Market>: ₱<p>" /
       "。<マーケット>が最安 ₱<p>" when cheapest_nearby present
     + " (nearby markets)" / " (kalapit na palengke)" / "（近隣市場）" when the
       item's basis is nearby_median
     (per piece items use /pc, /piraso, 1個; JA puts the unit first:
      "1kg ₱130")
  "Mas mahal ngayon:" / "Pricier than usual:" / "いつもより高め：" + max 3 lines
     "• <item>: ₱<price>/kg — +<pct>% vs usual" (+ sharp rise wording:
     "rose sharply this week" / "biglang tumaas ngayong linggo" / "今週大きく上がった")
  closing line: "Batay sa DA price monitoring, palengke ng Las Piñas." /
     "Based on DA price monitoring, Las Piñas public market." /
     "農業省（DA）の価格調査、ラス・ピニャス市場の価格。"; append the nearby
     footnote ONLY if uses_nearby_basis: " Ang may (kalapit na palengke) ay
     galing sa Alabang/La Huerta/Bicutan/Pasay." / " Items marked (nearby
     markets) use Alabang/La Huerta/Bicutan/Pasay." / "（近隣市場）はアラバン・
     ラ・ウエルタ・ビクタン・パサイの価格。"
  empty deals: "Walang kapansin-pansing mura ngayon." / "No notable deals
     today." / "今日は特に目立つお買い得はありません。"
Item display names: EN = canonical name without "(Local)"; TL = common
Filipino names where natural (Pechay Baguio, Sayote, Kamatis, Talong,
Siling labuyo? NO — use "Pulang sili", "Siling berde", Patatas, Karot,
Sibuyas (pula/puti), Manok, Itlog, Bigas …); JA as in the v2 prompt list
(ペチャイ・バギオ（白菜の一種）, サヨテ（ハヤトウリ）, トマト, ナス, 赤唐辛子,
青唐辛子, じゃがいも, にんじん, 赤玉ねぎ, 白玉ねぎ, 丸鶏, 豚バラ（リエンポ）,
豚肩ロース（カシム）, ガルンゴン（アジの一種）, バングス（ミルクフィッシュ）,
ティラピア, 卵, 米, 砂糖, カラマンシー, ゴーヤ, キャベツ). Keep a single
name table in Go shared by template, prompt hints and checker aliases.
Market names JA: ラス・ピニャス, アラバン, ラ・ウエルタ, ビクタン, パサイ.

## 3. Prompt + advise
- Prompt: same three sections `## Taglish`/`## English`/`## 日本語` (headers
  are only for splitting), but bodies must be plain FB-ready text in exactly
  the template's line structure (give the model the rendered template texts
  as the "skeleton" and ask it to make them read more naturally WITHOUT
  adding, removing or changing any number, item or market; it may reword
  connecting words only). Japanese 7-day wording: "先週比" (e.g. 先週比
  -40.9%); use 今週大きく下がった for sharp drops; never "still falling".
  Omit the nearby footnote when uses_nearby_basis is false.
- advise: after the LLM call, split into tl/en/ja texts. Status per run:
  `text_source` = "llm" if the call succeeded, all 3 sections present and the
  check (incl. lint) passes; otherwise "template" (use RenderTemplate texts),
  recording `fallback_reason`. advise exits 0 in both cases (fallback is not
  an error) but prints a warning; exit 1 only on I/O errors. Add a flag
  `-no-llm` to skip the call and use the template directly.
- Lint additions: flag "still falling"/"still down"/"tuloy-baba"; flag the
  nearby footnote words (近隣市場, kalapit na palengke, nearby markets) when
  uses_nearby_basis is false; flag markdown in bodies (lines starting with
  '#', '|', '**', or '- ' bullets are OK? -> allow "• " and "- " bullets,
  flag '**' and '|' tables).
- advise writes in out-dir: prompt.txt, advice.md (raw model text),
  run.json (+ text_source, fallback_reason), check.json, and `deals.json`
  (see 4).

## 4. deals.json for the site
New subcommand `presyo deals -signals FILE [-advice-dir DIR] -out FILE`
(and advise also writes it): JSON
{ as_of, generated_at (RFC3339, Asia/Manila; env PRESYO_GENERATED_AT
  overrides for tests), text_source ("llm"|"template"), model (when llm),
  texts: {tl, en, ja}, deals: [...from signals...], pricier: [...],
  uses_nearby_basis, thresholds }
If -advice-dir has run.json with text_source "llm" and a passing check.json
and advice.md, use those texts; else RenderTemplate. Stable key order, indent
2, non-ASCII unescaped. The orchestrator will copy it to
site/public/data/deals.json.
Optional cron hook: in go/scripts/daily.sh add a step AFTER export, gated by
env `PRESYO_DEALS=1` (default off, so current behaviour is unchanged):
`$BIN signals -db $DB -out $TMPDIR/signals.json && $BIN advise -signals ...
-out-dir $PRESYO_HOME/advice/<date> [-no-llm if PRESYO_DEALS_LLM!=1]` then
copy deals.json to $REPO/site/public/data/deals.json so the existing commit
step includes it (add it to the committed paths if needed). Failures of this
step must not fail the daily job's ingest/export result (log + record rc).
Extend daily_test.sh with a case for PRESYO_DEALS=1 using the fake BIN.
Document in go/README.md: OPENCODE_API_KEY must be present in the cron
environment (e.g. an env file with mode 600 sourced by the cron line) —
without it opencode fails with an opaque "UnknownError", in which case the
template fallback is used.

## 5. Site card (site/)
- Load `data/deals.json` (same fetchJson helper). If missing/invalid, hide
  the card silently.
- Card near the top (after the header/intro, before the charts): title
  "Sulit ngayon" / "Today's deals" / "今日のお買い得", as-of date, and the
  text for the current language rendered as plain text lines (textContent,
  NO innerHTML of model text — XSS safety), "• " lines as a list. Small
  muted footer: "Awtomatikong buod mula sa DA price data" /
  "Automatic summary from DA price data" / "DA価格データからの自動要約" and,
  if text_source is "llm", "(AI-assisted, numbers checked)" /
  "(may tulong ng AI, sinuri ang mga numero)" / "（AI補助・数値チェック済み）".
- Language toggle: add 日本語 ("ja") as a third option. Add a full ja
  STRINGS table in i18n.ts (translate all existing strings naturally; dates
  with ja-JP locale). Keep tl default. Update tests/types (Lang =
  "tl"|"en"|"ja"); `npm test` and `npm run build` must pass (`cd site &&
  npm test && npm run build`).
- Styling consistent with the existing CSS; works at 390px wide.
- Write a vitest test for the deals card text-to-lines function.

## 6. Finish
gofmt, go vet ./..., go test ./..., bash go/scripts/daily_test.sh,
cd site && npm test && npm run build. Rebuild go/bin/presyo. Generate
`/tmp/signals-v3.json` from /tmp/presyo-snap.db and run
`./bin/presyo advise -no-llm -signals /tmp/signals-v3.json -out-dir /tmp/adv-v3`
and print the three template texts and check result.

## 7. Combined Facebook post (added requirement)
One combined FB Page post text containing all three languages in one post:
Taglish first, then a separator line ("———"), then English, separator, then
Japanese, then a final block with the site link
`https://abalol.xyz/presyo-bantay/` and a short DA source note
("Source/Pinagmulan/出典: DA (Department of Agriculture) price monitoring").
Plain text, one line per item, emoji sparingly as section markers only
(e.g. 🛒 for deals title, 📈 for pricier), no markdown. Keep it short for a FB
feed: in the combined post, each language shows at most 5 deals and at most 2
pricier lines, and the per-language closing source line is replaced by the
single shared footer. Target <= ~1,800 characters total.
- Go: `RenderFBPost(texts or signals) string` built deterministically from
  the per-language texts (LLM or template — whichever `text_source` is
  chosen) by trimming/joining, never inventing content.
- Written as `fb_post.txt` by advise/deals (in out-dir) and as field
  `fb_post` in deals.json.
- The checker must cover it: run Check (+ lint) on the combined text too
  (treat each language block as its section; the footer has no numbers
  except none); include `fb_post` results in check.json; fallback to template
  if it fails. Tests for RenderFBPost (order, separators, link, limits,
  length) and that the real-fixture fb_post passes the check.
