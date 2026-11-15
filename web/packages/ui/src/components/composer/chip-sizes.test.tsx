import { describe, expect, it } from "vitest";

import { PILL_LABELLED_TRIGGER_CLASS, PILL_TRIGGER_CLASS } from "@/components/composer/pill-class";

describe("composer pill sizes", () => {
  it("chips-are-28px-with-13px-text-on-desktop", () => {
    for (const cls of [PILL_TRIGGER_CLASS, PILL_LABELLED_TRIGGER_CLASS]) {
      expect(cls).toContain("md:h-7");
      expect(cls).toContain("md:text-ui-caption");
      expect(cls).not.toContain("md:text-ui-sm");
    }
    expect(PILL_TRIGGER_CLASS).toContain("size-11");
    expect(PILL_LABELLED_TRIGGER_CLASS).toContain("h-11");
  });
});
