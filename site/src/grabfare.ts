import type { Lang } from "./i18n";
import { parseHourly, type GrabHourly } from "./grabchart";

export interface GrabFareText {
  title: string;
  route: string;
  headline: string;
  asof: string;
  traffic: string;
  trend: string;
  yesterday?: string;
  note: string;
}

export type GrabFareTrendDir = "up" | "down" | "flat" | "none";

export interface GrabFareView {
  text: Record<Lang, GrabFareText>;
  trendDir: GrabFareTrendDir;
  fallback: boolean;
  validUntilMs: number;
  hourly: GrabHourly | null;
}

function formatPeso(x: number): string {
  const n = Math.round(x);
  const neg = n < 0;
  const abs = Math.abs(n);
  const s = String(abs);
  let out = "";
  for (let i = 0; i < s.length; i++) {
    if (i > 0 && (s.length - i) % 3 === 0) out += ",";
    out += s[i];
  }
  return "₱" + (neg ? "-" : "") + out;
}

function buildRoute(from: string, to: string, via: string): string {
  if (via.trim() !== "") {
    return `${from} → ${to} (${via})`;
  }
  if (from.trim() === "" && to.trim() === "") return "";
  return `${from} → ${to}`;
}

function asStr(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function buildYesterday(lang: Lang, y: Record<string, unknown>): string {
  const dirRaw = typeof y.direction === "string" ? y.direction : "flat";
  const dir =
    dirRaw === "up" || dirRaw === "down" || dirRaw === "flat" || dirRaw === "none"
      ? dirRaw
      : "flat";
  const arrow = dir === "up" ? "▲" : dir === "down" ? "▼" : "＝";
  const delta = typeof y.delta_pesos === "number" ? y.delta_pesos : null;
  const pct = typeof y.delta_pct === "number" ? y.delta_pct : null;
  const hhmm = typeof y.previous_hhmm === "string" ? y.previous_hhmm : "";
  const amt = delta !== null ? formatPeso(Math.abs(delta)) : "";

  if (lang === "tl") {
    let pctStr = "";
    if (pct !== null) {
      if (dir === "up") pctStr = "(+" + Math.round(pct) + "%)";
      else if (dir === "down") pctStr = "(-" + Math.round(Math.abs(pct)) + "%)";
    }
    if (dir === "flat") {
      const prev = hhmm ? " kumpara kahapon " + hhmm : "";
      return "＝ Walang pagbabago" + prev;
    }
    const prev = hhmm ? " kumpara kahapon " + hhmm : " kumpara kahapon";
    return (arrow + " " + amt + " " + pctStr + prev).trim();
  }
  if (lang === "ja") {
    let pctStr = "";
    if (pct !== null) {
      if (dir === "up") pctStr = "（+" + Math.round(pct) + "%）";
      else if (dir === "down") pctStr = "（-" + Math.round(Math.abs(pct)) + "%）";
    }
    if (dir === "flat") {
      const prev = hhmm
        ? " 昨日" + hhmm + "比で変化なし"
        : " 昨日比で変化なし";
      return "＝" + prev;
    }
    const prev = hhmm ? " 昨日" + hhmm + "比" : " 昨日比";
    return (arrow + " " + amt + pctStr + prev).trim();
  }
  // en
  let pctStr = "";
  if (pct !== null) {
    if (dir === "up") pctStr = "(+" + Math.round(pct) + "%)";
    else if (dir === "down") pctStr = "(-" + Math.round(Math.abs(pct)) + "%)";
  }
  if (dir === "flat") {
    const prev = hhmm ? " vs yesterday " + hhmm : "";
    return "＝ No change" + prev;
  }
  const prev = hhmm ? " vs yesterday " + hhmm : " vs yesterday";
  return (arrow + " " + amt + " " + pctStr + prev).trim();
}

export function evalGrabFare(raw: unknown, nowMs: number): GrabFareView | null {
  if (typeof raw !== "object" || raw === null) return null;
  const r = raw as Record<string, unknown>;
  if (r.visible !== true) return null;
  if (r.state !== "fresh" && r.state !== "fallback") return null;

  const validUntil = r.valid_until;
  if (typeof validUntil !== "string") return null;
  const validUntilMs = Date.parse(validUntil);
  if (Number.isNaN(validUntilMs)) return null;
  if (validUntilMs < nowMs) return null;

  const asOf = r.as_of;
  if (typeof asOf !== "string") return null;
  const asOfMs = Date.parse(asOf);
  if (Number.isNaN(asOfMs)) return null;
  if (asOfMs > nowMs + 10 * 60 * 1000) return null;

  const text = r.text;
  if (typeof text !== "object" || text === null) return null;
  const t = text as Record<string, unknown>;
  const langs: Lang[] = ["tl", "en", "ja"];
  for (const l of langs) {
    const o = t[l];
    if (typeof o !== "object" || o === null) return null;
    const lo = o as Record<string, unknown>;
    for (const k of ["title", "headline", "asof", "trend", "note"]) {
      if (typeof lo[k] !== "string" || (lo[k] as string).length === 0) return null;
    }
  }

  let from = "";
  let to = "";
  let via = "";
  const routeObj = r.route;
  if (typeof routeObj === "object" && routeObj !== null) {
    const ro = routeObj as Record<string, unknown>;
    from = asStr(ro.from);
    to = asStr(ro.to);
    via = asStr(ro.via);
  }
  const route = buildRoute(from, to, via);

  const trendObj = r.trend;
  let trendDir: GrabFareTrendDir = "none";
  if (typeof trendObj === "object" && trendObj !== null) {
    const d = (trendObj as Record<string, unknown>).direction;
    if (d === "up" || d === "down" || d === "flat" || d === "none") trendDir = d;
  }

  const fallback = r.fallback === true;

  const vsYesterday = r.vs_yesterday;
  const hasYesterday =
    typeof vsYesterday === "object" && vsYesterday !== null;

  const viewText = {} as Record<Lang, GrabFareText>;
  for (const l of langs) {
    const lo = t[l] as Record<string, unknown>;
    const traffic = typeof lo.traffic === "string" ? (lo.traffic as string) : "";
    const entry: GrabFareText = {
      title: lo.title as string,
      route,
      headline: lo.headline as string,
      asof: lo.asof as string,
      traffic,
      trend: lo.trend as string,
      note: lo.note as string,
    };
    if (hasYesterday) {
      entry.yesterday = buildYesterday(l, vsYesterday as Record<string, unknown>);
    }
    viewText[l] = entry;
  }

  return {
    text: viewText,
    trendDir,
    fallback,
    validUntilMs,
    hourly: parseHourly(r.hourly),
  };
}
