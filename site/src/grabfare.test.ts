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
    route: { from: "Old Zapote Public Market", to: "Festival Supermall Alabang", via: "Alabang-Zapote Road" },
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
    expect(v!.text.en.route).toBe("Old Zapote Public Market → Festival Supermall Alabang (Alabang-Zapote Road)");
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

describe("evalGrabFare pickup note", () => {
  it("sets pickupNote trimmed", () => {
    const b = base();
    (b.text as any).tl.pickup_note = "  note tl  ";
    (b.text as any).en.pickup_note = "note en";
    (b.text as any).ja.pickup_note = "note ja";
    const v = evalGrabFare(b, NOW)!;
    expect(v.text.tl.pickupNote).toBe("note tl");
    expect(v.text.en.pickupNote).toBe("note en");
    expect(v.text.ja.pickupNote).toBe("note ja");
  });

  it("empty/whitespace/non-string/absent -> undefined", () => {
    const b = base();
    (b.text as any).tl.pickup_note = "   ";
    (b.text as any).en.pickup_note = 42;
    const v = evalGrabFare(b, NOW)!;
    expect(v.text.tl.pickupNote).toBeUndefined();
    expect(v.text.en.pickupNote).toBeUndefined();
    expect(v.text.ja.pickupNote).toBeUndefined();
  });

  it("truncates >300 chars to 300", () => {
    const b = base();
    (b.text as any).en.pickup_note = "a".repeat(350);
    const v = evalGrabFare(b, NOW)!;
    expect(v.text.en.pickupNote).toHaveLength(300);
  });

  it("fixture without pickup field/era evaluates as before", () => {
    const v1 = evalGrabFare(base(), NOW);
    const v2 = evalGrabFare(base(), NOW);
    expect(v1).toEqual(v2);
    expect(v1!.text.en.pickupNote).toBeUndefined();
  });
});

describe("evalGrabFare route names", () => {
  it("Old Zapote Public Market route", () => {
    const v = evalGrabFare(base(), NOW)!;
    expect(v.text.en.route).toBe("Old Zapote Public Market → Festival Supermall Alabang (Alabang-Zapote Road)");
  });

  it("Zapote Bridge route", () => {
    const b = base({ route: { from: "Zapote Bridge", to: "Festival Supermall Alabang", via: "Alabang-Zapote Road" } });
    const v = evalGrabFare(b, NOW)!;
    expect(v.text.en.route).toBe("Zapote Bridge → Festival Supermall Alabang (Alabang-Zapote Road)");
  });
});

