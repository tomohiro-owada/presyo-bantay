import type { Lang } from "./i18n";

/** One day in data/days.json (see presyo daypages). */
export interface DaysEntry {
  date: string;
  /** Site-relative path, e.g. "d/2026-10-23/". */
  path: string;
  label: { tl: string; en: string; ja: string };
  regions_with_data: number;
  south: {
    text: { tl: string; en: string; ja: string };
    image: string;
    title?: { tl: string; en: string; ja: string };
  };
}

/** The data/days.json manifest (newest first). */
export interface DaysFile {
  latest: string;
  days: DaysEntry[];
}

/** Join a BASE_URL-style base with a site-relative path. */
export function dayUrl(base: string, path: string): string {
  const b = base.endsWith("/") ? base : `${base}/`;
  return b + path.replace(/^\//, "");
}

/** The latest day entry, or null when days.json is missing/empty. */
export function latestDay(days: DaysFile | null): DaysEntry | null {
  if (!days || !Array.isArray(days.days) || days.days.length === 0) {
    return null;
  }
  return days.days[0];
}

/** Up to n most recent day entries (newest first). */
export function recentDays(days: DaysFile | null, n: number): DaysEntry[] {
  if (!days || !Array.isArray(days.days)) return [];
  return days.days.slice(0, n);
}

/** Current-language date label for a day entry. */
export function dayLabelForLang(entry: DaysEntry, lang: Lang): string {
  if (lang === "ja") return entry.label.ja;
  if (lang === "en") return entry.label.en;
  return entry.label.tl;
}

/** Current-language featured (south) text for a day entry. */
export function southTextForLang(entry: DaysEntry, lang: Lang): string {
  if (lang === "ja") return entry.south.text.ja;
  if (lang === "en") return entry.south.text.en;
  return entry.south.text.tl;
}
