import { describe, expect, it } from "vitest";
import {
  asofLookup,
  laggedCorrelation,
  pearson,
  shiftDate,
} from "./stats";

describe("pearson", () => {
  it("perfect positive linear relationship gives r = 1", () => {
    // ys = 2 * xs exactly.
    const { r, n } = pearson([1, 2, 3, 4, 5], [2, 4, 6, 8, 10]);
    expect(n).toBe(5);
    expect(r).toBeCloseTo(1.0, 12);
  });

  it("perfect negative linear relationship gives r = -1", () => {
    const { r, n } = pearson([1, 2, 3, 4, 5], [10, 8, 6, 4, 2]);
    expect(n).toBe(5);
    expect(r).toBeCloseTo(-1.0, 12);
  });

  it("partial correlation (hand-computed r = 0.6)", () => {
    // mean = 2.5 both; sxx = 5, syy = 5, sxy = 3 -> r = 0.6.
    const { r, n } = pearson([1, 2, 3, 4], [2, 1, 4, 3]);
    expect(n).toBe(4);
    expect(r).toBeCloseTo(0.6, 12);
  });

  it("zero variance in one series gives r = null", () => {
    const flat = pearson([5, 5, 5, 5], [1, 2, 3, 4]);
    expect(flat).toEqual({ r: null, n: 4, reason: "zero_variance" });
    const flatY = pearson([1, 2, 3], [7, 7, 7]);
    expect(flatY).toEqual({ r: null, n: 3, reason: "zero_variance" });
  });

  it("fewer than 3 pairs gives r = null", () => {
    expect(pearson([1, 2], [3, 4])).toEqual({
      r: null,
      n: 2,
      reason: "insufficient",
    });
    expect(pearson([1], [3])).toEqual({ r: null, n: 1, reason: "insufficient" });
    expect(pearson([], [])).toEqual({ r: null, n: 0, reason: "insufficient" });
  });

  it("reason is null when r is valid", () => {
    expect(pearson([1, 2, 3], [2, 4, 6]).reason).toBeNull();
  });

  it("flat price series (Bangus-style) reports zero_variance even with large n", () => {
    // Bangus flat at ₱270 in Las Piñas over 90 days vs a moving FX series.
    const price = Array(90).fill(270);
    const fx = Array.from({ length: 90 }, (_, i) => 58 + i * 0.01);
    const out = pearson(price, fx);
    expect(out.n).toBe(90);
    expect(out.r).toBeNull();
    expect(out.reason).toBe("zero_variance");
  });

  it("null entries are skipped pairwise", () => {
    // pairs: (1,2), (3,1), (4,4) -> r = 0.5, n = 3.
    const { r, n } = pearson(
      [1, null, 3, 4, null],
      [2, 99, 1, 4, 99],
    );
    expect(n).toBe(3);
    expect(r).toBeCloseTo(0.5, 12);
  });
});

describe("asofLookup", () => {
  // Mon 2026-10-19 .. Fri 2026-10-23 (ECB has no weekend rates).
  const fxByDate = new Map([
    ["2026-10-19", 58.0],
    ["2026-10-20", 58.1],
    ["2026-10-21", 58.2],
    ["2026-10-22", 58.3],
    ["2026-10-23", 58.4],
  ]);
  const fxDates = [...fxByDate.keys()].sort();

  it("exact date resolves to that day's rate", () => {
    expect(asofLookup(fxByDate, fxDates, "2026-10-21")).toBe(58.2);
  });

  it("weekend resolves to Friday (2 days stale, within limit)", () => {
    expect(asofLookup(fxByDate, fxDates, "2026-10-24")).toBe(58.4);
    expect(asofLookup(fxByDate, fxDates, "2026-10-25")).toBe(58.4);
  });

  it("more than 5 days stale gives null", () => {
    // 2026-10-23 -> 2026-10-29 is 6 days stale.
    expect(asofLookup(fxByDate, fxDates, "2026-10-29")).toBeNull();
  });

  it("target before the first rate gives null", () => {
    expect(asofLookup(fxByDate, fxDates, "2026-10-18")).toBeNull();
  });
});

describe("laggedCorrelation", () => {
  const fx: Array<readonly [string, number | null]> = [
    ["2026-10-19", 58.0],
    ["2026-10-20", 58.1],
    ["2026-10-21", 58.2],
    ["2026-10-22", 58.3],
    ["2026-10-23", 58.4],
  ];

  it("pairs price d with FX as-of d - lag", () => {
    const prices: Array<readonly [string, number | null]> = [
      ["2026-10-21", 100.0],
      ["2026-10-22", 101.0],
      ["2026-10-23", 102.0],
      // Monday: as-of Friday 10-23 (3 days stale, still usable).
      ["2026-10-26", 103.0],
      // Missing price days are skipped, not treated as zero.
      ["2026-10-27", null],
    ];
    const out = laggedCorrelation(prices, fx, 0);
    expect(out.lag).toBe(0);
    expect(out.n).toBe(4);
    expect(out.r).toBeCloseTo(0.9438798074485406, 9);
  });

  it("lag 7 skips prices with no FX available that far back", () => {
    const prices: Array<readonly [string, number | null]> = [
      ["2026-10-21", 100.0],
      ["2026-10-22", 101.0],
      ["2026-10-23", 102.0],
      ["2026-10-26", 103.0],
    ];
    // Only 10-26 -> 10-19 pairs up; n = 1 < 3 so r is null.
    expect(laggedCorrelation(prices, fx, 7)).toEqual({
      lag: 7,
      r: null,
      n: 1,
      reason: "insufficient",
    });
  });

  it("identical series give r = 1", () => {
    const prices = fx.map(
      ([d, r]): readonly [string, number | null] => [d, r],
    );
    const out = laggedCorrelation(prices, fx, 0);
    expect(out.n).toBe(5);
    expect(out.r).toBeCloseTo(1.0, 12);
    expect(out.reason).toBeNull();
  });

  it("flat price over a long window reports zero_variance, not insufficient", () => {
    const many: Array<readonly [string, number | null]> = [];
    const longFx: Array<readonly [string, number | null]> = [];
    for (let i = 0; i < 30; i++) {
      many.push([`2026-09-${String(i + 1).padStart(2, "0")}`, 270.0]);
      longFx.push([`2026-09-${String(i + 1).padStart(2, "0")}`, 58 + i * 0.01]);
    }
    const out = laggedCorrelation(many, longFx, 0);
    expect(out.n).toBe(30);
    expect(out.r).toBeNull();
    expect(out.reason).toBe("zero_variance");
  });
});

describe("shiftDate", () => {
  it("shifts across month boundaries", () => {
    expect(shiftDate("2026-10-23", -7)).toBe("2026-10-16");
    expect(shiftDate("2026-10-01", -1)).toBe("2026-09-30");
    expect(shiftDate("2026-10-23", 0)).toBe("2026-10-23");
  });
});
