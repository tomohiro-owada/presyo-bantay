import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import "./style.css";
import {
  LANG_KEY,
  STRINGS,
  categoryLabel,
  loadLang,
  saveLang,
  type Lang,
} from "./i18n";
import { asofLookup, laggedCorrelation, type NullReason } from "./stats";
import {
  dealsTextToLines,
  isDealsSubheading,
  splitPesoSegments,
  type DealsFile,
} from "./deals";
import {
  dayLabelForLang,
  dayUrl,
  latestDay,
  recentDays,
  type DaysFile,
} from "./days";
import { resolveDeals } from "./dealsSource";

/* ---------- Data shapes (see presyo_bantay/export_site.py) ---------- */

interface MetaMarket {
  id: number;
  name: string;
  short: string;
  city: string | null;
}

interface MetaCommodity {
  id: number;
  name: string;
  unit: string;
  category: string;
  group: string | null;
}

interface Meta {
  updated: string | null;
  generated_at: string;
  date_min: string | null;
  date_max: string | null;
  markets: MetaMarket[];
  commodities: MetaCommodity[];
  default_markets: number[];
  fx_source: string;
  price_source: string;
}

interface SeriesFile {
  d: string[];
  m: Record<string, Array<number | null>>;
}

interface FxFile {
  d: string[];
  r: number[];
}

interface WeekEntry {
  p: number | null;
  b: number | null;
  c: number | null;
}

interface WeekItem {
  id: number;
  name: string;
  unit: string;
  group: string | null;
  m: Record<string, WeekEntry>;
  cheap4: { p: number | null; m: string[] };
  cheap_all: { p: number | null; m: string[] };
}

interface WeekFile {
  latest: string | null;
  baseline: string | null;
  items: WeekItem[];
}

/* ---------- State ---------- */

const RANGES = ["30", "90", "180", "all"] as const;
type RangeKey = (typeof RANGES)[number];
const CORR_LAGS = [0, 7, 14, 30];

interface State {
  commodityId: number | null;
  markets: number[];
  range: RangeKey;
  fx: boolean;
  corrMarket: number | null;
}

// Okabe-Ito colorblind-friendly palette (no near-black), ordered so the
// first few selections are maximally distinct on a white background.
// uPlot multiplies `width` by devicePixelRatio internally, so width 2 is
// already DPR-scaled (crisp at devicePixelRatio 1 without extra scaling).
const PALETTE = [
  "#0072B2",
  "#D55E00",
  "#009E73",
  "#CC79A7",
  "#56B4E9",
  "#E69F00",
  "#F0E442",
  "#999999",
];

const FX_COLOR = "#6b7280";

const DATA = `${import.meta.env.BASE_URL}data/`;

let lang: Lang = loadLang();
let meta: Meta | null = null;
let week: WeekFile | null = null;
let fx: FxFile | null = null;
let fxFailed = false;
let deals: DealsFile | null = null;
let dealsMissing = false;
let days: DaysFile | null = null;
const seriesCache = new Map<number, SeriesFile>();
const state: State = {
  commodityId: null,
  markets: [],
  range: "90",
  fx: false,
  corrMarket: null,
};
let plot: uPlot | null = null;
let lastChart: {
  dates: string[];
  labels: string[];
  values: Array<Array<number | null>>;
  fxVals: Array<number | null> | null;
} | null = null;
// Market chips: collapsed shows selected chips + the first row of the rest,
// with a toggle to expand. Never clips chip text (hides whole chips).
let chipsExpanded = false;

/* ---------- Helpers ---------- */

function $(id: string): HTMLElement {
  const el = document.getElementById(id);
  if (!el) throw new Error(`missing element #${id}`);
  return el;
}

