# grabcard — Facebook image card + trilingual caption for the Grab fare estimate (DRY-RUN ONLY)

Builds on `internal/grabfare` (see GRABFARE_SPEC.md; already implemented: Build, Output, FetchGET, ParseInput, etc.).
Nothing here posts anywhere: there must be NO Facebook/Graph code, no state file, no token for FB. Output = files for review.
The card shows: route, metered taxi range + Grab Standard, as-of time (Manila), trend vs previous reading and vs same hour yesterday
(when available), the disclaimer "Grab app quote, not an official fare", a strip of REAL Grab map screenshots (already cropped to the
map area upstream: use them exactly as given, NEVER crop/edit/recolour them) with the small source label "Map: Grab app screenshot",
and the traffic level ONLY when that run's traffic analysis is present AND passes the sanity check. Every Presyo post shows the trend.
No Grab logo is drawn by us (screenshots may contain Grab UI/watermark; we do not add any).

## 1. API changes you can rely on (already deployed on the API side)
Each run in `/v1/grab/recent` has a new field `"map_images": ["map1","map2","map4"]` = kinds (from map,map1..map4) with a cropped image
available. Images: `GET {api-url}/v1/grab/map/{ts}/{kind}.webp` (Bearer token, returns image/webp, 404 if absent).
Traffic is non-null in the API only when the upstream traffic analysis succeeded.

## 2. internal/grabfare additions (stdlib + golang.org/x/image/webp only; x/image is already in go.mod; keep package pure/testable)
- `Run` gets `MapImages []string \`json:"map_images"\``.
- `TrafficSane(t *TrafficIn) (ok bool, reason string)`: false (reason "overlay") if the summary (lowercased) contains any of: notification, overlay,
  screenshot, supplied, food-order, food order, grab food; false (reason "unusable") if it contains: unable, cannot, can't, not a map, no route,
  does not show, doesn't show, not visible; false (reason "level") when level not in light/moderate/heavy/severe; false (reason "pct") when any of
  green/yellow/red pct is nil, outside 0..100, or the sum is outside 95..105; false (reason "empty") when the summary is blank.
- `Build` (site JSON) must now emit `traffic` only when `TrafficSane` is true (a run with insane traffic keeps its fare card, no traffic block, no traffic text).
- `BuildFromRun(doc *InputDoc, now time.Time, staleAfter time.Duration, ts string) *Output`: same as Build but treats the run with that ts as the newest
  (drop runs captured after it); returns the hidden/unavailable output if that run is not usable.
- Exported helpers for the caption: `YesterdayTextEn/Tl/Ja(TrendOut) string` e.g. "▼ ₱12 (-3%) vs yesterday 23:00", tl "▼ ₱12 (-3%) kumpara kahapon 23:00",
  ja "▼ ₱12（-3%） 昨日23:00比"; and exported `TrendTextEn/Tl/Ja` wrappers around the existing trend text functions.
- `mapfilter.go`: `HasDeviceMarker(img image.Image) bool` – true when the Android "my location" blue dot is visible: count pixels with
  |r-66|<25 && |g-133|<25 && |b-244|<20; flagged when count >= 150. (The route start pin is a different blue (r<40, g~140, b>220) and must NOT be counted.
  Measured on real screenshots: dot images 419-478 px, clean images 1-11 px.) Test with synthetic images (a filled blue disc of radius 12 is flagged; a
  pin-coloured disc and a blank image are not).
- `PickMaps(clean map[string]image.Image) []string`: from the clean (marker-free) images choose up to 3 kinds in route order map1,map2,map3,map4 spread over the
  route (3 or fewer available: take all; 4 available: map1, map2 or map3 (middle), map4 -> choose map1, map2, map4; with 2: both); if fewer than 2 route
  maps are clean, append `map` (overview) when clean; the result may have 0..3 entries.
- `SelectCardRun(doc *InputDoc, now time.Time, maxAge time.Duration) []Candidate`: candidates newest-first = runs that have a usable fare headline, age <= maxAge,
  at least one entry in MapImages, and NOT (traffic present && TrafficSane reason "overlay") (an overlay on the map means the maps themselves are suspect).

