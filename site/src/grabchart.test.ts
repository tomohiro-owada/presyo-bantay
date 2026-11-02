import { describe, expect, it } from "vitest";
import {
  buildGrabChartLegend,
  buildGrabChartSvg,
  parseHourly,
  splitRuns,
  type GrabHourly,
} from "./grabchart";
import { evalGrabFare } from "./grabfare";

function pts(hours: number[], base = 270) {
  return hours.map((h) => ({ h, min: base + h, max: base + h + 50 }));
}

function raw(today: number[], yesterday: number[]) {
  return {
    today_date: "2026-11-02",
    yesterday_date: "2026-11-01",
    today: pts(today),
    yesterday: pts(yesterday, 300),
  };
}

describe("parseHourly", () => {
  it("keeps yesterday with >= 6 points", () => {
    const h = parseHourly(raw([0, 1, 2], [0, 1, 2, 3, 4, 5]))!;
    expect(h.yesterday).not.toBeNull();
    expect(h.yesterdayPending).toBe(false);
  });
  it("hides yesterday with < 6 points and flags pending", () => {
    const h = parseHourly(raw([0, 1, 2], [21, 22, 23]))!;
    expect(h.yesterday).toBeNull();
    expect(h.yesterdayPending).toBe(true);
  });
  it("null when nothing to draw", () => {
    expect(parseHourly(raw([], [21, 22]))).toBeNull();
    expect(parseHourly(null)).toBeNull();
    expect(parseHourly("x")).toBeNull();
    expect(parseHourly({})).toBeNull();
  });
  it("only yesterday full is drawable", () => {
    const h = parseHourly(raw([], [0, 1, 2, 3, 4, 5, 6]))!;
    expect(h.today).toHaveLength(0);
    expect(h.yesterday).toHaveLength(7);
  });
  it("drops bad points, dedups hours and sorts", () => {
    const h = parseHourly({
      today: [
        { h: 5, min: 300, max: 350 },
        { h: 2, min: 280, max: 330 },
        { h: 2, min: 281, max: 331 },
        { h: 24, min: 1, max: 2 },
        { h: -1, min: 1, max: 2 },
        { h: 1.5, min: 1, max: 2 },
        { h: 3, min: 400, max: 300 },
        { h: 4, min: "x", max: 3 },
        { h: 6, min: 0, max: 10 },
        null,
      ],
      yesterday: [],
    })!;
    expect(h.today.map((p) => p.h)).toEqual([2, 5]);
    expect(h.today[0].min).toBe(281);
  });
});

describe("splitRuns", () => {
  it("breaks at missing hours", () => {
    const r = splitRuns(pts([0, 1, 2, 4, 5, 8]));
    expect(r.map((x) => x.map((p) => p.h))).toEqual([[0, 1, 2], [4, 5], [8]]);
  });
});

describe("buildGrabChartSvg", () => {
  const full = parseHourly(raw([0, 1, 2, 3, 5], [0, 1, 2, 3, 4, 5, 6, 7]))!;
  it("draws today bold, yesterday faint, and breaks the line at the gap", () => {
    const svg = buildGrabChartSvg(full, "en");
    expect(svg.startsWith("<svg")).toBe(true);
    expect((svg.match(/class="gc-t-line"/g) ?? []).length).toBe(1); // 0-3; 5 alone
    expect((svg.match(/class="gc-t-dot"/g) ?? []).length).toBe(5);
    expect(svg).toContain("gc-y-line");
    expect(svg).not.toContain("NaN");
    expect(svg).not.toContain("undefined");
  });
  it("no yesterday line when hidden", () => {
    const h = parseHourly(raw([0, 1, 2], [22, 23]))!;
    const svg = buildGrabChartSvg(h, "tl");
    expect(svg).not.toContain("gc-y-line");
    expect(svg).not.toContain("gc-y-dot");
  });
  it("single today point renders as a dot", () => {
    const h = parseHourly(raw([0], [0, 1, 2, 3, 4, 5]))!;
    const svg = buildGrabChartSvg(h, "ja");
    expect(svg).toContain("gc-t-dot");
    expect(svg).not.toContain("gc-t-line");
    expect(svg).not.toContain("NaN");
  });
  it("labels are trilingual", () => {
    expect(buildGrabChartLegend(full, "tl")).toContain("Kahapon");
    expect(buildGrabChartLegend(full, "en")).toContain("Yesterday");
    expect(buildGrabChartLegend(full, "ja")).toContain("昨日");
  });
  it("pending hint only when yesterday is hidden but present", () => {
    const h: GrabHourly = parseHourly(raw([0, 1], [22, 23]))!;
    expect(buildGrabChartLegend(h, "en")).toContain("gc-pending");
    expect(buildGrabChartLegend(full, "en")).not.toContain("gc-pending");
  });
  it("flat fares still give a sane axis", () => {
    const h = parseHourly({
      today: [
        { h: 0, min: 300, max: 300 },
        { h: 1, min: 300, max: 300 },
      ],
    })!;
    const svg = buildGrabChartSvg(h, "en");
    expect(svg).not.toContain("NaN");
    expect(svg).not.toContain("Infinity");
  });
});