function esc(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function fmtPeso(v: number | null): string {
  if (v === null || v === undefined || Number.isNaN(v)) return STRINGS[lang].noPrice;
  return (
    "₱" +
    v.toLocaleString("en-PH", {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    })
  );
}

function fmtDate(iso: string): string {
  const [y, m, d] = iso.split("-").map(Number);
  const dt = new Date(Date.UTC(y, m - 1, d));
  try {
    const locale = lang === "tl" ? "fil-PH" : lang === "ja" ? "ja-JP" : "en-PH";
    return dt.toLocaleDateString(locale, {
      month: "short",
      day: "numeric",
      year: "numeric",
      timeZone: "UTC",
    });
  } catch {
    return iso;
  }
}

function marketById(id: number): MetaMarket | undefined {
  return meta?.markets.find((m) => m.id === id);
}

function commodityById(id: number): MetaCommodity | undefined {
  return meta?.commodities.find((c) => c.id === id);
}

async function fetchJson<T>(path: string): Promise<T> {
  const res = await fetch(DATA + path);
  if (!res.ok) {
    throw new Error(`HTTP ${res.status} for ${path}`);
  }
  return (await res.json()) as T;
}

/* ---------- URL state ---------- */

function readURL(): void {
  const q = new URLSearchParams(location.search);
  const c = q.get("c");
  const m = q.get("m");
  const r = q.get("r");
  const f = q.get("fx");
  if (c !== null && /^\d+$/.test(c)) state.commodityId = Number(c);
  if (m !== null) {
    const ids = m
      .split(",")
      .map((s) => Number(s))
      .filter((n) => Number.isInteger(n));
    if (ids.length > 0) state.markets = ids;
  }
  if (r !== null && (RANGES as readonly string[]).includes(r)) {
    state.range = r as RangeKey;
  }
  state.fx = f === "1";
}

function writeURL(): void {
  const q = new URLSearchParams();
  if (state.commodityId !== null) q.set("c", String(state.commodityId));
  if (state.markets.length > 0) q.set("m", state.markets.join(","));
  q.set("r", state.range);
  if (state.fx) q.set("fx", "1");
  history.replaceState(null, "", `?${q.toString()}`);
}

/* ---------- Defaults ---------- */

function defaultCommodityId(): number | null {
  if (!meta || meta.commodities.length === 0) return null;
  const bangus = meta.commodities.find(
    (c) => c.name.toLowerCase() === "bangus",
  );
  if (bangus) return bangus.id;
  const fallback = meta.commodities.find(
    (c) => c.name === "Premium Rice (Imported)",
  );
  return (fallback ?? meta.commodities[0]).id;
}

/* ---------- Static strings ---------- */

function applyStrings(): void {
  const s = STRINGS[lang];
  document.documentElement.lang = lang === "tl" ? "fil" : lang;
  document.title = "Presyo Bantay";
  $("tagline").textContent = s.tagline;
  $("lang-label").textContent = s.languageLabel;
  const btl = $("lang-tl");
  const ben = $("lang-en");
  const bja = $("lang-ja");
  btl.textContent = s.langTaglish;
  ben.textContent = s.langEnglish;
  bja.textContent = s.langJapanese;
  btl.setAttribute("aria-pressed", String(lang === "tl"));
  ben.setAttribute("aria-pressed", String(lang === "en"));
  bja.setAttribute("aria-pressed", String(lang === "ja"));
  $("commodity-label").textContent = s.commodityLabel;
  $("markets-legend").textContent = s.marketsLabel;
  $("range-label").textContent = s.rangeLabel;
  $("range-all").textContent = s.rangeAll;
  document
    .querySelectorAll<HTMLButtonElement>("#range-group button[data-range]")
    .forEach((b) => {
      const r = b.dataset.range ?? "";
      b.textContent = r === "all" ? s.rangeAll : `${r}`;
      b.title = r === "all" ? s.rangeAll : `${r} ${s.rangeDays}`;
      b.setAttribute("aria-pressed", String(state.range === r));
    });
  $("fx-label").textContent = s.fxLabel;
  ($("fx-toggle") as HTMLInputElement).checked = state.fx;
  $("chart-h").textContent = s.chartTitle;
  $("chart-hint").textContent = s.chartHint;
  $("week-h").textContent = s.weekTitle;
  $("corr-h").textContent = s.corrTitle;
  $("corr-desc").textContent = s.corrDesc;
  $("corr-market-label").textContent = s.corrMarketLabel;
  $("corr-caution").textContent = s.corrCaution;
  $("foot-source").textContent = s.footerSource;
  $("foot-fx").textContent = s.footerFx;
  $("foot-stall").textContent = s.footerStall;
  $("foot-unofficial").textContent = s.footerUnofficial;
  $("foot-privacy").textContent = s.footerPrivacy;
  $("foot-updated-label").textContent = s.footerUpdated;
  $("foot-range-note").textContent = s.rangeNote;
  $("foot-code").textContent = s.footerCode;
  renderDeals();
  renderDays();
  if (meta?.updated) {
    const t = $("foot-updated");
    t.textContent = meta.updated;
    t.setAttribute("datetime", meta.updated);
  }
}

/* ---------- Deals card ---------- */

function renderDeals(): void {
  const card = document.getElementById("deals-card");
  if (!card) return;
  const s = STRINGS[lang];
  // Primary source: days.json latest South text (same as the Facebook
  // post). deals.json is only used when days.json is missing/empty.
  const resolved = resolveDeals(deals, dealsMissing, days, lang);
  const text = resolved.text;
  const date = resolved.date;
  const fromDays = resolved.fromDays;
  const latest = latestDay(days);
  if (text === null) {
    card.hidden = true;
    return;
  }
  const lines = dealsTextToLines(text);
  if (lines.length === 0) {
    card.hidden = true;
    return;
  }
  card.hidden = false;
  $("deals-h").textContent = s.dealsTitle;
  try {
    $("deals-date").textContent = fmtDate(date);
  } catch {
    $("deals-date").textContent = date;
  }
  const body = $("deals-body");
  body.innerHTML = "";
  // "• " lines become list items; everything else plain paragraphs.
  // textContent only — never innerHTML of model text (XSS safety).
  // Peso amounts (incl. sign/parens) are wrapped in nowrap spans so
  // "(-₱90)" / "（-₱90）" / "₱130" never break across lines.
  const appendPesoSafe = (el: HTMLElement, text: string): void => {
    for (const seg of splitPesoSegments(text)) {
      if (seg.nowrap) {
        const sp = document.createElement("span");
        sp.style.whiteSpace = "nowrap";
        sp.textContent = seg.text;
        el.append(sp);
      } else {
        el.append(document.createTextNode(seg.text));
      }
    }
  };
  let ul: HTMLUListElement | null = null;
  const flushUl = () => {
    ul = null;
  };
  for (const ln of lines.slice(1)) {
    if (ln.startsWith("• ")) {
      if (!ul) {
        ul = document.createElement("ul");
        ul.className = "deals-list";
        body.append(ul);
      }
      const li = document.createElement("li");
      appendPesoSafe(li, ln.slice(2).trim());
      ul.append(li);
    } else {
      flushUl();
      const p = document.createElement("p");
      p.className = isDealsSubheading(ln) ? "deals-sub" : "deals-line";
      appendPesoSafe(p, ln);
      body.append(p);
    }
  }
  const foot = $("deals-foot");
  foot.textContent =
    !fromDays && deals?.text_source === "llm"
      ? `${s.dealsFooter} ${s.dealsFooterAI}`
      : s.dealsFooter;
  // Link to the latest day page at the bottom of the deals card.
  const dayLink = document.getElementById("deals-day-link");
  if (dayLink instanceof HTMLAnchorElement) {
    if (latest) {
      dayLink.hidden = false;
      dayLink.href = dayUrl(import.meta.env.BASE_URL, latest.path);
      dayLink.textContent = s.dealsDayLink;
    } else {
      dayLink.hidden = true;
    }
  }
}

/* ---------- Daily reports card ---------- */

function renderDays(): void {
  const card = document.getElementById("days-card");
  if (!card) return;
  const s = STRINGS[lang];
  const latest = latestDay(days);
  if (!latest) {
    card.hidden = true;
    return;
  }
  card.hidden = false;
  $("days-h").textContent = s.daysTitle;
  const latestA = $("days-latest");
  if (latestA instanceof HTMLAnchorElement) {
    latestA.href = dayUrl(import.meta.env.BASE_URL, latest.path);
    latestA.textContent = s.daysLatest(dayLabelForLang(latest, lang));
  }
  const list = $("days-list");
  list.innerHTML = "";
  for (const e of recentDays(days, 7)) {
    const li = document.createElement("li");
    const a = document.createElement("a");
    a.href = dayUrl(import.meta.env.BASE_URL, e.path);
    a.textContent = dayLabelForLang(e, lang);
    li.append(a);
    list.append(li);
  }
  const all = $("days-all");
  if (all instanceof HTMLAnchorElement) {
    all.href = dayUrl(import.meta.env.BASE_URL, "d/");
    all.textContent = s.daysAll;
  }
}

/* ---------- Controls ---------- */

function buildCommoditySelect(): void {
  const sel = $("commodity") as HTMLSelectElement;
  sel.innerHTML = "";
  if (!meta) return;
  const seen = new Set<string>();
  const order: string[] = [];
  for (const c of meta.commodities) {
    const cat = c.category || "other";
    if (!seen.has(cat)) {
      seen.add(cat);
      order.push(cat);
    }
  }
  for (const cat of order) {
    const group = document.createElement("optgroup");
    group.label = categoryLabel(lang, cat);
    for (const c of meta.commodities.filter(
      (x) => (x.category || "other") === cat,
    )) {
      const opt = document.createElement("option");
      opt.value = String(c.id);
      opt.textContent = c.name;
      if (c.id === state.commodityId) opt.selected = true;
      group.appendChild(opt);
    }
    sel.appendChild(group);
  }
}

function buildMarketChips(): void {
  const box = $("market-chips");
  box.innerHTML = "";
  if (!meta) return;
  for (const m of meta.markets) {
    const b = document.createElement("button");
    b.type = "button";
    b.textContent = m.short;
    b.title = m.name;
    b.dataset.mid = String(m.id);
    b.setAttribute("aria-pressed", String(state.markets.includes(m.id)));
    b.addEventListener("click", () => {
      const i = state.markets.indexOf(m.id);
      if (i >= 0) {
        state.markets.splice(i, 1);
      } else {
        state.markets.push(m.id);
        state.markets.sort(
          (a, b2) =>
            (meta?.markets.findIndex((x) => x.id === a) ?? 0) -
            (meta?.markets.findIndex((x) => x.id === b2) ?? 0),
        );
      }
      if (
        state.corrMarket !== null &&
        !state.markets.includes(state.corrMarket)
      ) {
        state.corrMarket = state.markets[0] ?? null;
      }
      writeURL();
      void renderAll(false);
    });
    box.appendChild(b);
  }
  ensureChipsToggle();
  updateChipsVisibility();
}

function ensureChipsToggle(): void {
  if (document.getElementById("chips-toggle")) return;
  const box = $("market-chips");
  const toggle = document.createElement("button");
  toggle.type = "button";
  toggle.id = "chips-toggle";
  toggle.className = "chips-toggle";
  toggle.setAttribute("aria-expanded", "false");
  toggle.setAttribute("aria-controls", "market-chips");
  toggle.addEventListener("click", () => {
    chipsExpanded = !chipsExpanded;
    updateChipsVisibility();
  });
  box.after(toggle);
}

/**
 * Collapsed: always show selected chips plus the first row of the rest;
 * hide further chips wholesale (display:none) so no chip text is clipped.
 * Expanded: show everything (flex-wrap, no height cap).
 */
function updateChipsVisibility(): void {
  const box = $("market-chips");
  const toggle = document.getElementById(
    "chips-toggle",
  ) as HTMLButtonElement | null;
  if (!toggle) return;
  const s = STRINGS[lang];
  const btns = [...box.querySelectorAll<HTMLButtonElement>("button")];
  btns.forEach((b) => b.classList.remove("is-hidden"));
  if (btns.length === 0) {
    toggle.hidden = true;
    return;
  }
  if (chipsExpanded) {
    toggle.hidden = false;
    toggle.textContent = s.chipsLess;
    toggle.setAttribute("aria-expanded", "true");
    return;
  }
  // Collapsed: measure with everything visible, then hide whole chips
  // beyond the first unselected row. Selected chips (order:-1) stay on top.
  const unselected = btns.filter(
    (b) => b.getAttribute("aria-pressed") !== "true",
  );
  if (unselected.length === 0) {
    toggle.hidden = true;
    toggle.setAttribute("aria-expanded", "false");
    return;
  }
  const firstTop = Math.min(...unselected.map((b) => b.offsetTop));
  let hidden = 0;
  for (const b of unselected) {
    if (b.offsetTop !== firstTop) {
      b.classList.add("is-hidden");
      hidden++;
    }
  }
  if (hidden === 0) {
    toggle.hidden = true;
  } else {
    toggle.hidden = false;
    toggle.textContent = s.chipsMore(hidden);
  }
  toggle.setAttribute("aria-expanded", "false");
}

function buildCorrMarketSelect(): void {
  const sel = $("corr-market") as HTMLSelectElement;
  sel.innerHTML = "";
  const options =
    state.markets.length > 0
      ? state.markets
      : (meta?.markets.map((m) => m.id) ?? []);
  if (state.corrMarket === null || !options.includes(state.corrMarket)) {
    state.corrMarket = options[0] ?? null;
  }
  for (const id of options) {
    const m = marketById(id);
    if (!m) continue;
    const opt = document.createElement("option");
    opt.value = String(id);
    opt.textContent = m.short;
    opt.title = m.name;
    if (id === state.corrMarket) opt.selected = true;
    sel.appendChild(opt);
  }
}

function syncControls(): void {
  const sel = $("commodity") as HTMLSelectElement;
  if (state.commodityId !== null) sel.value = String(state.commodityId);
  document
    .querySelectorAll<HTMLButtonElement>("#market-chips button")
    .forEach((b) => {
      const mid = Number(b.dataset.mid);
      b.setAttribute("aria-pressed", String(state.markets.includes(mid)));
    });
  document
    .querySelectorAll<HTMLButtonElement>("#range-group button[data-range]")
    .forEach((b) => {
      b.setAttribute(
        "aria-pressed",
        String(state.range === (b.dataset.range ?? "")),
      );
    });
  ($("fx-toggle") as HTMLInputElement).checked = state.fx;
  buildCorrMarketSelect();
  updateChipsVisibility();
}

/* ---------- Data loading ---------- */

function setStatus(text: string, retry: (() => void) | null = null): void {
  const el = $("chart-status");
  el.innerHTML = "";
  if (!text) return;
  el.append(document.createTextNode(text));
  if (retry) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.textContent = STRINGS[lang].retry;
    btn.addEventListener("click", retry);
    el.append(btn);
  }
}

