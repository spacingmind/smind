import { describe, expect, it } from "vitest";

import { SHELL_FRAME_INSET_CLASS, SHELL_FRAME_PANEL_CLASS } from "@/lib/shell-frame";

describe("shell frame classes", () => {
  it("light frame carries gap, border, rounded and shadow", () => {
    expect(SHELL_FRAME_INSET_CLASS).toContain("p-1.5");
    expect(SHELL_FRAME_INSET_CLASS).toContain("pl-0");
    expect(SHELL_FRAME_PANEL_CLASS).toContain("rounded-xl");
    expect(SHELL_FRAME_PANEL_CLASS).toContain("border");
    expect(SHELL_FRAME_PANEL_CLASS).toContain("shadow-[");
  });

  it("dark frame resets the gap and shadow", () => {
    expect(SHELL_FRAME_INSET_CLASS).toContain("dark:p-0");
    expect(SHELL_FRAME_PANEL_CLASS).toContain("dark:shadow-none");
    expect(SHELL_FRAME_PANEL_CLASS).toContain("dark:border-0");
  });
});