describe("evalGrabFare carries hourly", () => {
  it("hourly is optional", () => {
    const base = {
      visible: true,
      state: "fresh",
      valid_until: "2026-11-02T10:00:00+08:00",
      as_of: "2026-11-02T08:00:00+08:00",
      text: Object.fromEntries(
        ["tl", "en", "ja"].map((l) => [l, { title: "T", headline: "H", asof: "A", trend: "t", note: "N" }]),
      ),
    };
    const now = Date.parse("2026-11-02T08:05:00+08:00");
    expect(evalGrabFare(base, now)!.hourly).toBeNull();
    const v = evalGrabFare({ ...base, hourly: raw([0, 1], [1, 2, 3, 4, 5, 6]) }, now)!;
    expect(v.hourly!.today).toHaveLength(2);
    expect(v.hourly!.yesterday).toHaveLength(6);
  });
});

describe("parseHourly era", () => {
  it("era default 0 for missing/negative/non-integer/string", () => {
    const h = parseHourly({
      today: [
        { h: 0, min: 10, max: 20 },
        { h: 1, min: 10, max: 20, era: -1 },
        { h: 2, min: 10, max: 20, era: 1.5 },
        { h: 3, min: 10, max: 20, era: "1" },
      ],
      yesterday: [],
    })!;
    for (const p of h.today) expect(p.era).toBe(0);
  });

  it("two points same hour -> lower era wins regardless of order", () => {
    const h1 = parseHourly({
      today: [
        { h: 5, min: 10, max: 20, era: 1 },
        { h: 5, min: 11, max: 21, era: 0 },
      ],
      yesterday: [],
    })!;
    expect(h1.today[0].era).toBe(0);
    expect(h1.today[0].min).toBe(11);
    const h2 = parseHourly({
      today: [
        { h: 5, min: 11, max: 21, era: 0 },
        { h: 5, min: 10, max: 20, era: 1 },
      ],
      yesterday: [],
    })!;
    expect(h2.today[0].era).toBe(0);
    expect(h2.today[0].min).toBe(11);
  });

  it("era kept on parsed points", () => {
    const h = parseHourly({
      today: [{ h: 0, min: 10, max: 20, era: 2 }],
      yesterday: [],
    })!;
    expect(h.today[0].era).toBe(2);
  });
});

describe("splitRuns era", () => {
  it("consecutive hours 11 era1,12/13 era0 -> 2 runs", () => {
    const r = splitRuns([
      { h: 11, min: 1, max: 2, era: 1 },
      { h: 12, min: 1, max: 2, era: 0 },
      { h: 13, min: 1, max: 2, era: 0 },
    ]);
    expect(r.map((x) => x.map((p) => p.h))).toEqual([[11], [12, 13]]);
  });

  it("without era unchanged", () => {
    const r = splitRuns([
      { h: 0, min: 1, max: 2 },
      { h: 1, min: 1, max: 2 },
      { h: 2, min: 1, max: 2 },
    ]);
    expect(r.map((x) => x.map((p) => p.h))).toEqual([[0, 1, 2]]);
  });
});