async function loadSeries(id: number): Promise<SeriesFile> {
  const hit = seriesCache.get(id);
  if (hit) return hit;
  const data = await fetchJson<SeriesFile>(`series/c${id}.json`);
  seriesCache.set(id, data);
  return data;
}

async function loadFx(): Promise<FxFile | null> {
  if (fx !== null) return fx;
  if (fxFailed) return null;
  try {
    fx = await fetchJson<FxFile>("fx.json");
  } catch {
    fxFailed = true;
    fx = null;
  }
  return fx;
}

/* ---------- Range windowing ---------- */

function windowDates(dates: string[], range: RangeKey): string[] {
  if (range === "all" || dates.length === 0) return dates;
  const days = Number(range);
  const last = dates[dates.length - 1];
  const [y, m, d] = last.split("-").map(Number);
  const cutoffMs = Date.UTC(y, m - 1, d) - (days - 1) * 86_400_000;
  const cutoff = new Date(cutoffMs).toISOString().slice(0, 10);
  return dates.filter((x) => x >= cutoff);
}

/* ---------- Chart ---------- */

function destroyPlot(): void {
  if (plot) {
    plot.destroy();
    plot = null;
  }
  $("chart").innerHTML = "";
}

function updateReadout(idx: number | null | undefined): void {
  const box = $("chart-readout");
  // uPlot fires setCursor with idx == null on init and on mouse-leave;
  // fall back to the latest values so the readout is never blank.
  if (!lastChart || lastChart.dates.length === 0) {
    box.textContent = "";
    return;
  }
  const i =
    idx === null || idx === undefined
      ? lastChart.dates.length - 1
      : Math.max(0, Math.min(idx, lastChart.dates.length - 1));
  const parts = [fmtDate(lastChart.dates[i])];
  lastChart.labels.forEach((label, k) => {
    parts.push(`${label}: ${fmtPeso(lastChart!.values[k][i] ?? null)}`);
  });
  if (lastChart.fxVals) {
    const v = lastChart.fxVals[i];
    parts.push(
      `USD/PHP: ${v === null || v === undefined ? STRINGS[lang].noPrice : v.toFixed(2)}`,
    );
  }
  box.textContent = parts.join(" · ");
}