describe("evalGrabFare bike", () => {
  function bike(overrides: Record<string, unknown> = {}) {
    return {
      label: "Motor taxi (Move It)",
      fresh: true,
      text: {
        tl: { headline: "h tl", asof: "a tl" },
        en: { headline: "h en", asof: "a en" },
        ja: { headline: "h ja", asof: "a ja" },
      },
      hourly: { today: [{ h: 12, min: 147, max: 147 }] },
      ...overrides,
    };
  }

  it("bike fresh parsed", () => {
    const v = evalGrabFare(base({ bike: bike() }), NOW)!;
    expect(v.bike).not.toBeNull();
    expect(v.bike!.label).toBe("Motor taxi (Move It)");
    expect(v.bike!.fresh).toBe(true);
    expect(v.bike!.text.tl).toEqual({ headline: "h tl", asof: "a tl" });
    expect(v.bike!.text.en).toEqual({ headline: "h en", asof: "a en" });
    expect(v.bike!.text.ja).toEqual({ headline: "h ja", asof: "a ja" });
    expect(v.bike!.hourly).not.toBeNull();
    expect(v.bike!.hourly!.today).toHaveLength(1);
    expect(v.bike!.hourly!.today[0]).toEqual({ h: 12, min: 147, max: 147, era: 0 });
  });

  it("bike stale parsed", () => {
    const b = bike({
      fresh: false,
      text: {
        tl: { headline: "Motor taxi (Move It): —", asof: "Walang bagong Move It quote sa nakaraang 90 minuto" },
        en: { headline: "Motor taxi (Move It): —", asof: "No Move It quote in the last 90 minutes" },
        ja: { headline: "Motor taxi (Move It): —", asof: "直近90分のMove It表示はありません" },
      },
    });
    const v = evalGrabFare(base({ bike: b }), NOW)!;
    expect(v.bike).not.toBeNull();
    expect(v.bike!.fresh).toBe(false);
    expect(v.bike!.text.tl.headline).toBe("Motor taxi (Move It): —");
    expect(v.bike!.text.en.headline).toBe("Motor taxi (Move It): —");
    expect(v.bike!.text.ja.headline).toBe("Motor taxi (Move It): —");
  });

  it("bike absent -> view.bike null", () => {
    const v = evalGrabFare(base(), NOW)!;
    expect(v.bike).toBeNull();
  });

  it("bike absent rest identical", () => {
    const v0 = evalGrabFare(base(), NOW)!;
    const v1 = evalGrabFare(base({ bike: bike() }), NOW)!;
    expect({ ...v1, bike: null }).toEqual({ ...v0, bike: null });
  });

  it("bike non-object/array/string -> null, card evaluates", () => {
    for (const bad of [null, 42, "str", [1, 2], true]) {
      const v = evalGrabFare(base({ bike: bad }), NOW);
      expect(v).not.toBeNull();
      expect(v!.bike).toBeNull();
    }
  });

  it("bike text missing language -> null", () => {
    const b = bike();
    delete (b.text as any).ja;
    const v = evalGrabFare(base({ bike: b }), NOW)!;
    expect(v).not.toBeNull();
    expect(v.bike).toBeNull();
  });

  it("bike empty headline or asof -> null", () => {
    const mk = (headline: string, asof: string) => bike({
      text: {
        tl: { headline, asof },
        en: { headline, asof },
        ja: { headline, asof },
      },
    });
    expect(evalGrabFare(base({ bike: mk("", "a") }), NOW)!.bike).toBeNull();
    expect(evalGrabFare(base({ bike: mk("h", "") }), NOW)!.bike).toBeNull();
  });

  it("bike headline or asof > 200 chars -> null", () => {
    const long = "a".repeat(201);
    const mk = (headline: string, asof: string) => bike({
      text: {
        tl: { headline, asof },
        en: { headline, asof },
        ja: { headline, asof },
      },
    });
    expect(evalGrabFare(base({ bike: mk(long, "a") }), NOW)!.bike).toBeNull();
    expect(evalGrabFare(base({ bike: mk("h", long) }), NOW)!.bike).toBeNull();
  });

  it("bike label empty or > 60 chars -> null", () => {
    expect(evalGrabFare(base({ bike: bike({ label: "" }) }), NOW)!.bike).toBeNull();
    expect(evalGrabFare(base({ bike: bike({ label: "a".repeat(61) }) }), NOW)!.bike).toBeNull();
  });

  it("bike fresh not boolean -> null", () => {
    expect(evalGrabFare(base({ bike: bike({ fresh: "true" as any }) }), NOW)!.bike).toBeNull();
    expect(evalGrabFare(base({ bike: bike({ fresh: 1 as any }) }), NOW)!.bike).toBeNull();
  });

  it("bike hourly empty lists -> hourly null, bike non-null", () => {
    const b = bike({ hourly: { today: [], yesterday: [] } });
    const v = evalGrabFare(base({ bike: b }), NOW)!;
    expect(v.bike).not.toBeNull();
    expect(v.bike!.hourly).toBeNull();
  });

  it("bike hourly garbage -> hourly null", () => {
    const b = bike({ hourly: "garbage" as any });
    const v = evalGrabFare(base({ bike: b }), NOW)!;
    expect(v.bike).not.toBeNull();
    expect(v.bike!.hourly).toBeNull();
  });
});
