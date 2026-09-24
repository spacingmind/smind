import { describe, expect, it } from "vitest";

import { resolveNextApprovalPolicy } from "@/components/composer/approval-policy-cycle";

const OPTIONS = [{ id: "manual" }, { id: "auto-safe" }, { id: "full-access" }] as const;

describe("resolveNextApprovalPolicy", () => {
  it("advances to the next option", () => {
    expect(resolveNextApprovalPolicy(OPTIONS, "manual")).toBe("auto-safe");
    expect(resolveNextApprovalPolicy(OPTIONS, "auto-safe")).toBe("full-access");
  });

  it("wraps from the last option back to the first", () => {
    expect(resolveNextApprovalPolicy(OPTIONS, "full-access")).toBe("manual");
  });

  it("starts from the first option when the current value isn't in the list", () => {
    expect(resolveNextApprovalPolicy(OPTIONS, "not-a-real-policy" as never)).toBe("auto-safe");
  });

  it("returns null with fewer than two options -- nothing to cycle to", () => {
    expect(resolveNextApprovalPolicy([{ id: "manual" }], "manual")).toBeNull();
    expect(resolveNextApprovalPolicy([], "manual")).toBeNull();
  });
});