/**
 * uPlot points filter: draw a dot only for isolated values (a non-null
 * value whose immediate neighbours are both missing). With spanGaps:false
 * such values draw no line segment, so without points they would be
 * invisible. Dense runs draw no points (returns []), keeping lines clean.
 */
function isolatedPointsFilter(u: uPlot, si: number): number[] | null {
  const col = u.data[si] as Array<number | null>;
  const out: number[] = [];
  for (let i = 0; i < col.length; i++) {
    const v = col[i];
    if (v === null || v === undefined) continue;
    const prev = i > 0 ? col[i - 1] : null;
    const next = i < col.length - 1 ? col[i + 1] : null;
    if (
      (prev === null || prev === undefined) &&
      (next === null || next === undefined)
    ) {
      out.push(i);
    }
  }
  return out;
}

async function renderChart(): Promise<void> {
  destroyPlot();
  lastChart = null;
  $("chart-legend").innerHTML = "";
  $("chart-empty").textContent = "";
  updateReadout(null);
  if (!meta || state.commodityId === null) return;

  const id = state.commodityId;
  let series: SeriesFile;
  try {
    setStatus(STRINGS[lang].loading);
    series = await loadSeries(id);
  } catch {
    setStatus(STRINGS[lang].loadError, () => void renderAll(false));
    return;
  }
  setStatus("");

  const dates = windowDates(series.d, state.range);
  if (dates.length === 0 || state.markets.length === 0) {
    $("chart-empty").textContent =
      state.markets.length === 0
        ? STRINGS[lang].marketsLabel
        : STRINGS[lang].noDataForMarket("");
    return;
  }
  const fullIdx = new Map(series.d.map((d, i) => [d, i] as const));

  const x: number[] = dates.map((d) => {
    const [y, m, dd] = d.split("-").map(Number);
    return Date.UTC(y, m - 1, dd) / 1000;
  });

  const labels: string[] = [];
  const values: Array<Array<number | null>> = [];
  const colors: string[] = [];
  const emptyMarkets: string[] = [];
  state.markets.forEach((mid, k) => {
    const col = series.m[String(mid)] ?? [];
    const vals = dates.map((d) => {
      const j = fullIdx.get(d);
      const v = j === undefined ? null : (col[j] ?? null);
      return typeof v === "number" ? v : null;
    });
    const m = marketById(mid);
    const label = m?.short ?? String(mid);
    if (vals.every((v) => v === null)) {
      emptyMarkets.push(m?.name ?? label);
      return;
    }
    labels.push(label);
    values.push(vals);
    colors.push(PALETTE[k % PALETTE.length]);
  });

  if (labels.length === 0) {
    $("chart-empty").textContent = STRINGS[lang].noDataForMarket(
      emptyMarkets.join(", "),
    );
    return;
  }
  if (emptyMarkets.length > 0) {
    $("chart-empty").textContent = STRINGS[lang].noDataForMarket(
      emptyMarkets.join(", "),
    );
  }

  // FX overlay, aligned per date via as-of lookup (weekends -> Friday).
  let fxVals: Array<number | null> | null = null;
  if (state.fx) {
    const fxData = await loadFx();
    if (fxData && fxData.d.length > 0) {
      const byDate = new Map<string, number>();
      fxData.d.forEach((d, i) => byDate.set(d, fxData.r[i]));
      const sorted = [...byDate.keys()].sort();
      fxVals = dates.map((d) => asofLookup(byDate, sorted, d));
      if (fxVals.every((v) => v === null)) fxVals = null;
    }
  }

  const data: uPlot.AlignedData = [x, ...values];
  if (fxVals) data.push(fxVals);

  const seriesDefs: uPlot.Series[] = [{}];
  labels.forEach((label, k) => {
    seriesDefs.push({
      label,
      stroke: colors[k],
      width: 2,
      spanGaps: false,
      points: {
        show: false,
        filter: isolatedPointsFilter,
        size: 7,
        width: 0,
        fill: colors[k],
        stroke: colors[k],
      },
    });
  });
  if (fxVals) {
    seriesDefs.push({
      label: "USD/PHP",
      scale: "fx",
      stroke: FX_COLOR,
      width: 2,
      dash: [6, 4],
      spanGaps: false,
      points: { show: false },
    });
  }

  const box = $("chart");
  const width = Math.max(280, box.clientWidth || 320);
  const opts: uPlot.Options = {
    width,
    height: 280,
    scales: {
      x: { time: true },
      ...(fxVals ? { fx: {} } : {}),
    },
    axes: [
      {},
      {
        values: (_u, splits) =>
          splits.map((v) => (v === null ? "" : `₱${Math.round(v)}`)),
      },
      ...(fxVals
        ? [
            {
              scale: "fx",
              side: 1 as const,
              grid: { show: false },
              values: (_u: uPlot, splits: number[]) =>
                splits.map((v) => (v === null ? "" : v.toFixed(1))),
            },
          ]
        : []),
    ],
    cursor: { show: true, x: true, y: false },
    legend: { show: false },
    series: seriesDefs,
    hooks: {
      setCursor: [
        (u) => {
          updateReadout(u.cursor.idx);
        },
      ],
    },
  };

  lastChart = { dates, labels, values, fxVals };
  plot = new uPlot(opts, data, box);

  const legend = $("chart-legend");
  labels.forEach((label, k) => {
    const span = document.createElement("span");
    span.className = "lg";
    const sw = document.createElement("span");
    sw.className = "sw";
    sw.style.background = colors[k];
    span.append(sw, document.createTextNode(label));
    legend.append(span);
  });
  if (fxVals) {
    const span = document.createElement("span");
    span.className = "lg";
    const sw = document.createElement("span");
    sw.className = "sw";
    sw.style.background =
      `repeating-linear-gradient(90deg,${FX_COLOR} 0 4px,transparent 4px 7px)`;
    span.append(sw, document.createTextNode("USD/PHP"));
    legend.append(span);
  }
  // Default readout: latest values.
  updateReadout(dates.length - 1);
}

