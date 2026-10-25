import type { Lang } from "./i18n";
import { dealsTextForLang, type DealsFile } from "./deals";
import { latestDay, southTextForLang, type DaysFile } from "./days";

export interface ResolvedDeals {
  text: string | null;
  date: string;
  fromDays: boolean;
}

/**
 * Resolve the deals-card content. days.json (latest day's featured South
 * text, same as the Facebook post) wins; deals.json is only used when
 * days.json is missing or its latest text is empty.
 */
export function resolveDeals(
  deals: DealsFile | null,
  dealsMissing: boolean,
  days: DaysFile | null,
  lang: Lang,
): ResolvedDeals {
  const latest = latestDay(days);
  if (latest) {
    const t = southTextForLang(latest, lang);
    if (t && t.trim().length > 0) {
      return { text: t, date: latest.date, fromDays: true };
    }
  }
  if (deals && !dealsMissing) {
    try {
      const t = dealsTextForLang(deals, lang);
      if (t && t.trim().length > 0) {
        return { text: t, date: deals.as_of, fromDays: false };
      }
    } catch {
      // fall through to null
    }
  }
  return { text: null, date: "", fromDays: false };
}
