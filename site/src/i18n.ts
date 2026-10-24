/** UI strings: Taglish (default) and English. No external locale files. */

export type Lang = "tl" | "en";

export const LANG_KEY = "pb-lang";

const CATEGORY_TL: Record<string, string> = {
  rice: "Bigas",
  fish: "Isda",
  egg: "Itlog",
  vegetables: "Gulay",
  spices: "Pampalasa",
  sugar: "Asukal",
  pork: "Baboy",
  poultry: "Manok",
  other: "Iba pa",
};

const CATEGORY_EN: Record<string, string> = {
  rice: "Rice",
  fish: "Fish",
  egg: "Eggs",
  vegetables: "Vegetables",
  spices: "Spices",
  sugar: "Sugar",
  pork: "Pork",
  poultry: "Poultry",
  other: "Other",
};

export interface Strings {
  tagline: string;
  languageLabel: string;
  langTaglish: string;
  langEnglish: string;
  commodityLabel: string;
  marketsLabel: string;
  rangeLabel: string;
  rangeDays: string;
  rangeAll: string;
  fxLabel: string;
  chartTitle: string;
  chartHint: string;
  weekTitle: string;
  weekUnitKg: string;
  weekUnitPc: string;
  thMarket: string;
  thLatest: string;
  thChange: string;
  cheapestBadge: string;
  noBaseline: string;
  noPrice: string;
  noDataForMarket: (market: string) => string;
  loading: string;
  loadError: string;
  retry: string;
  corrTitle: string;
  corrMarketLabel: string;
  corrDesc: string;
  corrLag: string;
  corrR: string;
  corrN: string;
  corrInterp: string;
  corrWeak: string;
  corrModerate: string;
  corrStrong: string;
  corrPositive: string;
  corrNegative: string;
  corrSparse: string;
  corrFlat: string;
  corrCaution: string;
  corrNoFx: string;
  chipsMore: (n: number) => string;
  chipsLess: string;
  rangeNote: string;
  footerSource: string;
  footerFx: string;
  footerStall: string;
  footerUnofficial: string;
  footerUpdated: string;
  footerCode: string;
}

const tl: Strings = {
  tagline: "Araw-araw na presyo sa palengke, mula sa DA Bantay Presyo.",
  languageLabel: "Wika:",
  langTaglish: "Taglish",
  langEnglish: "English",
  commodityLabel: "Paninda:",
  marketsLabel: "Mga palengke:",
  rangeLabel: "Saklaw:",
  rangeDays: "araw",
  rangeAll: "Lahat",
  fxLabel: "Ipakita ang USD/PHP",
  chartTitle: "Presyo sa paglipas ng panahon",
  chartHint: "I-hover o i-tap ang chart para makita ang presyo kada araw.",
  weekTitle: "Ngayong linggo",
  weekUnitKg: "Presyo kada kilo (₱/kg).",
  weekUnitPc: "Presyo kada piraso (₱/piraso).",
  thMarket: "Palengke",
  thLatest: "Pinakabagong presyo",
  thChange: "Pagbabago sa linggo",
  cheapestBadge: "pinakamura",
  noBaseline: "walang nakaraang linggo",
  noPrice: "wala",
  noDataForMarket: (m) => `Walang data para sa ${m} sa panahong ito.`,
  loading: "Naglo-load…",
  loadError: "Nabigong i-load ang data. Tingnan ang koneksyon at subukang muli.",
  retry: "Subukan ulit",
  corrTitle: "USD/PHP at presyo",
  corrMarketLabel: "Palengke:",
  corrDesc:
    "Pearson r sa pagitan ng presyo at USD/PHP, sa iba't ibang lag (presyo sa araw d vs FX noong d − lag).",
  corrLag: "Lag (araw)",
  corrR: "r",
  corrN: "n",
  corrInterp: "Kahulugan",
  corrWeak: "mahina",
  corrModerate: "katamtaman",
  corrStrong: "malakas",
  corrPositive: "positibong kaugnayan",
  corrNegative: "negatibong kaugnayan",
  corrSparse: "kulang ang data",
  corrFlat: "walang galaw ang presyo sa panahong ito",
  corrCaution:
    "Paalala: ang correlation ay hindi causation — sabay man gumalaw ang dalawang series, hindi ibig sabihin nito na FX ang dahilan ng galaw ng presyo. Maikli at maingay ang ilang linggong window, kaya huwag masyadong magtiwala sa isang numero lang.",
  corrNoFx: "Walang FX data para sa panahong ito, kaya walang maikukumpara.",
  chipsMore: (n) => `Iba pang palengke (+${n})`,
  chipsLess: "Itago",
  rangeNote:
    "Paalala: ang ilang lumang data (bago mag-2026-08-18) ay nailathala bilang price range; midpoint ang ipinapakita sa chart.",
  footerSource: "Source: Department of Agriculture – Bantay Presyo (da.gov.ph)",
  footerFx: "USD/PHP: European Central Bank reference rates via Frankfurter",
  footerStall:
    "Paalala: iba-iba ang presyo kada pwesto/stall; gabay lang ang mga presyong ito.",
  footerUnofficial:
    "Unofficial, non-profit community project; hindi affiliated sa DA.",
  footerUpdated: "Huling update ng data:",
  footerCode: "Source code (MIT)",
};