/* ---------- Week table ---------- */

function renderWeek(): void {
  const body = $("week-body");
  body.innerHTML = "";
  const s = STRINGS[lang];
  if (!week || state.commodityId === null) return;
  const item = week.items.find((x) => x.id === state.commodityId);
  const comm = commodityById(state.commodityId);
  const unitText = $("week-unit");
  if (comm) {
    unitText.textContent =
      comm.unit === "per piece" ? s.weekUnitPc : s.weekUnitKg;
  } else {
    unitText.textContent = "";
  }
  if (!item) {
    body.textContent = s.noDataForMarket(
      comm?.name ?? String(state.commodityId),
    );
    return;
  }
  const table = document.createElement("table");
  table.className = "data";
  const thead = document.createElement("thead");
  const hr = document.createElement("tr");
  for (const [text, cls] of [
    [s.thMarket, ""],
    [s.thLatest, "num"],
    [s.thChange, "num"],
  ] as const) {
    const th = document.createElement("th");
    th.textContent = text;
    if (cls) th.scope = "col";
    if (cls) th.className = cls;
    else th.scope = "col";
    hr.append(th);
  }
  thead.append(hr);
  table.append(thead);
  // Highlight the cheapest among the selected markets shown here.
  let minP: number | null = null;
  for (const mid of state.markets) {
    const p = item.m[String(mid)]?.p ?? null;
    if (p !== null && (minP === null || p < minP)) minP = p;
  }
  const tbody = document.createElement("tbody");
  for (const mid of state.markets) {
    const m = marketById(mid);
    const entry = item.m[String(mid)];
    const tr = document.createElement("tr");
    const isCheap =
      entry?.p !== null && entry?.p !== undefined && entry.p === minP;
    if (isCheap) tr.className = "cheap";
    const tdM = document.createElement("td");
    tdM.textContent = m?.short ?? String(mid);
    tdM.title = m?.name ?? "";
    if (isCheap) {
      const badge = document.createElement("span");
      badge.className = "badge";
      badge.textContent = s.cheapestBadge;
      tdM.append(badge);
    }
    const tdP = document.createElement("td");
    tdP.className = "num";
    tdP.textContent = fmtPeso(entry?.p ?? null);
    const tdC = document.createElement("td");
    tdC.className = "num";
    if (entry?.c === null || entry?.c === undefined) {
      tdC.textContent =
        entry?.p === null || entry?.p === undefined
          ? s.noPrice
          : `— ${s.noBaseline}`;
    } else if (entry.c > 0) {
      tdC.textContent = `▲${entry.c.toFixed(1)}%`;
      tdC.classList.add("up");
    } else if (entry.c < 0) {
      tdC.textContent = `▼${Math.abs(entry.c).toFixed(1)}%`;
      tdC.classList.add("down");
    } else {
      tdC.textContent = "0.0%";
    }
    tr.append(tdM, tdP, tdC);
    tbody.append(tr);
  }
  table.append(tbody);
  body.append(table);
}

