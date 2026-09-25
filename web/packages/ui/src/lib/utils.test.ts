import { describe, expect, it } from "vitest";

import { cn } from "@/lib/utils";

/**
 * Regression coverage for the tailwind-merge misconfiguration this file's
 * `cn()` comment describes: `text-ui-*` (index.css's font-size scale) must
 * not be dropped when it's merged alongside a `text-{color}` class -- the
 * exact shape every selected/active variant produces (a size class from a
 * shared component's base classes, a color class appended by the call
 * site's conditional className). Before `extendTailwindMerge` registered
 * `text-ui-*` under the `font-size` group, `cn()` treated it as an
 * unrecognized (therefore color-group-by-default) class and silently
 * dropped it whenever a real color class came after it in the merge.
 */
describe("cn()", () => {
  it("keeps a text-ui-* size alongside a later text-{color} class", () => {
    expect(cn("text-ui-xs", "text-accent-foreground")).toBe("text-ui-xs text-accent-foreground");
    expect(cn("text-ui-sm", "text-foreground")).toBe("text-ui-sm text-foreground");
  });

  it("keeps a text-ui-* size alongside an earlier text-{color} class", () => {
    expect(cn("text-foreground", "text-ui-base")).toBe("text-foreground text-ui-base");
  });

  it("still resolves two conflicting text-ui-* sizes to the last one", () => {
    expect(cn("text-ui-xl", "text-ui-sm")).toBe("text-ui-sm");
    expect(cn("text-ui-sm", "text-ui-xs")).toBe("text-ui-xs");
  });

  it("still resolves two conflicting text-{color} classes to the last one", () => {
    expect(cn("text-foreground", "text-destructive")).toBe("text-destructive");
  });

  it("still resolves two conflicting rounded-* classes to the last one (sanity check, unrelated group)", () => {
    expect(cn("rounded-lg", "rounded-md")).toBe("rounded-md");
  });
});
