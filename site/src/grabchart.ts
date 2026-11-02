import type { Lang } from "./i18n";

/** One hour-of-day reading (Manila). */
export interface HourPt {
  h: number;
  min: number;
  max: number;
  era?: number;
}

export interface GrabHourly {
  todayDate: string;
  yesterdayDate: string;
  today: HourPt[];
  /** Null when fewer than MIN_YESTERDAY_POINTS hours are recorded. */
  yesterday: HourPt[] | null;
  /** Yesterday had some points but not enough to draw a line. */
  yesterdayPending: boolean;
}

export const MIN_YESTERDAY_POINTS = 6;

function parseEra(v: unknown): number {
  if (typeof v !== "number" || !Number.isInteger(v) || v < 0) return 0;
  return v;
}

function parsePts(raw: unknown): HourPt[] {
  if (!Array.isArray(raw)) return [];
  const byHour = new Map<number, HourPt>();
  for (const it of raw) {
    if (typeof it !== "object" || it === null) continue;
    const o = it as Record<string, unknown>;
    const h = o.h;
    const mn = o.min;
    const mx = o.max;
    if (typeof h !== "number" || !Number.isInteger(h) || h < 0 || h > 23) continue;
    if (typeof mn !== "number" || typeof mx !== "number") continue;
    if (!Number.isFinite(mn) || !Number.isFinite(mx) || mn <= 0 || mx < mn) continue;
    const era = parseEra(o.era);
    const pt: HourPt = { h, min: mn, max: mx, era };
    const prev = byHour.get(h);
    if (!prev || era <= (prev.era ?? 0)) byHour.set(h, pt);
  }
  return [...byHour.values()].sort((a, b) => a.h - b.h);
}

/** Validate the optional `hourly` block of grab-fare.json; null = no chart. */
export function parseHourly(raw: unknown): GrabHourly | null {
  if (typeof raw !== "object" || raw === null) return null;
  const r = raw as Record<string, unknown>;
  const today = parsePts(r.today);
  const yAll = parsePts(r.yesterday);
  if (today.length === 0 && yAll.length < MIN_YESTERDAY_POINTS) return null;
  const yesterday = yAll.length >= MIN_YESTERDAY_POINTS ? yAll : null;
  return {
    todayDate: typeof r.today_date === "string" ? r.today_date : "",
    yesterdayDate: typeof r.yesterday_date === "string" ? r.yesterday_date : "",
    today,
    yesterday,
    yesterdayPending: yesterday === null && yAll.length > 0,
  };
}

interface ChartText {
  title: string;
  today: string;
  yesterday: string;
  band: string;
  pending: string;
  aria: string;
  newPickup: string;
  prev: string;
  ariaBreak: string;
}

const CHART_TEXT: Record<Lang, ChartText> = {
  tl: {
    title: "Pamasahe kada oras (oras sa Maynila)",
    today: "Ngayon",
    yesterday: "Kahapon",
    band: "saklaw (min–max)",
    pending: "Lalabas ang linya ng kahapon kapag may sapat nang oras na naitala.",
    aria: "Linyang tsart ng pamasahe kada oras, ngayon kumpara kahapon",
    newPickup: "Bagong sundo",
    prev: "dating sundo",
    ariaBreak: "May marka kung saan nagbago ang sundo.",
  },
  en: {
    title: "Fare by hour of day (Manila time)",
    today: "Today",
    yesterday: "Yesterday",
    band: "range (min–max)",
    pending: "Yesterday's line appears once enough hours are recorded.",
    aria: "Line chart of the fare by hour of day, today versus yesterday",
    newPickup: "New pickup",
    prev: "earlier pickup",
    ariaBreak: "A marker shows where the pickup point changed.",
  },
  ja: {
    title: "時間帯別の料金（マニラ時間）",
    today: "今日",
    yesterday: "昨日",
    band: "幅（最小〜最大）",
    pending: "昨日の線は、記録が十分に溜まると表示されます。",
    aria: "時間帯別の料金の折れ線グラフ（今日と昨日の比較）",
    newPickup: "新しい乗車地点",
    prev: "以前の乗車地点",
    ariaBreak: "乗車地点が変わった位置に印があります。",
  },
};