/* ---------- Correlation panel ---------- */

function interpret(
  r: number | null,
  n: number,
  reason: NullReason | null = null,
): string {
  const s = STRINGS[lang];
  // "kulang ang data" / "not enough data" is only for small samples.
  if (n < 10) return s.corrSparse;
  // With plenty of pairs but no r, one series had zero variance, e.g. a
  // price flat at ₱270 for the whole window.
  if (r === null || reason === "zero_variance") return s.corrFlat;
  const strength =
    Math.abs(r) < 0.3
      ? s.corrWeak
      : Math.abs(r) < 0.7
        ? s.corrModerate
        : s.corrStrong;
  const dir = r >= 0 ? s.corrPositive : s.corrNegative;
  return `${strength}, ${dir}`;
}

async function renderCorr(): Promise<void> {
  const body = $("corr-body");
  body.innerHTML = "";
  const s = STRINGS[lang];
  if (!meta || state.commodityId === null || state.corrMarket === null) return;

  let series: SeriesFile;
  try {
    series = await loadSeries(state.commodityId);
  } catch {
    body.textContent = s.loadError;
    return;
  }
  const fxData = await loadFx();
  if (!fxData || fxData.d.length === 0) {
    body.textContent = s.corrNoFx;
    return;
  }

  const mid = String(state.corrMarket);
  const col = series.m[mid] ?? [];
  const pricePairs: Array<readonly [string, number | null]> = series.d.map(
    (d, i) => [d, (col[i] ?? null) as number | null],
  );
  // Apply the chosen range window (relative to the latest series date).
  const kept = new Set(windowDates(series.d, state.range));
  const windowed = pricePairs.filter(([d]) => kept.has(d));
  const fxPairs: Array<readonly [string, number | null]> = fxData.d.map(
    (d, i) => [d, fxData.r[i] as number],
  );

  const table = document.createElement("table");
  table.className = "data";
  const thead = document.createElement("thead");
  const hr = document.createElement("tr");
  for (const [text, cls] of [
    [s.corrLag, ""],
    [s.corrR, "num"],
    [s.corrN, "num"],
    [s.corrInterp, ""],
  ] as const) {
    const th = document.createElement("th");
    th.scope = "col";
    th.textContent = text;
    if (cls) th.className = cls;
    hr.append(th);
  }
  thead.append(hr);
  table.append(thead);
  const tbody = document.createElement("tbody");
  for (const lag of CORR_LAGS) {
    const { r, n, reason } = laggedCorrelation(windowed, fxPairs, lag);
    const tr = document.createElement("tr");
    const tdL = document.createElement("td");
    tdL.textContent = String(lag);
    const tdR = document.createElement("td");
    tdR.className = "num";
    tdR.textContent = r === null ? "—" : r.toFixed(2);
    const tdN = document.createElement("td");
    tdN.className = "num";
    tdN.textContent = String(n);
    const tdI = document.createElement("td");
    tdI.textContent = interpret(r, n, reason);
    tr.append(tdL, tdR, tdN, tdI);
    tbody.append(tr);
  }
  table.append(tbody);
  body.append(table);
}

