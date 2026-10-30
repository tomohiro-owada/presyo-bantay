# Presyo Bantay — household utilities: sources and design (draft, 2026-10-26)

Status: research + local prototype only (branch `utilities`). Nothing has been
pushed, deployed, or posted.

## 1. Sources tested (curl from the box AND from the VPS, 2026-10-26 ~07:00 Manila)

| # | Item | Source (official) | Format | Cadence | Box | VPS | Parse difficulty | Notes |
|---|---|---|---|---|---|---|---|---|
| 1a | Fuel weekly adjustment | DOE "Oil Monitor" page `doe.gov.ph/data-and-prices/liquid-fuels/oil-monitor` → PDFs on `d24qbtp4vooyzi.cloudfront.net/api/media/file/...` | HTML list + text PDF (2 pp) | weekly, "As of <Tuesday>" | 200 | 200 | medium: free-text sentences with typos ("1.93L/", "4.60/LPhp 4.62/L"), ranges | Official figures for the week; has movement no. and YTD net |
| 1b | Fuel prior notice (per company) | DOE `.../retail-pump-prices/price-adjustments` → "Prior Notice on LF Price Adjustments <dates>" PDF | text PDF table | weekly (Mon notices, Tue effectivity) | 200 | 200 | easy-medium (per-company rows, take mode/min/max) | Good cross-check for 1a; not in prototype |
| 1c | Prevailing pump prices NCR | DOE `.../retail-pump-prices/ncr-pump-prices` → "NCR Price Monitoring <date>" PDF | text PDF (2 pp) | weekly, ~1 week lag | 200 | 200 | easy for the NCR summary block (product / low / high / common); per-city table harder | ~14 cities monitored; **Las Piñas is NOT one of them** (nearest: Parañaque, Muntinlupa). Link titles are not in date order and file names are inconsistent → use the date inside the PDF |
| 1d | DOE Monday advisory article ("Liquid Fuels and LPG Price Adjustments for …") | doe.gov.ph article pages | HTML | weekly-ish (Mon evening) | article fetch works, but the news listing is client-rendered and slugs are not predictable | – | hard to discover | Not usable for automation; Monday-before-effectivity figures otherwise only come from oil-company/news posts (not official machine-readable) |
| 2a | LPG NCR monthly | DOE `.../lpg-monitor/ncr-lpg-prices` → "NCR <Month> 2026 LPG Price Monitoring" PDF | text PDF (1 p) | monthly (first ~week) | 200 | 200 | easy for 11-kg range + common and Auto-LPG; Las Piñas row = brand values + city range (brand column mapping is ambiguous → MVP uses the city range only) | Las Piñas IS covered |
| 2b | LPG prior notice (per company, per kg) | DOE `prior-notice-on-price-adjustments-for-lpg` | text PDF | monthly (staggered tranches) | 200 | – | medium (staggered: e.g. Regasco P10/P5/P2.50 = P17.50/kg) | Use later for "up to ₱X/11 kg" |
| 3a | Meralco monthly rate | `company.meralco.com.ph/news-and-advisories/latest-news-and-press-releases` → `.../<higher\|lower>-...rates-<month>-<year>` | HTML press release | monthly (~10th–13th) | 200 | 200 | easy: "overall rate for a typical household to P15.0615 from P14.7424 per kWh", "200 kWh … around P64" | Meralco itself states the 200 kWh impact |
| 3b | Meralco rate schedule | `meralcomain.s3...amazonaws.com/2026-10/10-2026_rate_schedule.pdf` | PDF, 1 huge table | monthly | 200 | 200 | hard (30+ columns) | Not needed; future month's file 403 until published |
| 4a | Maynilad interruptions (scheduled / emergency / rotational) | `mayniladwater.com.ph/wp-json/interruptions/{scheduled,emergency,rotational}?id=&city=Las+Piñas` (the endpoint the public Service Advisories page itself calls) | JSON string wrapping an HTML table | live | 200 (~1–20 s) | 200 | easy | Las Piñas currently: 0 rows in all three (checked 2026-10-26). The full page `/service-advisories-2/` also works but takes ~25 s |
| 4b | Maynilad weekly maintenance post | WordPress REST `wp-json/wp/v2/posts?search=maintenance activities` | JSON (HTML table in content) | weekly (Fri ~15:00–17:30) | 200 | 200 | easy (rowspan city cells) | Las Piñas appeared in 1 of the last ~16 weekly posts (Aug 21: Almanza Uno, Pilar, Talon 1/2/5, Aug 26–27) |
| 4c | Maynilad tariff changes (FCDA quarterly, annual rebasing) | WP REST search "FCDA" → "Notice to Maynilad Customers: FCDA Effective <date>" | JSON/HTML | quarterly (Jan/Apr/Jul/Oct 1, notice ~15th of prior month) | 200 | 200 | easy-medium | Oct 1 2026 FCDA: −0.02 % of ₱52.86/cu.m. avg basic charge ≈ −₱0.01/cu.m. (negligible) |

