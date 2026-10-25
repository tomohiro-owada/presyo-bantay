import { describe, expect, it } from "vitest";
import { resolveDeals } from "./dealsSource";
import type { DealsFile } from "./deals";
import type { DaysFile } from "./days";

const deals: DealsFile = {
  as_of: "2026-10-23",
  generated_at: "2026-10-24T00:00:00+08:00",
  text_source: "template",
  texts: { tl: "deals-tl", en: "deals-en", ja: "deals-ja" },
  uses_nearby_basis: false,
};

const days: DaysFile = {
  latest: "2026-10-24",
  days: [
    {
      date: "2026-10-24",
      path: "d/2026-10-24/",
      label: { tl: "Okt 24, 2026", en: "Oct 24, 2026", ja: "2026年10月24日" },
      regions_with_data: 5,
      south: {
        text: { tl: "south-tl", en: "south-en", ja: "south-ja" },
        image: "d/2026-10-24/south.png",
      },
    },
  ],
};

describe("resolveDeals", () => {
  it("prefers days.json latest South text over deals.json", () => {
    expect(resolveDeals(deals, false, days, "tl")).toEqual({
      text: "south-tl",
      date: "2026-10-24",
      fromDays: true,
    });
    expect(resolveDeals(deals, false, days, "en")?.text).toBe("south-en");
    expect(resolveDeals(deals, false, days, "ja")?.text).toBe("south-ja");
  });

  it("falls back to deals.json when days.json is missing or empty", () => {
    expect(resolveDeals(deals, false, null, "tl")).toEqual({
      text: "deals-tl",
      date: "2026-10-23",
      fromDays: false,
    });
    expect(
      resolveDeals(deals, false, { latest: "", days: [] }, "tl"),
    ).toEqual({
      text: "deals-tl",
      date: "2026-10-23",
      fromDays: false,
    });
    const blankDays: DaysFile = {
      latest: "2026-10-24",
      days: [
        {
          ...days.days[0],
          south: { text: { tl: "  ", en: "", ja: "" }, image: "" },
        },
      ],
    };
    const r = resolveDeals(deals, false, blankDays, "tl");
    expect(r.text).toBe("deals-tl");
    expect(r.fromDays).toBe(false);
  });

  it("returns null when both sources are missing", () => {
    expect(resolveDeals(null, true, null, "tl").text).toBeNull();
  });
});