/* ---------- Render all ---------- */

async function renderAll(rebuildControls: boolean): Promise<void> {
  if (!meta) return;
  if (rebuildControls) {
    buildCommoditySelect();
    buildMarketChips();
  }
  applyStrings();
  syncControls();
  await renderChart();
  renderWeek();
  await renderCorr();
}

/* ---------- Boot ---------- */

function bindEvents(): void {
  ($("commodity") as HTMLSelectElement).addEventListener("change", (e) => {
    state.commodityId = Number((e.target as HTMLSelectElement).value);
    seriesCache.delete(state.commodityId);
    writeURL();
    void renderAll(false);
  });
  document
    .querySelectorAll<HTMLButtonElement>("#range-group button[data-range]")
    .forEach((b) => {
      b.addEventListener("click", () => {
        state.range = (b.dataset.range ?? "90") as RangeKey;
        writeURL();
        void renderAll(false);
      });
    });
  ($("fx-toggle") as HTMLInputElement).addEventListener("change", (e) => {
    state.fx = (e.target as HTMLInputElement).checked;
    writeURL();
    void renderAll(false);
  });
  ($("corr-market") as HTMLSelectElement).addEventListener("change", (e) => {
    state.corrMarket = Number((e.target as HTMLSelectElement).value);
    void renderCorr();
  });
  const setLang = (l: Lang) => {
    lang = l;
    saveLang(l);
    try {
      localStorage.setItem(LANG_KEY, l);
    } catch {
      /* best-effort */
    }
    buildCommoditySelect();
    void renderAll(false);
  };
  $("lang-tl").addEventListener("click", () => setLang("tl"));
  $("lang-en").addEventListener("click", () => setLang("en"));
  $("lang-ja").addEventListener("click", () => setLang("ja"));

  let timer = 0;
  window.addEventListener("resize", () => {
    window.clearTimeout(timer);
    timer = window.setTimeout(() => {
      updateChipsVisibility();
      void renderChart();
    }, 150);
  });
}

