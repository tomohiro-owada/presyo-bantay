/** UI strings: Taglish (default), English and Japanese. No external locale files. */

export type Lang = "tl" | "en" | "ja";

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

const CATEGORY_JA: Record<string, string> = {
  rice: "米",
  fish: "魚",
  egg: "卵",
  vegetables: "野菜",
  spices: "香辛料",
  sugar: "砂糖",
  pork: "豚肉",
  poultry: "鶏肉",
  other: "その他",
};

export interface Strings {
  tagline: string;
  languageLabel: string;
  langTaglish: string;
  langEnglish: string;
  langJapanese: string;
  commodityLabel: string;
  marketsLabel: string;
  rangeLabel: string;
  rangeDays: string;
  rangeAll: string;
  fxLabel: string;
  dealsTitle: string;
  dealsFooter: string;
  dealsFooterAI: string;
  dealsDayLink: string;
  daysTitle: string;
  daysLatest: (date: string) => string;
  daysAll: string;
  weeksTitle: string;
  weeksLatest: (date: string) => string;
  weeksAll: string;
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
  footerPrivacy: string;
  footerUpdated: string;
  footerCode: string;
}

const tl: Strings = {
  tagline: "Araw-araw na presyo sa palengke, mula sa DA",
  languageLabel: "Wika:",
  langTaglish: "Taglish",
  langEnglish: "English",
  langJapanese: "日本語",
  commodityLabel: "Paninda:",
  marketsLabel: "Mga palengke:",
  rangeLabel: "Saklaw:",
  rangeDays: "araw",
  rangeAll: "Lahat",
  fxLabel: "Ipakita ang USD/PHP",
  dealsTitle: "Sulit ngayon",
  dealsFooter: "Awtomatikong buod mula sa DA price data",
  dealsFooterAI: "(may tulong ng AI, sinuri ang mga numero)",
  dealsDayLink: "Lahat ng 5 rehiyon →",
  daysTitle: "Araw-araw na ulat",
  daysLatest: (date) => `Tingnan ang ulat ngayong ${date} — 5 rehiyon →`,
  daysAll: "Lahat ng ulat →",
  weeksTitle: "Lingguhang ulat",
  weeksLatest: (date) => `Tingnan ang lingguhang ulat ${date} →`,
  weeksAll: "Lahat ng lingguhang ulat →",
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
  footerPrivacy:
    "Gumagamit ang site na ito ng Google Analytics para sa bilang ng bisita.",
  footerUpdated: "Huling update ng data:",
  footerCode: "Source code (MIT)",
};

const en: Strings = {
  tagline: "Daily market prices from DA Bantay Presyo",
  languageLabel: "Language:",
  langTaglish: "Taglish",
  langEnglish: "English",
  langJapanese: "日本語",
  commodityLabel: "Commodity:",
  marketsLabel: "Markets:",
  rangeLabel: "Range:",
  rangeDays: "days",
  rangeAll: "All",
  fxLabel: "Show USD/PHP",
  dealsTitle: "Today's deals",
  dealsFooter: "Automatic summary from DA price data",
  dealsFooterAI: "(AI-assisted, numbers checked)",
  dealsDayLink: "All 5 areas →",
  daysTitle: "Daily reports",
  daysLatest: (date) => `See the ${date} report — 5 areas →`,
  daysAll: "All reports →",
  weeksTitle: "Weekly reports",
  weeksLatest: (date) => `See the ${date} weekly report →`,
  weeksAll: "All weekly reports →",
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
  footerPrivacy: "This site uses Google Analytics to count visits.",
  footerUpdated: "Data last updated:",
  footerCode: "Source code (MIT)",
};

const ja: Strings = {
  tagline: "DAの市場価格を毎日お届け",
  languageLabel: "言語：",
  langTaglish: "Taglish",
  langEnglish: "English",
  langJapanese: "日本語",
  commodityLabel: "品目：",
  marketsLabel: "市場：",
  rangeLabel: "期間：",
  rangeDays: "日間",
  rangeAll: "全期間",
  fxLabel: "USD/PHPを表示",
  dealsTitle: "今日のお買い得",
  dealsFooter: "DA価格データからの自動要約",
  dealsFooterAI: "（AI補助・数値チェック済み）",
  dealsDayLink: "全5地域を見る →",
  daysTitle: "毎日のレポート",
  daysLatest: (date) => `${date}のレポート（5地域）→`,
  daysAll: "すべてのレポート →",
  weeksTitle: "週間レポート",
  weeksLatest: (date) => `${date}の週間レポート →`,
  weeksAll: "すべての週間レポート →",
  chartTitle: "価格の推移",
  chartHint: "チャートにホバーまたはタップすると毎日の価格が表示されます。",
  weekTitle: "今週",
  weekUnitKg: "1キロあたりの価格（₱/kg）。",
  weekUnitPc: "1個あたりの価格（₱/個）。",
  thMarket: "市場",
  thLatest: "最新価格",
  thChange: "週間変動",
  cheapestBadge: "最安",
  noBaseline: "前週データなし",
  noPrice: "なし",
  noDataForMarket: (m) => `${m}のデータはこの期間にありません。`,
  loading: "読み込み中…",
  loadError: "データの読み込みに失敗しました。接続を確認して再試行してください。",
  retry: "再試行",
  corrTitle: "USD/PHPと価格",
  corrMarketLabel: "市場：",
  corrDesc:
    "価格とUSD/PHPのピアソン相関係数。ラグ別（d日の価格とd−ラグ時点の為替）。",
  corrLag: "ラグ（日）",
  corrR: "r",
  corrN: "n",
  corrInterp: "解釈",
  corrWeak: "弱い",
  corrModerate: "中程度",
  corrStrong: "強い",
  corrPositive: "正の相関",
  corrNegative: "負の相関",
  corrSparse: "データ不足",
  corrFlat: "この期間は価格変動なし",
  corrCaution:
    "注意：相関は因果関係ではありません。二つの系列が一緒に動いても、為替の動きが価格の動きの原因とは限りません。短い期間の数値はばらつきが大きいため、一つの数値を過信しないでください。",
  corrNoFx: "この期間の為替データがないため、比較できません。",
  chipsMore: (n) => `その他の市場（+${n}）`,
  chipsLess: "閉じる",
  rangeNote:
    "注意：一部の古いデータ（2026-08-18以前）は価格帯で公表されていたため、チャートは中間値を表示しています。",
  footerSource: "出典：農業省（DA）バンタイ・プレショ（da.gov.ph）",
  footerFx: "USD/PHP：フランクフルター経由の欧州中央銀行参考レート",
  footerStall: "注意：価格は売り場ごとに異なります。目安としてご利用ください。",
  footerUnofficial: "非公式の非営利コミュニティプロジェクト。DAとは関係ありません。",
  footerPrivacy: "アクセス解析にGoogleアナリティクスを使っています。",
  footerUpdated: "データ最終更新：",
  footerCode: "ソースコード（MIT）",
};

export const STRINGS: Record<Lang, Strings> = { tl, en, ja };

export const CATEGORY_LABEL: Record<Lang, Record<string, string>> = {
  tl: CATEGORY_TL,
  en: CATEGORY_EN,
  ja: CATEGORY_JA,
};

export function categoryLabel(lang: Lang, category: string | null): string {
  const table = CATEGORY_LABEL[lang];
  return (category && table[category]) || table.other;
}

export function loadLang(): Lang {
  try {
    const saved = localStorage.getItem(LANG_KEY);
    if (saved === "en" || saved === "tl" || saved === "ja") {
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
