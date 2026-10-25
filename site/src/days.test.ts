import { describe, expect, it } from "vitest";
import {
  dayLabelForLang,
  dayUrl,
  latestDay,
  recentDays,
  southTextForLang,
  type DaysFile,
} from "./days";

const sample: DaysFile = {
  latest: "2026-10-24",
  days: [
    {
      date: "2026-10-24",
      path: "d/2026-10-24/",
      label: { tl: "Okt 24, 2026", en: "Oct 24, 2026", ja: "2026年10月24日" },
      regions_with_data: 5,
      south: {
        text: { tl: "tl24", en: "en24", ja: "ja24" },
        image: "d/2026-10-24/south.png",
      },
    },
    {
      date: "2026-10-23",
      path: "d/2026-10-23/",
      label: { tl: "Okt 23, 2026", en: "Oct 23, 2026", ja: "2026年10月23日" },
      regions_with_data: 4,
      south: {
        text: { tl: "tl23", en: "en23", ja: "ja23" },
        image: "d/2026-10-23/south.png",
      },
    },
  ],
};

describe("dayUrl", () => {
  it("joins BASE_URL-style bases with site-relative paths", () => {
    expect(dayUrl("/presyo-bantay/", "d/2026-10-23/")).toBe(
      "/presyo-bantay/d/2026-10-23/",
    );
    expect(dayUrl("/", "d/")).toBe("/d/");
    expect(dayUrl("/presyo-bantay", "d/2026-10-23/")).toBe(
      "/presyo-bantay/d/2026-10-23/",
    );
  });
});

describe("latestDay", () => {
  it("returns the first (newest) entry", () => {
    expect(latestDay(sample)?.date).toBe("2026-10-24");
  });

  it("returns null for missing or empty manifests", () => {
    expect(latestDay(null)).toBeNull();
    expect(latestDay({ latest: "", days: [] })).toBeNull();
  });
});

describe("recentDays", () => {
  it("returns up to n entries newest-first", () => {
    expect(recentDays(sample, 7).map((d) => d.date)).toEqual([
      "2026-10-24",
      "2026-10-23",
    ]);
    expect(recentDays(sample, 1).map((d) => d.date)).toEqual(["2026-10-24"]);
    expect(recentDays(null, 7)).toEqual([]);
  });
});

describe("dayLabelForLang / southTextForLang", () => {
  it("picks the text for each language", () => {
    const e = sample.days[0];
    expect(dayLabelForLang(e, "tl")).toBe("Okt 24, 2026");
    expect(dayLabelForLang(e, "en")).toBe("Oct 24, 2026");
    expect(dayLabelForLang(e, "ja")).toBe("2026年10月24日");
    expect(southTextForLang(e, "tl")).toBe("tl24");
    expect(southTextForLang(e, "en")).toBe("en24");
    expect(southTextForLang(e, "ja")).toBe("ja24");
  });
});
