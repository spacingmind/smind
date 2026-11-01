import { describe, expect, it } from "vitest";

import { PERMISSION_REASON_LABEL, permissionResolutionLabel } from "@/components/timeline/permission-reason";

describe("PERMISSION_REASON_LABEL", () => {
  it.each([
    ["human", "You approved", "success"],
    ["auto_accept", "Auto-accepted", "running"],
    ["auto_safe", "Auto-approved (legacy)", "running"],
    ["policy_allow", "Allowed by policy", "running"],
    ["policy_deny", "Denied by policy", "warning"],
    ["timeout", "Timed out", "warning"],
    ["provider_cancellation", "Cancelled by provider", "warning"],
  ] as const)("resolves %s to label %j with status %j", (reason, label, status) => {
    expect(PERMISSION_REASON_LABEL[reason]).toEqual({ label, status });
  });

  it("the label map itself has no entry for an unknown reason; the fallback lives in permissionResolutionLabel", () => {
    expect(PERMISSION_REASON_LABEL["some_future_reason"]).toBeUndefined();
  });
});

describe("permissionResolutionLabel", () => {
  it("policy resolutions have their own labels", () => {
    expect(permissionResolutionLabel({ reason: "policy_allow" })).toEqual({ label: "Allowed by policy", status: "running" });
    expect(permissionResolutionLabel({ reason: "policy_deny" })).toEqual({ label: "Denied by policy", status: "warning" });
  });

  it("unknown permission resolution renders as text", () => {
    expect(permissionResolutionLabel({ reason: "frobnicated" })).toEqual({ label: "frobnicated", status: "neutral" });
  });

  it("a missing reason still has no badge, and a human rejection reads denied", () => {
    expect(permissionResolutionLabel({})).toBeUndefined();
    expect(permissionResolutionLabel({ reason: "human", optionKind: "reject_once" })).toEqual({ label: "You denied", status: "warning" });
  });
});
