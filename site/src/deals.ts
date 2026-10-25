import type { Lang } from "./i18n";

export interface DealsFile {
  as_of: string;
  generated_at: string;
  text_source: "llm" | "template";
  model?: string;
  texts: { tl: string; en: string; ja: string };
  uses_nearby_basis: boolean;
}

/** Split a deals text into non-empty trimmed lines (for the deals card). */
export function dealsTextToLines(text: string): string[] {
  return text
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l.length > 0);
}

/** A line is a sub-heading (e.g. "Pricier than usual:" / "いつもより高め：")
 *  when it ends with an ASCII or full-width colon. */
export function isDealsSubheading(line: string): boolean {
  const t = line.trim();
  return t.endsWith(":") || t.endsWith("：");
}

export interface DealsTextSegment {
  text: string;
  nowrap: boolean;
}

// Peso amounts incl. optional sign and wrapping parens, e.g. "₱130",
// "(-₱90)", "（-₱90）". Unit suffixes ("/kg") are left outside the span.
const PESO_RE = /[\(（]?[-‐‑‒–—−]?₱\d[\d,]*(?:\.\d+)?[\)）]?/g;

/** Split a deals line so peso amounts can be wrapped in nowrap spans. */
export function splitPesoSegments(text: string): DealsTextSegment[] {
  const out: DealsTextSegment[] = [];
  let last = 0;
  PESO_RE.lastIndex = 0;
  for (;;) {
    const m = PESO_RE.exec(text);
    if (!m || m[0].length === 0) break;
    const idx = m.index;
    if (idx > last) out.push({ text: text.slice(last, idx), nowrap: false });
    out.push({ text: m[0], nowrap: true });
    last = idx + m[0].length;
  }
  if (last < text.length) out.push({ text: text.slice(last), nowrap: false });
  if (out.length === 0) out.push({ text, nowrap: false });
  return out;
}

/** Pick the current-language text from a deals file. */
export function dealsTextForLang(deals: DealsFile, lang: Lang): string {
  if (lang === "ja") return deals.texts.ja;
  if (lang === "en") return deals.texts.en;
  return deals.texts.tl;
}