export function chartText(lang: Lang): ChartText {
  return CHART_TEXT[lang];
}

const W = 360;
const H = 190;
const ML = 40;
const MR = 12;
const MT = 14;
const MB = 26;

function mid(p: HourPt): number {
  return (p.min + p.max) / 2;
}

function niceStep(span: number): number {
  if (span <= 60) return 10;
  if (span <= 150) return 25;
  if (span <= 300) return 50;
  return 100;
}

function r1(n: number): string {
  return (Math.round(n * 10) / 10).toString();
}

/** Split consecutive hours into runs; a missing hour breaks the line. */
export function splitRuns(pts: HourPt[]): HourPt[][] {
  const runs: HourPt[][] = [];
  let cur: HourPt[] = [];
  for (const p of pts) {
    if (
      cur.length > 0 &&
      (p.h !== cur[cur.length - 1].h + 1 ||
        (p.era ?? 0) !== (cur[cur.length - 1].era ?? 0))
    ) {
      runs.push(cur);
      cur = [];
    }
    cur.push(p);
  }
  if (cur.length > 0) runs.push(cur);
  return runs;
}

function peso(n: number): string {
  return "₱" + Math.round(n).toLocaleString("en-US");
}

/** Pure SVG string for the hour-of-day chart. Empty string = nothing to draw. */
export function buildGrabChartSvg(data: GrabHourly, lang: Lang): string {
  if (data.today.length === 0 && !data.yesterday) return "";
  const tx = CHART_TEXT[lang];
  const all: HourPt[] = [...data.today, ...(data.yesterday ?? [])];
  let lo = Math.min(...all.map((p) => p.min));
  let hi = Math.max(...all.map((p) => p.max));
  const step = niceStep(hi - lo);
  lo = Math.floor((lo - step * 0.2) / step) * step;
  hi = Math.ceil((hi + step * 0.2) / step) * step;
  if (hi - lo < step * 2) hi = lo + step * 2;

  const x = (h: number) => ML + (h / 23) * (W - ML - MR);
  const y = (v: number) => MT + (1 - (v - lo) / (hi - lo)) * (H - MT - MB);

  // era boundaries inside today
  const boundaries: Array<{ h1: number; h2: number }> = [];
  for (let i = 1; i < data.today.length; i++) {
    if ((data.today[i].era ?? 0) !== (data.today[i - 1].era ?? 0)) {
      boundaries.push({ h1: data.today[i - 1].h, h2: data.today[i].h });
    }
  }
  const hasBoundary = boundaries.length > 0;

  const parts: string[] = [];
  parts.push(
    `<svg class="grab-chart-svg" viewBox="0 0 ${W} ${H}" role="img" aria-label="${esc(hasBoundary ? tx.aria + " " + tx.ariaBreak : tx.aria)}" preserveAspectRatio="xMidYMid meet">`,
  );
  // y grid + labels
  for (let v = lo; v <= hi + 0.001; v += step) {
    const yy = r1(y(v));
    parts.push(`<line class="gc-grid" x1="${ML}" x2="${W - MR}" y1="${yy}" y2="${yy}"/>`);
    parts.push(`<text class="gc-tick" x="${ML - 5}" y="${r1(y(v) + 3.5)}" text-anchor="end">${peso(v)}</text>`);
  }
  // x ticks every 3 hours
  for (let h = 0; h <= 21; h += 3) {
    const xx = r1(x(h));
    parts.push(`<line class="gc-xtick" x1="${xx}" x2="${xx}" y1="${H - MB}" y2="${H - MB + 4}"/>`);
    parts.push(
      `<text class="gc-tick" x="${xx}" y="${H - MB + 16}" text-anchor="middle">${String(h).padStart(2, "0")}</text>`,
    );
  }
  // yesterday: faint, thin, midpoint only
  if (data.yesterday) {
    for (const run of splitRuns(data.yesterday)) {
      const era = run[0].era ?? 0;
      const clsLine = era > 0 ? "gc-y-line gc-prev" : "gc-y-line";
      const clsDot = era > 0 ? "gc-y-dot gc-prev" : "gc-y-dot";
      if (run.length === 1) {
        parts.push(`<circle class="${clsDot}" cx="${r1(x(run[0].h))}" cy="${r1(y(mid(run[0])))}" r="2"/>`);
      } else {
        const pts = run.map((p) => `${r1(x(p.h))},${r1(y(mid(p)))}`).join(" ");
        parts.push(`<polyline class="${clsLine}" points="${pts}"/>`);
      }
    }
  }
  // today: min-max band, bold midpoint line, dots
  for (const run of splitRuns(data.today)) {
    const era = run[0].era ?? 0;
    const bandCls = era > 0 ? "gc-band gc-prev" : "gc-band";
    const lineCls = era > 0 ? "gc-t-line gc-prev" : "gc-t-line";
    if (run.length > 1) {
      const up = run.map((p) => `${r1(x(p.h))},${r1(y(p.max))}`);
      const down = [...run].reverse().map((p) => `${r1(x(p.h))},${r1(y(p.min))}`);
      parts.push(`<polygon class="${bandCls}" points="${[...up, ...down].join(" ")}"/>`);
      const pts = run.map((p) => `${r1(x(p.h))},${r1(y(mid(p)))}`).join(" ");
      parts.push(`<polyline class="${lineCls}" points="${pts}"/>`);
    }
  }
  for (const p of data.today) {
    const cls = (p.era ?? 0) > 0 ? "gc-t-dot gc-prev" : "gc-t-dot";
    const titleSuffix = (p.era ?? 0) > 0 ? ` · ${tx.prev}` : "";
    parts.push(
      `<circle class="${cls}" cx="${r1(x(p.h))}" cy="${r1(y(mid(p)))}" r="3"><title>${String(p.h).padStart(2, "0")}:00 ${peso(p.min)}–${peso(p.max)}${titleSuffix}</title></circle>`,
    );
  }
  // era break markers
  for (const b of boundaries) {
    const mx = (x(b.h1) + x(b.h2)) / 2;
    const anchor = mx > W / 2 ? "end" : "start";
    parts.push(
      `<line class="gc-break" x1="${r1(mx)}" x2="${r1(mx)}" y1="${MT}" y2="${H - MB}"/>`,
    );
    parts.push(
      `<text class="gc-break-label" x="${r1(mx)}" y="${MT + 9}" text-anchor="${anchor}">${esc(tx.newPickup)}</text>`,
    );
  }
  // latest reading label
  const last = data.today[data.today.length - 1];
  if (last) {
    const lx = x(last.h);
    const anchor = lx > W - 60 ? "end" : "middle";
    const ly = y(last.max) - 7;
    parts.push(
      `<text class="gc-last" x="${r1(lx)}" y="${r1(Math.max(ly, 10))}" text-anchor="${anchor}">${peso(mid(last))}</text>`,
    );
  }
  parts.push("</svg>");
  return parts.join("");
}

