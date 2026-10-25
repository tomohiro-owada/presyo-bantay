import type { Lang } from "./i18n";

/** One week in data/weeks.json (see presyo weekpages). */
export interface WeeksEntry {
  id: string;
  /** Site-relative path, e.g. "w/2026-W43/". */
  path: string;
  label: { tl: string; en: string; ja: string };
  summary: { tl: string; en: string; ja: string };
}

/** The data/weeks.json manifest (newest first). */
export interface WeeksFile {
  latest: string;
  weeks: WeeksEntry[];
}

/** Join a BASE_URL-style base with a site-relative path. */
export function weekUrl(base: string, path: string): string {
  const b = base.endsWith("/") ? base : `${base}/`;
  return b + path.replace(/^\//, "");
}

/** The latest week entry, or null when weeks.json is missing/empty. */
export function latestWeek(weeks: WeeksFile | null): WeeksEntry | null {
  if (!weeks || !Array.isArray(weeks.weeks) || weeks.weeks.length === 0) {
    return null;
  }
  return weeks.weeks[0];
}

/** Up to n most recent week entries (newest first). */
export function recentWeeks(
  weeks: WeeksFile | null,
  n: number,
): WeeksEntry[] {
  if (!weeks || !Array.isArray(weeks.weeks)) return [];
  return weeks.weeks.slice(0, n);
}

/** Current-language range label for a week entry. */
export function weekLabelForLang(entry: WeeksEntry, lang: Lang): string {
  if (lang === "ja") return entry.label.ja;
  if (lang === "en") return entry.label.en;
  return entry.label.tl;
}

/** Current-language top-drop summary for a week entry. */
export function weekSummaryForLang(entry: WeeksEntry, lang: Lang): string {
  if (lang === "ja") return entry.summary.ja;
  if (lang === "en") return entry.summary.en;
  return entry.summary.tl;
}