## 3. Caption (`internal/grabfare/caption.go`) — same shape as the other Presyo utility captions
`RenderCaption(o *Output) string`, built ONLY from Output fields (so numbers always match the card), blocks separated by the line "――――――――" ... use the exact separator
constant used by internal/utilities (`utilities.Separator`; copy its value into a local const, do not import utilities if that creates a cycle) — 4 blocks: TL, EN, JA, footer.
TL block (emoji prefixes like the power post):
```
🚕 Tantiya sa pamasahe ng Grab: Zapote Bridge → Festival Mall (Alabang-Zapote Rd)
💰 ₱308–370 metered taxi · Grab Standard ₱309 · Ayon sa Grab app noong 23:00 (oras sa Maynila)
📈 ▼ ₱5 (-1%) kumpara sa 22:36
📈 ▲ ₱12 (+4%) kumpara kahapon 23:00          <- only when vs_yesterday exists
🚦 Trapiko: magaan (mga 5% na pula)           <- only when Output.Traffic != nil
Quote lang ito mula sa Grab app sa oras na iyon. Hindi opisyal na pamasahe.
```
EN block starts with 🇬🇧 and JA block with 🇯🇵 (same content in English/Japanese using the strings already in Output.Text plus the yesterday helpers; the headline
line contains the headline text, "Grab Standard ₱309"/"グラブ スタンダード ₱309" when Fare.Standard != nil, and the as-of text).
Trend line is mandatory: when direction is "none" print the "no earlier reading yet" text. Footer block: `Details: https://abalol.xyz/presyo-bantay/` newline
`Source: Grab app quote at that time · Not an official fare · Map: Grab app screenshot · Hindi opisyal na pamasahe`.
Route names come from Output.Route (from/to); shorten "Festival Supermall Alabang" to "Festival Mall" and "Alabang-Zapote Road" to "Alabang-Zapote Rd" in the caption only.
`CheckCaption(text string, o *Output) []string` (problems, empty = ok): 4 blocks, none empty; no "$", "NaN", nested parentheses; every ₱ amount and every N% and every HH:MM
in the text must be derivable from Output values (headline min/max/standard rounded, trend deltas and pct rounded, as_of_hhmm, previous_hhmm, yesterday hhmm, traffic red pct);
each of the three language blocks must contain a trend line (▲/▼/＝ or the none text) and the disclaimer wording (TL "Hindi opisyal", EN "Not an official fare", JA "公式運賃ではありません");
a traffic line (🚦) is present iff Output.Traffic != nil; the only URL is the site URL; no "ssh", "/home", "@", IPs.

