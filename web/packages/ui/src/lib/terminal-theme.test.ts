import { describe, expect, it } from "vitest";

import { resolveTerminalTheme } from "@/lib/terminal-theme";

describe("resolveTerminalTheme", () => {
  it("the terminal background and cursor accent read the pane surface token (--color-panel), not --color-terminal-bg", () => {
    // jsdom has no cascade and echoes the var() reference it was given, which names the token that is read.
    const theme = resolveTerminalTheme();
    expect(theme.background).toBe("var(--color-panel)");
    expect(theme.cursorAccent).toBe("var(--color-panel)");
    expect(theme.foreground).toBe("var(--color-terminal-fg)");
  });
});