const en: Strings = {
  tagline: "Daily market prices, from DA Bantay Presyo.",
  languageLabel: "Language:",
  langTaglish: "Taglish",
  langEnglish: "English",
  commodityLabel: "Commodity:",
  marketsLabel: "Markets:",
  rangeLabel: "Range:",
  rangeDays: "days",
  rangeAll: "All",
  fxLabel: "Show USD/PHP",
  chartTitle: "Price over time",
  chartHint: "Hover or tap the chart to see daily prices.",
  weekTitle: "This week",
  weekUnitKg: "Prices per kilogram (₱/kg).",
  weekUnitPc: "Prices per piece (₱/piece).",
  thMarket: "Market",
  thLatest: "Latest price",
  thChange: "Weekly change",
  cheapestBadge: "cheapest",
  noBaseline: "no previous week",
  noPrice: "n/a",
  noDataForMarket: (m) => `No data for ${m} in this period.`,
  loading: "Loading…",
  loadError: "Failed to load data. Check your connection and try again.",
  retry: "Retry",
  corrTitle: "USD/PHP and prices",
  corrMarketLabel: "Market:",
  corrDesc:
    "Pearson r between price and USD/PHP at different lags (price on day d vs FX as of d − lag).",
  corrLag: "Lag (days)",
  corrR: "r",
  corrN: "n",
  corrInterp: "Interpretation",
  corrWeak: "weak",
  corrModerate: "moderate",
  corrStrong: "strong",
  corrPositive: "positive association",
  corrNegative: "negative association",
  corrSparse: "not enough data",
  corrFlat: "price did not change in this window",
  corrCaution:
    "Caution: correlation is not causation — two series moving together does not mean FX moves caused the price moves. Short windows are noisy, so do not over-read a single number.",
  corrNoFx: "No FX data for this period, so there is nothing to compare.",
  chipsMore: (n) => `More markets (+${n})`,
  chipsLess: "Show less",
  rangeNote:
    "Note: some older data (before 2026-08-18) was published as price ranges; the chart shows the midpoint.",
  footerSource: "Source: Department of Agriculture – Bantay Presyo (da.gov.ph)",
  footerFx: "USD/PHP: European Central Bank reference rates via Frankfurter",
  footerStall: "Note: prices vary by stall/pwesto and are a guide only.",
  footerUnofficial: "Unofficial, non-profit community project; not affiliated with DA.",
  footerUpdated: "Data last updated:",
  footerCode: "Source code (MIT)",
};

export const STRINGS: Record<Lang, Strings> = { tl, en };

export const CATEGORY_LABEL: Record<Lang, Record<string, string>> = {
  tl: CATEGORY_TL,
  en: CATEGORY_EN,
};

export function categoryLabel(lang: Lang, category: string | null): string {
  const table = CATEGORY_LABEL[lang];
  return (category && table[category]) || table.other;
}

export function loadLang(): Lang {
  try {
    const saved = localStorage.getItem(LANG_KEY);
    if (saved === "en" || saved === "tl") {
      return saved;
    }
  } catch {
    // localStorage unavailable (private mode): fall through to default.
  }
  return "tl";
}

export function saveLang(lang: Lang): void {
  try {
    localStorage.setItem(LANG_KEY, lang);
  } catch {
    // Ignore: persistence is best-effort only.
  }
}
