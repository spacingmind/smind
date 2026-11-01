import { describe, expect, it } from "vitest";

import { formatCooldownUntil, formatResetLabel } from "@/lib/account-usage/format";
import { buildBrowserProviders } from "@/lib/model-browser";
import { gateActionLabel, gateBadge, gateDetail, gateReasonText, isHardGate, liveGate } from "@/lib/provider-gate";
import type { ProviderGate, ProviderInfo } from "@/lib/types";

const NOW = Date.parse("2026-11-01T10:00:00Z");
const future = new Date(NOW + 2 * 3600_000).toISOString();
const past = new Date(NOW - 60_000).toISOString();

describe("provider gate helpers", () => {
  it("an advisory gate is not hard; every other gate is, an unknown reason included", () => {
    expect(isHardGate({ reason: "not_signed_in" })).toBe(true);
    expect(isHardGate({ reason: "not_signed_in", advisory: true })).toBe(false);
    expect(isHardGate({ reason: "something_new" })).toBe(true);
    expect(isHardGate(undefined)).toBe(false);
  });

  it("a time gate whose until has passed is ungated; one without until stays", () => {
    expect(liveGate({ reason: "cooling_down", until: past }, NOW)).toBeUndefined();
    expect(liveGate({ reason: "out_of_plan", until: past }, NOW)).toBeUndefined();
    expect(liveGate({ reason: "cooling_down", until: future }, NOW)).toBeDefined();
    expect(liveGate({ reason: "out_of_plan" }, NOW)).toBeDefined();
    expect(liveGate({ reason: "not_signed_in", until: past }, NOW)).toBeDefined();
  });

  it("reason text per reason; out_of_plan and cooling_down reuse the usage meter's formatters", () => {
    expect(gateReasonText({ reason: "not_signed_in" }, NOW)).toBe("Signed out");
    expect(gateReasonText({ reason: "unsupported_by_harness" }, NOW)).toBe("Not supported by this harness");
    expect(gateReasonText({ reason: "model_not_in_plan" }, NOW)).toBe("Not in your plan");
    expect(gateReasonText({ reason: "out_of_plan", until: future }, NOW)).toBe(`Plan used up · ${formatResetLabel(future, NOW)}`);
    expect(gateReasonText({ reason: "cooling_down", until: future }, NOW)).toBe(`Cooling down until ${formatCooldownUntil(Date.parse(future), NOW)}`);
    expect(gateReasonText({ reason: "out_of_plan" }, NOW)).toBe("Plan used up");
  });

  it("the row detail adds only what the badge does not say", () => {
    expect(gateDetail({ reason: "not_signed_in" }, NOW)).toBe("");
    expect(gateDetail({ reason: "unsupported_by_harness" }, NOW)).toBe("");
    expect(gateDetail({ reason: "model_not_in_plan" }, NOW)).toBe("");
    expect(gateDetail({ reason: "out_of_plan", until: future }, NOW)).toBe(formatResetLabel(future, NOW));
    expect(gateDetail({ reason: "out_of_plan" }, NOW)).toBe("");
    expect(gateDetail({ reason: "cooling_down", until: future }, NOW)).toBe(`until ${formatCooldownUntil(Date.parse(future), NOW)}`);
    expect(gateDetail({ reason: "fresh", action: { kind: "x", label: "Do the thing" } }, NOW)).toBe("Do the thing");
  });

  it("badge labels are the short reasons", () => {
    expect(gateBadge({ reason: "out_of_plan" }).label).toBe("Plan used up");
    expect(gateBadge({ reason: "unsupported_by_harness" }).label).toBe("Unsupported");
  });

  it("an unknown reason shows its action label, else the reason", () => {
    expect(gateReasonText({ reason: "fresh", action: { kind: "x", label: "Do the thing" } }, NOW)).toBe("Do the thing");
    expect(gateReasonText({ reason: "fresh" }, NOW)).toBe("fresh");
  });

  it("action labels: a control only for kinds the picker can run", () => {
    const kinds: Array<[string, string | undefined]> = [
      ["sign_in", "Sign in"],
      ["clear_cooldown", "Clear"],
      ["switch_profile", "Switch"],
      ["pick_model", "Pick model"],
      ["pick_provider", "Pick provider"],
      ["wait_until", undefined],
      ["retry_now", undefined],
    ];
    for (const [kind, want] of kinds) {
      expect(gateActionLabel({ reason: "r", action: { kind, label: "l" } })).toBe(want);
    }
    expect(gateActionLabel({ reason: "r" })).toBeUndefined();
  });

  it("badge tone: advisory and time gates warn, setup gates are danger", () => {
    expect(gateBadge({ reason: "not_signed_in" }).status).toBe("danger");
    expect(gateBadge({ reason: "unsupported_by_harness" }).status).toBe("danger");
    expect(gateBadge({ reason: "cooling_down" }).status).toBe("warning");
    expect(gateBadge({ reason: "out_of_plan" }).status).toBe("warning");
    expect(gateBadge({ reason: "not_signed_in", advisory: true }).status).toBe("warning");
  });
});

describe("buildBrowserProviders gates", () => {
  const pg: ProviderGate = { reason: "cooling_down", until: future };
  const mg: ProviderGate = { reason: "model_not_in_plan", advisory: true };
  const infos: ProviderInfo[] = [
    { id: "glm", label: "GLM", gate: pg, models: [{ id: "a", label: "A" }] },
    { id: "claude-native", label: "Claude Code", modelGates: { opus: mg }, models: [{ id: "opus", label: "Opus" }, { id: "sonnet", label: "Sonnet" }] },
  ];

  it("copies the provider gate; the default row and models inherit it", () => {
    const [glm] = buildBrowserProviders(infos, {});
    expect(glm!.gate).toEqual(pg);
    const rows = glm!.modelSelection.kind === "models" ? glm!.modelSelection.rows : [];
    expect(rows.map((r) => r.gate)).toEqual([pg, pg]);
  });

  it("a model gate applies to that model only", () => {
    const claude = buildBrowserProviders(infos, {})[1]!;
    expect(claude.gate).toBeUndefined();
    const rows = claude.modelSelection.kind === "models" ? claude.modelSelection.rows : [];
    expect(Object.fromEntries(rows.map((r) => [r.modelId, r.gate]))).toEqual({ "": undefined, opus: mg, sonnet: undefined });
  });
});
