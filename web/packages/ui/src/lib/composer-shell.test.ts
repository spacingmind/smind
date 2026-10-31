import { describe, expect, it } from "vitest";

import { COMPOSER_ACTION_BUTTON_CLASS, COMPOSER_CARD_CLASS } from "@/lib/composer-shell";

describe("composer shell classes", () => {
  it("card and send classes are shared constants", () => {
    expect(COMPOSER_CARD_CLASS).toContain("rounded-2xl");
    expect(COMPOSER_CARD_CLASS).toContain("border-input-border");
    expect(COMPOSER_ACTION_BUTTON_CLASS).toContain("rounded-full");
    expect(COMPOSER_ACTION_BUTTON_CLASS).toContain("md:size-8");
  });
});