## 4. Renderer `internal/fbchart/grab.go`
`RenderGrabFare(o *grabfare.Output, maps []image.Image, sample bool) (*image.RGBA, error)` – 1080x1350 in the exact style of RenderPowerAlert (read power.go, water.go and
utilities.go: cream background, utilHeader/utilHeaderAt, utilTitleAt, billCard panel, pill, fillTriangle, cdraw/wrapLines/cfitSize helpers, utilFooter, Montserrat+Noto fallback so JA works).
Error when o is not visible or o.Fare == nil. Layout (top to bottom; compute y positions, no overlaps, nothing below footerTop = H-118):
- optional red banner when `sample` is true (like pastSample): "SAMPLE — trial data, not a live post" (then dy=90 as in power.go).
- header "Presyo Bantay", title (TL) "Tantiya sa pamasahe ng Grab", subtitle "Zapote Bridge → Festival Mall · Alabang-Zapote Rd".
- white panel (billCard): big "₱308–370" (ExtraBold, ~120 px, fit to width), caption under it "Metered taxi · Grab app quote" (Medium ~30), a green pill "Grab Standard ₱309" (only when Standard != nil), and the as-of line "Ayon sa Grab app noong 23:00 · oras sa Maynila" (Medium ~28, mutCol).
- trend rows (always at least one; ~30 px): coloured triangle (up = red, down = green, flat = grey square/bar, none = grey dot) + TL text "₱5 (-1%) kumpara sa 22:36"; second row for vs_yesterday when present "₱12 (+4%) kumpara kahapon 23:00". Colours: up red (price rising is bad), down green.
- traffic chip only when o.Traffic != nil: pill "Trapiko: magaan" coloured light=green, moderate=amber (#C98A00), heavy=red, severe=redDk.
- map strip: len(maps) tiles side by side (max 3), each (W-2*marginL-(n-1)*16)/n wide, height from the 720:744 aspect, scaled with golang.org/x/image/draw CatmullRom (the images are drawn unmodified: no crop, no overlay, no tint), rounded 14 px corners and a 2 px sepCol border; below it the small label "Map: Grab app screenshot" (Medium 22, mutCol). With 0 maps the strip is omitted and the remaining blocks are spread vertically.
- footer: utilFooter("Source: Grab app quote at that time", "Hindi opisyal na pamasahe · Not an official fare").
Vertical spacing: distribute free space evenly like power.go does. Tests: renders 1080x1350 for 0/2/3 maps and with/without traffic/yesterday/sample; invisible output errors; a map tile region contains the source pixels (spot check after scaling tolerance); text-fit smoke (no panic with very long numbers); EncodeCompact works on the result.

## 5. Command `presyo grabcard` (`cmd/presyo/cmd_grabcard.go`, register in main.go, README row)
`presyo grabcard -out-dir DIR [-api-url URL] [-token-env NAME] [-env-file FILE] [-hours 26] [-now RFC3339] [-input-file FILE -maps-dir DIR] [-max-age 3h] [-stale-after 3h] [-sample] [-no-maps]`
- Config exactly like `grabfare` (flags/env/env-file only; nothing hardcoded; never print URL/token; print only generic messages).
- Fetch recent JSON (or -input-file), pick the newest candidate from SelectCardRun, fetch its images (or read `{maps-dir}/{ts}_{kind}.webp`), decode webp, drop marker images with HasDeviceMarker, PickMaps; if a candidate ends with fewer than 2 clean maps go to the next candidate; if none -> exit 4 "no eligible run" (write nothing). `-no-maps` ignores maps and uses the newest usable fare run (fare-only card). Then `BuildFromRun`, render PNG via fbchart.RenderGrabFare + `fbchart.EncodeCompact`, caption via RenderCaption, run CheckCaption (exit 3 and write nothing but `grab-card.check.json` when problems exist).
- Outputs in DIR (atomic writes): `grab-card.png`, `grab-card.txt` (caption), `grab-card.check.json` ({"ok":bool,"problems":[...]}), `grab-card.json` (run ts, chosen map kinds, dropped map kinds with reason "device-marker", whether traffic was included or dropped with its TrafficSane reason, as_of). Exit 0 ok.
- Tests (httptest API with a fake `/v1/grab/map/...` endpoint and Bearer check; fixtures generated in-test): happy path with 3 maps; marker image dropped; overlay-traffic run skipped in favour of an older clean run; traffic insane (non-overlay) -> card without traffic; no maps -> exit 4; checker failure path; no token/URL in outputs/stderr; output scan has no server identifiers (no "http" except the site URL in the caption, no "/home", "ssh").
Keep gofmt clean; `go vet ./...` and `go test ./internal/grabfare ./internal/fbchart ./cmd/presyo` must pass.

## Map top strip (added)
The renderer leaves out the top `fbchart.MapTopCutPx` (130 px at the 720 px native width) of every map image: the Grab group-ride bar and back arrow. It scales only the sub-rectangle below that strip; the source image is never modified (`TestRenderGrabFareCutsTopStrip`). Tile aspect follows the cut image.

## Hourly auto post (`grabcard -auto`)

One cron tick of the hourly Grab fare post, run through `scripts/grabpost.sh` at `10,15,20 5-21 * * *` (Manila).

Gates (a skip exits 0 and says why on stderr):
- `PRESYO_GRAB_FB=1` is required to post (off by default). `-dry-run` needs no gate, no FB credentials and writes no state.
- window `05:00-22:00` Manila, `PRESYO_GRAB_POST_WINDOW=HH:MM-HH:MM` or `-window` to change it; tick minutes `:10-:29` only.
- kill switch: the existing `fbpost.disabled` file.
- `-sample` is refused when posting.

Ladder per hour (data must be from a run captured in the CURRENT Manila hour and at most 40 min old; never an older reading):
- `:10`, `:15` card with that run's clean maps (>= 2 after the device-marker filter and the top-strip cut). No fresh maps: exit 4 and wait.
- `:20`..`:29` maps if they appeared, otherwise a fare-only card (`Fare lang ngayong oras · walang mapa`). No fare for the hour (pending/absent): exit 4, skip the hour.
- after `:29` the hour is skipped.

Spacing (state file `fbpost.json`, entry `region="grab"`, `as_of="YYYY-MM-DDTHH"`):
- one Grab post per Manila hour (later ticks no-op, idempotent);
- at least 50 min since the previous Grab post (5 min grace, same convention as the other gap rules);
- the tick is skipped while any non-Grab post is younger than 15 min.
- Grab posts are excluded from every other gap rule (`lastNonGrabPost`): DA digest 75 min, `fbpost`, `fbutil` (emergency 30 min), `fbweekly`. DA digest caps/logic are otherwise unchanged.

Posting reuses `postPhoto` (Graph API `/photos`) and the state lock (`<state>.lock`); the page token is read from `fb.env`/env, never printed (errors are scrubbed). `-dry-run -sim-record` appends a `post_id="dry-run"` entry to the given `-state` to simulate idempotency: use a scratch state file only.

## Focus band (Colours / SM Southmall / Village Square)
The watcher writes `<ts>_focus.webp` (720x280, cut from map3 below the device marker). The API lists it as kind `focus` in `map_images` (after the map kinds) and serves `/v1/grab/map/<ts>/focus.webp` under the same strict rules.
- With a clean focus image the card shows it as a full-width (940 px) strip under the fare/trend rows, captioned "Colours · SM Southmall · Village Square"; the usual 3-map row stays exactly as large as before below it (source label "Map: Grab app screenshot"). No top cut is applied to the focus band (already cropped). A focus image with the device dot is dropped.
- Layout: spacing is reclaimed first (panel padding, 14 px gaps), then the card grows taller by exactly what is missing (about 1080x1532 with focus + 3 maps, no traffic chip). Without focus the card is unchanged (1080x1350). A run with focus is eligible even with fewer than 2 clean thumbnails.
- Caption gets one line per language (TL/EN/JA) naming the stretch. Fare-only cards (`-no-maps`, the :20 fallback) never include it.
