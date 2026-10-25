import { describe, expect, it } from "vitest";
import {
  latestWeek,
  recentWeeks,
  weekLabelForLang,
  weekSummaryForLang,
  weekUrl,
  type WeeksFile,
} from "./weeks";

const sample: WeeksFile = {
  latest: "2026-W43",
  weeks: [
    {
      id: "2026-W43",
      path: "w/2026-W43/",
      label: { tl: "Okt 19–23, 2026", en: "Oct 19–23, 2026", ja: "10/19〜10/23" },
      summary: { tl: "Sayote −20%", en: "Chayote −20%", ja: "サヨテ −20%" },
    },
    {
      id: "2026-W42",
      path: "w/2026-W42/",
      label: { tl: "Okt 12–17, 2026", en: "Oct 12–17, 2026", ja: "10/12〜10/17" },
      summary: { tl: "", en: "", ja: "" },
    },
  ],
};

describe("weekUrl", () => {
  it("joins BASE_URL-style bases with site-relative paths", () => {
    expect(weekUrl("/presyo-bantay/", "w/2026-W43/")).toBe(
      "/presyo-bantay/w/2026-W43/",
    );
    expect(weekUrl("/", "w/")).toBe("/w/");
    expect(weekUrl("/presyo-bantay", "w/2026-W43/")).toBe(
      "/presyo-bantay/w/2026-W43/",
    );
  });
});

describe("latestWeek", () => {
  it("returns the first (newest) entry", () => {
    expect(latestWeek(sample)?.id).toBe("2026-W43");
  });

  it("returns null for missing or empty manifests", () => {
    expect(latestWeek(null)).toBeNull();
    expect(latestWeek({ latest: "", weeks: [] })).toBeNull();
  });
});

describe("recentWeeks", () => {
  it("returns up to n newest entries", () => {
    expect(recentWeeks(sample, 1).map((w) => w.id)).toEqual(["2026-W43"]);
    expect(recentWeeks(sample, 7).map((w) => w.id)).toEqual([
      "2026-W43",
      "2026-W42",
    ]);
    expect(recentWeeks(null, 7)).toEqual([]);
  });
});

describe("weekLabelForLang / weekSummaryForLang", () => {
  it("picks the current-language strings", () => {
    const w = sample.weeks[0];
    expect(weekLabelForLang(w, "tl")).toBe("Okt 19–23, 2026");
    expect(weekLabelForLang(w, "en")).toBe("Oct 19–23, 2026");
    expect(weekLabelForLang(w, "ja")).toBe("10/19〜10/23");
    expect(weekSummaryForLang(w, "tl")).toBe("Sayote −20%");
    expect(weekSummaryForLang(w, "en")).toBe("Chayote −20%");
    expect(weekSummaryForLang(w, "ja")).toBe("サヨテ −20%");
  });

  it("returns empty summaries as-is (caller falls back)", () => {
    expect(weekSummaryForLang(sample.weeks[1], "tl")).toBe("");
  });
});
