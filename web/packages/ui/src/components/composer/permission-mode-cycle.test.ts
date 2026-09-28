import { describe, expect, it } from "vitest";

import { resolveNextPermissionMode } from "@/components/composer/permission-mode-cycle";

const OPTIONS = [{ id: "acceptEdits" }, { id: "default" }, { id: "plan" }] as const;

describe("resolveNextPermissionMode (W3)", () => {
  it("advances to the next mode", () => {
    expect(resolveNextPermissionMode(OPTIONS, "acceptEdits")).toBe("default");
    expect(resolveNextPermissionMode(OPTIONS, "default")).toBe("plan");
  });

  it("wraps from the last mode back to the first", () => {
    expect(resolveNextPermissionMode(OPTIONS, "plan")).toBe("acceptEdits");
  });

  it("starts from the first mode when the current value isn't in the list", () => {
    expect(resolveNextPermissionMode(OPTIONS, "auto-safe")).toBe("default");
  });

  it("returns null with fewer than two modes -- nothing to cycle to", () => {
    expect(resolveNextPermissionMode([{ id: "default" }], "default")).toBeNull();
    expect(resolveNextPermissionMode([], "default")).toBeNull();
  });
});