describe("buildGrabChartSvg era", () => {
  const eraData = {
    todayDate: "",
    yesterdayDate: "",
    today: [
      { h: 10, min: 100, max: 120, era: 1 },
      { h: 11, min: 110, max: 130, era: 1 },
      { h: 12, min: 200, max: 220, era: 0 },
      { h: 13, min: 210, max: 230, era: 0 },
    ],
    yesterday: null,
    yesterdayPending: false,
  } as GrabHourly;

  const noEraData = {
    todayDate: "",
    yesterdayDate: "",
    today: [
      { h: 10, min: 100, max: 120, era: 0 },
      { h: 11, min: 110, max: 130, era: 0 },
      { h: 12, min: 200, max: 220, era: 0 },
      { h: 13, min: 210, max: 230, era: 0 },
    ],
    yesterday: null,
    yesterdayPending: false,
  } as GrabHourly;

  it("one break line and label per language", () => {
    const labels: Record<string, string> = { tl: "Bagong sundo", en: "New pickup", ja: "新しい乗車地点" };
    for (const lang of ["tl", "en", "ja"]) {
      const svg = buildGrabChartSvg(eraData, lang as any);
      expect((svg.match(/class="gc-break"/g) ?? []).length).toBe(1);
      expect((svg.match(/class="gc-break-label"/g) ?? []).length).toBe(1);
      expect(svg).toContain(labels[lang]);
    }
  });

  it("break x is midpoint of x(11) and x(12)", () => {
    const ML = 40, MR = 12, W = 360;
    const x = (h: number) => ML + (h / 23) * (W - ML - MR);
    const mx = (x(11) + x(12)) / 2;
    const expected = (Math.round(mx * 10) / 10).toString();
    const svg = buildGrabChartSvg(eraData, "tl");
    const m = svg.match(/class="gc-break" x1="([^"]+)"/);
    expect(m![1]).toBe(expected);
  });

  it("has gc-prev and single-era polylines", () => {
    const svg = buildGrabChartSvg(eraData, "tl");
    expect(svg).toContain("gc-prev");
    const polys = [...svg.matchAll(/<polyline class="([^"]*)" points="([^"]*)"/g)];
    const ML = 40, MR = 12, W = 360;
    const x = (h: number) => ML + (h / 23) * (W - ML - MR);
    const r1 = (n: number) => (Math.round(n * 10) / 10).toString();
    const x10 = r1(x(10)), x11 = r1(x(11)), x12 = r1(x(12)), x13 = r1(x(13));
    const era1Poly = polys.find((p) => p[1].includes("gc-prev"))!;
    const era0Poly = polys.find((p) => p[1] === "gc-t-line")!;
    expect(era1Poly[2]).toContain(x10);
    expect(era1Poly[2]).toContain(x11);
    expect(era1Poly[2]).not.toContain(x12);
    expect(era1Poly[2]).not.toContain(x13);
    expect(era0Poly[2]).toContain(x12);
    expect(era0Poly[2]).toContain(x13);
    expect(era0Poly[2]).not.toContain(x10);
    expect(era0Poly[2]).not.toContain(x11);
    for (const p of polys) {
      const hasEarly = p[2].includes(x10) || p[2].includes(x11);
      const hasLate = p[2].includes(x12) || p[2].includes(x13);
      expect(!(hasEarly && hasLate)).toBe(true);
    }
  });

  it("aria-label contains ariaBreak sentence", () => {
    const svg = buildGrabChartSvg(eraData, "en");
    const m = svg.match(/aria-label="([^"]*)"/);
    expect(m![1]).toContain("A marker shows where the pickup point changed.");
  });

  it("era-1 dot titles end with dating sundo in tl", () => {
    const svg = buildGrabChartSvg(eraData, "tl");
    const titles = [...svg.matchAll(/<title>([^<]*)<\/title>/g)].map((m) => m[1]);
    const t10 = titles.find((t) => t.startsWith("10:00"))!;
    const t11 = titles.find((t) => t.startsWith("11:00"))!;
    expect(t10.endsWith(" · dating sundo")).toBe(true);
    expect(t11.endsWith(" · dating sundo")).toBe(true);
  });

  it("legend with boundary has gc-sw-k and label", () => {
    const html = buildGrabChartLegend(eraData, "en");
    expect(html).toContain("gc-sw-k");
    expect(html).toContain("New pickup");
  });

  it("legend without era has no gc-sw-k", () => {
    const html = buildGrabChartLegend(noEraData, "en");
    expect(html).not.toContain("gc-sw-k");
  });

  it("no-era input has no break/prev and plain aria", () => {
    const svg = buildGrabChartSvg(noEraData, "en");
    expect(svg).not.toContain("gc-break");
    expect(svg).not.toContain("gc-prev");
    const m = svg.match(/aria-label="([^"]*)"/);
    expect(m![1]).toBe("Line chart of the fare by hour of day, today versus yesterday");
  });

  it("break anchor start when left of middle, end when right", () => {
    const leftData = {
      todayDate: "",
      yesterdayDate: "",
      today: [
        { h: 2, min: 100, max: 120, era: 1 },
        { h: 3, min: 200, max: 220, era: 0 },
      ],
      yesterday: null,
      yesterdayPending: false,
    } as GrabHourly;
    const rightData = {
      todayDate: "",
      yesterdayDate: "",
      today: [
        { h: 20, min: 100, max: 120, era: 1 },
        { h: 21, min: 200, max: 220, era: 0 },
      ],
      yesterday: null,
      yesterdayPending: false,
    } as GrabHourly;
    const leftSvg = buildGrabChartSvg(leftData, "en");
    const rightSvg = buildGrabChartSvg(rightData, "en");
    expect(leftSvg).toMatch(/class="gc-break-label"[^>]*text-anchor="start"/);
    expect(rightSvg).toMatch(/class="gc-break-label"[^>]*text-anchor="end"/);
  });
});
