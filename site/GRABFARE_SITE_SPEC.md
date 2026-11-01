# Site: Grab fare card (client-side, data file fetched at runtime)

The static site (Vite + vanilla TypeScript, GitHub Pages) must show a "Grab fare estimate" card from a small JSON file that is
refreshed hourly OUTSIDE the site build. The JSON is the output of `presyo grabfare` (`grab-fare.json`; shape in go/GRABFARE_SPEC.md:
`visible`, `state` (fresh|fallback|stale|unavailable), `valid_until` (RFC3339), `as_of`, `fare{min,max,standard}`, `trend{direction,...}`,
`vs_yesterday`, `traffic|null`, `text{tl,en,ja:{title,headline,asof,traffic,trend,note}}`, `route{from,to,via}`).
Nothing is rebuilt hourly: the page fetches the file when it loads (and every 10 minutes while open).

## Implement
- `site/src/grabfare.ts` (pure, unit-tested, no DOM): `export function evalGrabFare(raw: unknown, nowMs: number): GrabFareView | null`
  - returns null (card hidden) unless: raw is an object, `visible === true`, state is "fresh" or "fallback", `valid_until` parses and is >= nowMs,
    `as_of` parses and is not more than 10 minutes in the future, `text.tl|en|ja` objects with a non-empty string `headline`, `title`, `asof`, `trend`, `note`.
  - GrabFareView = { text: Record<Lang, {title, route, headline, asof, traffic, trend, yesterday?, note}>, trendDir: "up"|"down"|"flat"|"none", fallback: boolean, validUntilMs: number }.
    `route` is built from route.from/to/via ("Zapote Bridge → Festival Supermall Alabang (Alabang-Zapote Road)"); `traffic` may be "" (then not shown);
    `yesterday` only when vs_yesterday exists: build with the same wording as the caption: en "▼ ₱12 (-3%) vs yesterday 23:00", tl "... kumpara kahapon 23:00", ja "... 昨日23:00比" using vs_yesterday.delta_pesos / delta_pct / previous_hhmm (arrow up/down/＝ from direction).
  - never trusts markup: strings are only ever assigned with textContent.
- `site/src/grabfare.test.ts` (vitest): fresh ok; expired valid_until -> null; visible false -> null; stale/unavailable -> null; future as_of -> null; garbage/missing text -> null; yesterday line wording for up/down/flat; fallback flag; invalid JSON types.
- `index.html`: add `<section class="card" id="grab-card" aria-labelledby="grab-h" hidden>` after the deals card with h2#grab-h, p#grab-route, p#grab-headline (strong), p#grab-asof, p#grab-traffic, p#grab-trend, p#grab-yesterday, p#grab-note.hint.
- `site/src/main.ts`: `loadGrabFare()` fetches `GRAB_FARE_URL` with `{cache: "no-store"}` (failure/non-200/bad JSON => card stays hidden, no error UI, no console spam), stores the evaluated view, `renderGrabFare()` fills the card for the current `lang` (re-run on language switch like renderDeals) and re-evaluates validUntil against Date.now() before showing (hide when expired); `setInterval(loadGrabFare, 10 * 60 * 1000)`.
  `const GRAB_FARE_URL = import.meta.env.VITE_GRAB_FARE_URL ?? "https://raw.githubusercontent.com/tomohiro-owada/presyo-bantay/grab-data/grab-fare.json"`. Add the vite env typing if needed (src/vite-env.d.ts or existing).
  Color the trend line with the existing --up (rising price) / --down CSS variables via classes `grab-up`/`grab-down`; add the few CSS rules to style.css matching the deals card look (no new colours).
- Do NOT touch data files under site/public/data, and do not add any server identifiers. Keep `npm run build` (tsc --noEmit && vite build) and `npm test` green. Run them.

## Hour-of-day chart (inside the same card)
- `site/src/grabchart.ts`: `parseHourly(raw)` validates `hourly` of grab-fare.json (hours 0-23 integers, positive min<=max, dedup, sorted);
  null when today is empty and yesterday has < 6 points. Yesterday is hidden (`yesterday = null`, `yesterdayPending = true`) when it has < 6 points.
- `buildGrabChartSvg(data, lang)`: inline SVG (viewBox 360x190, width 100%), x = 00-23 Manila hours (ticks every 3h), y = fare in PHP.
  Today = bold line through the midpoint of the metered taxi range + light min-max band + dots (hover title with range); yesterday = thin faint
  midpoint line only. A missing hour breaks the line (no interpolation); isolated points are dots. Latest today value is labelled.
  Grab Standard is not drawn (keeps the chart readable). Colours reuse --accent/--muted/--line.
- `buildGrabChartLegend`: legend (Today/Yesterday/range) + a hint when yesterday exists but is still < 6 points. Labels TL/EN/JA live in grabchart.ts.
- No new dependency; the chart is re-rendered on language switch with the rest of the card.
