import { describe, expect, it } from "vitest";
import {
  dealsTextForLang,
  dealsTextToLines,
  isDealsSubheading,
  splitPesoSegments,
  type DealsFile,
} from "./deals";

const sample: DealsFile = {
  as_of: "2026-10-23",
  generated_at: "2026-10-24T00:00:00+08:00",
  text_source: "template",
  texts: {
    tl: "🛒 Sulit ngayon sa palengke (2026-10-23)\n• Sayote: ₱60/kg\n\nMas mahal ngayon:\n• Pulang sili: ₱450/kg",
    en: "🛒 Today's market deals (2026-10-23)\n• Chayote: ₱60/kg",
    ja: "🛒 今日のお買い得（2026-10-23）\n• サヨテ： ₱60",
  },
  uses_nearby_basis: false,
};

describe("dealsTextToLines", () => {
  it("splits on newlines, trims, and drops blank lines", () => {
    expect(dealsTextToLines(sample.texts.tl)).toEqual([
      "🛒 Sulit ngayon sa palengke (2026-10-23)",
      "• Sayote: ₱60/kg",
      "Mas mahal ngayon:",
      "• Pulang sili: ₱450/kg",
    ]);
  });

  it("returns [] for empty or blank text", () => {
    expect(dealsTextToLines("")).toEqual([]);
    expect(dealsTextToLines("\n  \n")).toEqual([]);
  });
});

describe("isDealsSubheading", () => {
  it("treats ASCII and full-width colons as sub-headings", () => {
    expect(isDealsSubheading("Pricier than usual:")).toBe(true);
    expect(isDealsSubheading("Mas mahal ngayon:")).toBe(true);
    expect(isDealsSubheading("いつもより高め：")).toBe(true);
    expect(isDealsSubheading("• Sayote: ₱60/kg")).toBe(false);
    expect(isDealsSubheading("Batay sa DA price monitoring.")).toBe(false);
  });
});

describe("splitPesoSegments", () => {
  it("marks peso amounts incl. sign/parens as nowrap", () => {
    const segs = splitPesoSegments("• Test: ₱130/kg — (-₱90)");
    const wrapped = segs.filter((s) => s.nowrap).map((s) => s.text);
    expect(wrapped).toEqual(["₱130", "(-₱90)"]);
    expect(segs.map((s) => s.text).join("")).toBe(
      "• Test: ₱130/kg — (-₱90)",
    );
    const ja = splitPesoSegments("いつもより40.9%安い（-₱90）").filter(
      (s) => s.nowrap,
    );
    expect(ja.map((s) => s.text)).toEqual(["（-₱90）"]);
  });
});
describe("dealsTextForLang", () => {
  it("picks the text for each language", () => {
    expect(dealsTextForLang(sample, "tl")).toBe(sample.texts.tl);
    expect(dealsTextForLang(sample, "en")).toBe(sample.texts.en);
    expect(dealsTextForLang(sample, "ja")).toBe(sample.texts.ja);
  });
});
