# grabfare — "Grab fare estimate" card (dry-run prototype)

Shows a Grab app quote for a fixed taxi route on Presyo Bantay: Old Zapote Public Market -> Festival Supermall Alabang (pickup was "Zapote Bridge" until run 20261102-1220)
(Alabang-Zapote Road, Las Pinas/Muntinlupa). It is a quote read from the Grab app at a given time, NOT an official
fare, and not endorsed by Grab. No Grab screenshots, logos or brand colours.

## Command
`presyo grabfare -out-dir DIR [-api-url URL] [-token-env NAME] [-env-file FILE] [-hours 50] [-now RFC3339] [-input-file FILE] [-stale-after 2h] [-timeout 20s]`
- Source: `GET {api-url}/v1/grab/recent?hours=N` with `Authorization: Bearer <token>` (JSON, see "API" below). `-input-file`
  reads the same JSON from disk instead (tests/offline). api-url and token are NEVER hardcoded or defaulted in the repo: they
  come from flags/env (`PRESYO_GRAB_API_URL`, `PRESYO_GRAB_API_TOKEN`) or `-env-file` (KEY=VALUE lines, `#` comments; no
  default path). Missing config -> exit 1 with a generic message. The token must never be printed or logged; the URL must not
  be printed either (print only "fetching grab fare data").