async function boot(): Promise<void> {
  bindEvents();
  applyStrings();
  setStatus(STRINGS[lang].loading);
  try {
    meta = await fetchJson<Meta>("meta.json");
  } catch {
    setStatus(STRINGS[lang].loadError, () => void boot());
    return;
  }
  setStatus("");
  try {
    week = await fetchJson<WeekFile>("week.json");
  } catch {
    week = null;
  }
  try {
    const d = await fetchJson<DealsFile>("deals.json");
    if (d && typeof d.texts?.tl === "string" && typeof d.texts?.en === "string" && typeof d.texts?.ja === "string") {
      deals = d;
    } else {
      dealsMissing = true;
    }
  } catch {
    dealsMissing = true;
  }
  // Daily report pages manifest (missing file => both cards cope).
  try {
    const d = await fetchJson<DaysFile>("days.json");
    if (d && typeof d.latest === "string" && Array.isArray(d.days)) {
      days = d;
    } else {
      days = null;
    }
  } catch {
    days = null;
  }

  readURL();
  // Validate / default the commodity and markets against freshly loaded meta.
  const validComm = meta.commodities.some((c) => c.id === state.commodityId);
  if (!validComm) state.commodityId = defaultCommodityId();
  const validIds = new Set(meta.markets.map((m) => m.id));
  state.markets = state.markets.filter((id) => validIds.has(id));
  if (state.markets.length === 0 && new URLSearchParams(location.search).get("m") === null) {
    state.markets = meta.default_markets.filter((id) => validIds.has(id));
  }
  if (state.markets.length === 0) {
    state.markets = meta.markets.slice(0, 4).map((m) => m.id);
  }
  state.corrMarket =
    state.markets.includes(state.corrMarket ?? -1)
      ? state.corrMarket
      : (state.markets[0] ?? null);
  writeURL();
  await renderAll(true);
}

void boot();