/** Legend + optional pending hint as plain HTML (escaped). */
export function buildGrabChartLegend(data: GrabHourly, lang: Lang): string {
  const tx = CHART_TEXT[lang];
  const items: string[] = [];
  items.push(`<span class="gc-key"><i class="gc-sw gc-sw-t"></i>${esc(tx.today)}</span>`);
  if (data.yesterday) {
    items.push(`<span class="gc-key"><i class="gc-sw gc-sw-y"></i>${esc(tx.yesterday)}</span>`);
  }
  items.push(`<span class="gc-key"><i class="gc-sw gc-sw-b"></i>${esc(tx.band)}</span>`);
  // boundary detection
  let hasBoundary = false;
  for (let i = 1; i < data.today.length; i++) {
    if ((data.today[i].era ?? 0) !== (data.today[i - 1].era ?? 0)) {
      hasBoundary = true;
      break;
    }
  }
  if (hasBoundary) {
    items.push(`<span class="gc-key"><i class="gc-sw gc-sw-k"></i>${esc(tx.newPickup)}</span>`);
  }
  let html = `<div class="gc-legend">${items.join("")}</div>`;
  if (data.yesterdayPending) {
    html += `<p class="gc-pending">${esc(tx.pending)}</p>`;
  }
  return html;
}

function esc(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}
