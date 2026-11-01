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
