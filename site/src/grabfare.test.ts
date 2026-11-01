import { describe, expect, it } from "vitest";
import { evalGrabFare, type GrabFareView } from "./grabfare";

const NOW = Date.parse("2026-11-02T08:00:00+08:00");

function base(overrides: Record<string, unknown> = {}) {
  return {
    visible: true,
    state: "fresh",
    valid_until: "2026-11-02T10:00:00+08:00",
    as_of: "2026-11-02T08:00:00+08:00",
    fallback: false,
    route: { from: "Zapote Bridge", to: "Festival Supermall Alabang", via: "Alabang-Zapote Road" },
    trend: { direction: "up", delta_pesos: 12, delta_pct: 4, previous_as_of: "x", previous_hhmm: "07:00" },
    vs_yesterday: null,
    text: {
      tl: { title: "T", headline: "H", asof: "A", traffic: "Trapiko: mabigat", trend: "▲ ₱12 (+4%) kumpara sa 07:00", note: "N" },
      en: { title: "T", headline: "H", asof: "A", traffic: "Traffic: heavy", trend: "▲ ₱12 (+4%) vs 07:00", note: "N" },
      ja: { title: "T", headline: "H", asof: "A", traffic: "交通状況：混雑", trend: "▲ ₱12（+4%） 07:00比", note: "N" },
    },
    ...overrides,
  };
}

describe("evalGrabFare", () => {
  it("fresh ok", () => {
    const v = evalGrabFare(base(), NOW);
    expect(v).not.toBeNull();
    expect(v!.fallback).toBe(false);
    expect(v!.trendDir).toBe("up");
    expect(v!.text.en.route).toBe("Zapote Bridge → Festival Supermall Alabang (Alabang-Zapote Road)");
  });

  it("expired valid_until -> null", () => {
    expect(evalGrabFare(base({ valid_until: "2026-11-02T07:00:00+08:00" }), NOW)).toBeNull();
  });

  it("visible false -> null", () => {
    expect(evalGrabFare(base({ visible: false }), NOW)).toBeNull();
  });

  it("stale/unavailable -> null", () => {
    expect(evalGrabFare(base({ state: "stale" }), NOW)).toBeNull();
    expect(evalGrabFare(base({ state: "unavailable" }), NOW)).toBeNull();
  });

  it("future as_of -> null", () => {
    const future = new Date(NOW + 20 * 60 * 1000).toISOString();
    expect(evalGrabFare(base({ as_of: future }), NOW)).toBeNull();
  });

  it("garbage/missing text -> null", () => {
    expect(evalGrabFare(base({ text: {} }), NOW)).toBeNull();
    const b = base();
    (b.text as any).en.headline = "";
    expect(evalGrabFare(b, NOW)).toBeNull();
  });

  it("yesterday line wording up/down/flat", () => {
    const vy = { direction: "down", delta_pesos: -12, delta_pct: -3, previous_hhmm: "23:00" };
    const v = evalGrabFare(base({ vs_yesterday: vy }), NOW) as GrabFareView;
    expect(v.text.en.yesterday).toBe("▼ ₱12 (-3%) vs yesterday 23:00");
    expect(v.text.tl.yesterday).toBe("▼ ₱12 (-3%) kumpara kahapon 23:00");
    expect(v.text.ja.yesterday).toBe("▼ ₱12（-3%） 昨日23:00比");
    const vy2 = { direction: "up", delta_pesos: 5, delta_pct: 2, previous_hhmm: "23:00" };
    const v2 = evalGrabFare(base({ vs_yesterday: vy2 }), NOW) as GrabFareView;
    expect(v2.text.en.yesterday).toBe("▲ ₱5 (+2%) vs yesterday 23:00");
    const vy3 = { direction: "flat", delta_pesos: 0, delta_pct: 0, previous_hhmm: "23:00" };
    const v3 = evalGrabFare(base({ vs_yesterday: vy3 }), NOW) as GrabFareView;
    expect(v3.text.en.yesterday).toBe("＝ No change vs yesterday 23:00");
    expect(v3.text.tl.yesterday).toBe("＝ Walang pagbabago kumpara kahapon 23:00");
    expect(v3.text.ja.yesterday).toBe("＝ 昨日23:00比で変化なし");
  });

  it("fallback flag", () => {
    const v = evalGrabFare(base({ state: "fallback", fallback: true }), NOW);
    expect(v!.fallback).toBe(true);
  });

  it("invalid JSON types", () => {
    expect(evalGrabFare(null, NOW)).toBeNull();
    expect(evalGrabFare("str", NOW)).toBeNull();
    expect(evalGrabFare(42, NOW)).toBeNull();
    expect(evalGrabFare([], NOW)).toBeNull();
  });
});
