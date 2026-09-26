import { describe, expect, it } from "vitest";

import {
  APPROVAL_POLICY_AUTO_SAFE,
  APPROVAL_POLICY_MANUAL,
  approvalPolicies,
  approvalPolicyLabel,
  fullAccessPolicy,
  liveSwitchablePolicies,
} from "@/lib/approval-policies";

/**
 * Run-config IA: one approval-policy vocabulary consumed by the composer,
 * the agent form, General's defaults and the mid-run control. This suite
 * pins the shared strings themselves -- the label unification criterion
 * is about identical wording everywhere, so a change here must be a
 * deliberate one, not a copy-paste drift in one of the four consumers.
 */
describe("approval-policies vocabulary", () => {
  it("manual and auto-safe have one label each", () => {
    expect(APPROVAL_POLICY_MANUAL).toEqual({
      id: "manual",
      label: "Manual approval",
      help: expect.any(String),
    });
    expect(APPROVAL_POLICY_AUTO_SAFE.label).toBe("Auto-safe");
  });

  it("full-access keeps each provider's own real vocabulary", () => {
    expect(fullAccessPolicy("claude-native").label).toBe("Bypass");
    expect(fullAccessPolicy("codex-native").label).toBe("Full Access");
    expect(fullAccessPolicy("glm").label).toBe("Bypass all permissions");
    // An unknown provider falls back to Claude's, never a generic string.
    expect(fullAccessPolicy("kimi" as never).label).toBe("Bypass all permissions");
  });

  it("approvalPolicies returns the three tiers in order", () => {
    expect(approvalPolicies("claude-native").map((p) => p.id)).toEqual([
      "manual",
      "auto-safe",
      "full-access",
    ]);
  });

  it("the mid-run control's two tiers use the same labels as the composer", () => {
    const composer = approvalPolicies("claude-native").slice(0, 2);
    expect(liveSwitchablePolicies()).toEqual(composer);
  });

  it("approvalPolicyLabel answers the shared label without provider context", () => {
    expect(approvalPolicyLabel("manual")).toBe("Manual approval");
    expect(approvalPolicyLabel("auto-safe")).toBe("Auto-safe");
    expect(approvalPolicyLabel("full-access")).toBe("Full access");
  });
});