- Writes into DIR atomically (temp file + rename): `grab-fare.json`, `grab-fare.html` (snippet), `grab-fare-preview.html`
  (standalone page for review, wraps the snippet, tl/en/ja toggle with a few lines of inline JS, site colours via CSS vars
  --bg #faf8f2, --card #fff, --ink #1c1a15, --muted #5c5648, --line #e2dccb, --accent #0b5d3b, --up #b3261e, --down #1e7e34).
- Exit codes: 0 ok (including a stale/hidden card written), 1 config/fetch/parse error.
- On fetch/parse failure: do NOT leave an old card looking current: if DIR/grab-fare.json exists and its `valid_until` is in the
  past (relative to now) rewrite it + the snippet as the hidden/stale state; otherwise leave files untouched; exit 1.
- Package `go/internal/grabfare` (pure functions; time injected), command in `go/cmd/presyo/cmd_grabfare.go`, register
  in main.go's switch + usage, add a README row. stdlib only. gofmt clean. Tests with a fake `httptest` server and fixtures.

## API input (do not invent fields)
```
{"route":{"from","to","via","label"},"source":"...","currency":"PHP","server_time":"...+08:00","hours":50,"count":N,
 "runs":[ newest first:
  {"ts":"20261102-0800","captured_at":"2026-11-02T08:00:00+08:00","analyzed_at":string|null,
   "status":"complete|fare_only|traffic_only|pending",
   "fare": null | {"currency":"PHP","standard":{"price","eta_min"},"saver":{"price","eta_min"},
        "metered_taxi":{"min","max","eta_min"},"express":{"min","max","eta_min"},"others":[...]},
   "traffic": null | {"level":"light|moderate|heavy|severe","summary","green_pct","yellow_pct","red_pct",
        "segments":[{"level","red_sections":[],"yellow_sections":[]}]}}]}
```
All numbers may be null.

## Selection rules (now = injected clock, Asia/Manila fixed zone UTC+8, no tzdata dependency)
- A run is usable for the fare if `fare != null` and a headline can be computed: headline = metered_taxi min..max
  (if only one of min/max is present use it for both; if metered_taxi has neither, fall back to standard.price as min=max and mark
  `basis:"standard"`; basis otherwise "metered_taxi"). Secondary values shown when present: Grab Standard price.
- current = the newest usable run. age = now - captured_at.
  - If the newest run overall is `pending` (not yet analysed) but an older usable run exists within `stale-after`, show that older
    run, with `fallback:true` and its own as_of time (never relabel it as current hour).
  - age > stale-after (default 2h), or no usable run, or only pending rows -> state `stale`/`unavailable`: `visible:false`, the
    snippet is an HTML comment only (`<!-- grab-fare: hidden (stale) -->`), JSON carries the reason. Never show an old fare as current.
- Traffic: from the same run if `traffic != null`; else traffic omitted (do not take it from another run).
- Trend (Presyo rule: always show a trend): previous = the next older usable run (older than current, within 26h window).
  Compare the headline midpoint (min+max)/2: delta pesos and pct; direction `up` (delta >= +1 peso), `down` (delta <= -1 peso), `flat` (|delta| < 1 peso).
  Carry the previous run's as_of. If a usable run exists 24h +/- 30min before current, also give `vs_yesterday` the same way. If there is no
  previous run: direction `none` with text saying no earlier reading yet.
- Money display: whole pesos with thousands separator, `₱282–338` (en dash); equal min=max -> `₱282`.
  Raw numbers stay unrounded in JSON.

## grab-fare.json (public-safe: no hosts/paths/tokens)
```
{"generated_at":RFC3339(+08:00),"visible":bool,"state":"fresh|fallback|stale|unavailable","reason":string,
 "valid_until":RFC3339 (captured_at + stale-after) | null,
 "route":{"from","to","via"},"currency":"PHP","basis":"metered_taxi|standard",
 "as_of":RFC3339,"as_of_hhmm":"HH:MM","age_minutes":int,"fallback":bool,
 "fare":{"min":n,"max":n,"standard":n|null},
 "traffic":null|{"level","summary_en","green_pct","yellow_pct","red_pct","worst_sections":[up to 3 street names]},
 "trend":{"direction":"up|down|flat|none","delta_pesos":n|null,"delta_pct":n|null,"previous_as_of":..,"previous_hhmm":..},
 "vs_yesterday":null|{same shape without none},
 "hourly":{"today_date":"YYYY-MM-DD","yesterday_date":"YYYY-MM-DD","today":[{"h":0-23,"min":n,"max":n,"std":n|omitted}],"yesterday":[...]},   // visible states only
 "text":{"tl":{"title","headline","asof","traffic","trend","note"},"en":{...},"ja":{...}}}
```
Texts, Taglish (tl) / English / Japanese. Example en: title "Grab fare estimate", route "Old Zapote Public Market → Festival Mall (Alabang-Zapote Rd)" (earlier pickup: Zapote Bridge),
headline "₱282–338 metered taxi quote", asof "As of 08:00 (Manila time)", traffic "Traffic: heavy (about 35% red)" (omit % if null), trend "▲ ₱12 (+4%) vs 07:00"
(▼ down, ＝ "No change vs 07:00", none: "No earlier reading yet"), note "Quote shown in the Grab app at that time. Not an official fare; actual fare may differ."
For fallback add to asof: "(latest analysed reading)". Japanese and Taglish equivalents must be natural and short (Taglish: "Tantiya sa pamasahe ng Grab", "Ayon sa Grab app noong 08:00
(oras sa Maynila)", "Trapiko: mabigat", trend "▲ ₱12 (+4%) kumpara sa 07:00", note "Quote lang ito mula sa Grab app sa oras na iyon. Hindi opisyal na pamasahe."). Traffic level words: light/moderate/heavy/severe =
tl: magaan/katamtaman/mabigat/sobrang bigat; ja: 空いている/やや混雑/混雑/ひどい渋滞.
Numbers in text must come from the data (a test must recompute and check every ₱ amount and % in each text appears in JSON values).
HTML: escape everything (html.EscapeString); snippet = `<section class="grab-fare" data-valid-until=... aria-label=...>` with three `<div lang="tl|en|ja" class="gf-lang">`
blocks (only tl visible by default via a `hidden` attribute on the others; the preview page toggles `hidden`), plus a 1-2 line CSS block using
CSS variables with fallbacks. No images, no external requests.

## Tests
fresh complete run; up/down/flat/none trend; fallback when newest is pending; stale (>2h) hidden; only pending rows -> unavailable hidden; fare_only (traffic omitted);
standard-basis fallback; vs_yesterday; null numbers; HTML escaping; fetch via httptest with bearer header check; token/URL never appear in stdout/stderr/outputs;
failure path rewriting an expired previous card but leaving a valid one; a scan test that output files contain no "http", "ssh", "/home", "@" besides explicit allowed strings.

## hourly series (site chart)
`-hours` now defaults to 50 so the recent-runs request covers all of yesterday. `hourly` groups plausible usable readings by Manila date
(today = date of `now`, yesterday = the day before) and hour; the earliest reading in an hour wins; future readings are ignored; missing hours are
absent from the lists (never interpolated). Lists are `[]`, never null. Present only on visible (fresh/fallback) output; old consumers ignore it.

## Pickup change and eras
The API may add an optional top-level `pickup` block right after `route`:
`"pickup":{"current":"Old Zapote Public Market","since_ts":"20261102-1220","since":"2026-11-02T12:20:00+08:00","history":[{"since_ts":"19700101-0000","since":"1970-01-01T00:00:00+08:00","pickup":"Zapote Bridge"},{"since_ts":"20261102-1220","since":"2026-11-02T12:20:00+08:00","pickup":"Old Zapote Public Market"}]}`.
`route.from` already equals the current pickup. When the block is absent (old API) everything behaves as before: all runs are era 0 and no pickup output is emitted.

A run's ts is "YYYYMMDD-HHMM" (Manila). Era of a run = number of history entries whose `since_ts` is greater than the run ts (lexicographic). Era 0 = current pickup.

Fares from different pickups are NOT comparable. Build splits usables into `all` (every plausible reading, for the hourly chart) and `cur` (era 0 only); headline, fallback, freshness, trend, vs_yesterday and traffic come ONLY from era-0 usables. trendFor/vsYesterday receive the era-0 list. If there is no era-0 usable reading the output is the same unavailable (hidden) output as today.

buildHourly takes all usables and the doc era function. Per (Manila date, hour) it picks the reading with the lowest era first, then the earliest captured, and sets `HourPoint.Era`. The `yesterday` list contains only era-0 readings; `today` keeps all eras (renderers draw a break marker). Future readings are ignored.

Pickup note is shown only when the change is relevant: Manila date of `pickup.since` >= yesterday's date (rest of change day and the next day), and the doc has a previous pickup. Texts:
- tl: "Bagong sundo mula {N}: {P}. Hindi kasama sa paghahambing ang mga naunang presyo mula sa {Q}."
- en: "Pickup changed at {N} to {P}. Earlier fares from {Q} are not compared."
- ja: "{N}から乗車地点を{P}に変更。以前の{Q}の料金とは比較していません。"
where N = since HH:MM, P = current pickup name, Q = previous pickup name. `Output.Pickup` (name/since/since_hhmm/previous) is set whenever the block is valid.

Runs with era > 0 are never eligible for a card; `BuildFromRun` with an era > 0 ts returns the unavailable output.

`Output.Pickup` is placed after `Route` and has `name`, `since` (RFC3339 Manila), `since_hhmm`, and `previous` (optional). `LangText` gains `pickup_note`.
