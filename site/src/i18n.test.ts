import { describe, expect, it } from "vitest";
import { STRINGS } from "./i18n";

describe("footerPrivacy", () => {
  it("has the privacy notice in all languages", () => {
    expect(STRINGS.tl.footerPrivacy).toBe(
      "Gumagamit ang site na ito ng Google Analytics para sa bilang ng bisita.",
    );
    expect(STRINGS.en.footerPrivacy).toBe(
      "This site uses Google Analytics to count visits.",
    );
    expect(STRINGS.ja.footerPrivacy).toBe(
      "アクセス解析にGoogleアナリティクスを使っています。",
    );
  });
});