Not used: `legacy.doe.gov.ph` (TLS error), DOE `/api/*` and `/sitemap.xml`
(return the 404 page), Maynilad bill-calculator Azure API (internal API behind
a page widget; not a sanctioned data source).

### Terms / licence notes
- DOE: government data; no terms page found; robots.txt not served (404 page).
  Republishing official prices with attribution should be fine.
- Meralco: corporate site has no robots.txt; the Terms & Conditions page is about
  the My Meralco portal/app (bans bots there). Press releases are meant for the
  public; we only quote 3–4 numbers with a link. One page fetch/day max.
- **Maynilad: website T&C say material "may not be copied, reproduced,
  republished … except for your own personal non-commercial use".** We would
  only relay facts (barangay, time window, reason) with a link, which is
  public-safety info, but this is the one source where the user should decide
  (or ask Maynilad Corporate Communications for OK). robots.txt allows
  everything except /wp-admin/.

## 2. Recommended MVP (in order)
1. **Weekly fuel post (Tuesday)** — DOE Oil Monitor (+ NCR common price of the
   previous week). Highest value, fully official, reliable fetch.
2. **Monthly "Bayarin sa bahay" post (mid-month, after Meralco announces)** —
   Meralco rate + 200 kWh impact, LPG 11-kg NCR common price + Las Piñas
   range (+ month-on-month), Maynilad status line / FCDA when relevant.
3. **Las Piñas water-interruption alerts** — only when Las Piñas rows appear
   (expect ~1–2/month). Site page always; FB only after the user confirms the
   Maynilad terms question.

## 3. Fetch schedule (Manila time; one new `utilities.sh`)
- Fuel: Tue 09:00–Wed 20:00 Manila, hourly conditional fetch of the DOE
  oil-monitor page (1 request; PDF only when a new title appears).
- Meralco: daily 1 request to the news listing, days 5–20 of the month.
- LPG: daily 1 request to the DOE NCR-LPG page, days 1–15 of the month.
- Maynilad: every 3 h 06:00–21:00 Manila (3 small JSON calls), plus Fri/Sat
  for the weekly maintenance post.
- All: same UA as DA, ≥5 s between requests to one host, cache PDFs, no retry
  loops, write `data/utilities/snapshot.json` + history JSON in the repo.

## 4. Posting rules (fit with current limits)
Current: 5 regional DA posts/day (05:30–20:00, ≥2 h gap, one per run),
Sunday weekly 18:05 with a 16:00–20:00 hold.
- Same state file + flock + global min-gap (2 h) — utilities posts count
  toward the gap like the weekly post does.
- **DA first**: `fbutil -auto` posts only when the DA regions for the latest
  data date are all posted (or no fresh DA data), so it never delays a DA post.
  Window 12:00–19:00 Manila; never on Sunday 16:00–20:00 (weekly hold).
- Fuel: once per effective week (state key `fuel:2026-10-20`), Tue/Wed only;
  skip if not available by Wed 19:00.
- Bills: once per month (`bills:2026-10`), after the Meralco release is parsed;
  uses whichever LPG month is newest.
- Water alert: once per interruption id (`water:<hash>`), only if it starts
  ≥ 6 h later and ≤ 72 h ahead; exempt from the "DA first" rule but still
  respects the 2 h gap; max 1/day.
- Expected volume: +1/week (fuel) +1/month (bills) +0–2/month (water) ≈
  6–7 extra posts/month vs ~150 DA posts/month.

## 5. Site
- `/u/` "Bayarin / Utilities / 光熱費" page (Go-generated static HTML like
  `/w/`): latest fuel adjustment + 12-week bar history, NCR common pump prices,
  LPG 11-kg monthly line, Meralco ₱/kWh monthly line + 200 kWh bill estimate,
  Las Piñas water interruptions (current + last 30 days), sources and
  "checked at" times. Data `site/public/data/utilities.json`.
- Frozen per-post pages `/u/fuel/2026-10-20/`, `/u/bills/2026-10/` (FB == site),
  linked from the top page card.

## 6. Open questions for the user
1. Maynilad T&C (no republishing): OK to relay interruption facts with a link,
   or ask Maynilad first / site-only?
2. Fuel post day: Tuesday (official DOE figures, the day prices change) vs
   Monday evening from news/oil-company posts (not official, not automatable)?
3. Household-impact assumptions: 10 L fuel fill-up and 200 kWh (Meralco's own
   typical) OK? Want a "typical family month" total (e.g. 1 LPG tank + 200 kWh
   + 20 cu.m water)?
4. Las Piñas is not in DOE's weekly pump monitoring — show NCR common price +
   Parañaque/Muntinlupa rows, or NCR only?
5. Which LPG brand(s) to name (Gasul/Solane/Petron)? Brand column mapping in
   the DOE PDF is ambiguous, so the MVP shows only the Las Piñas range.
