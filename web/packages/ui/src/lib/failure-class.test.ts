import { afterEach, describe, expect, it } from "vitest";
import {
  describeFailureCard,
  failureCountdown,
  isActionableFailure,
  openFailureSettings,
  parseSettingsTarget,
  type FailureAction,
} from "@/lib/failure-class";

const NOW = new Date("2026-11-01T10:00:00Z");

describe("isActionableFailure", () => {
  it("is true for every class the daemon sends except cli_exited", () => {
    for (const k of ["auth_expired", "plan_window_exhausted", "model_not_in_plan", "transient_429", "rate_limited"]) expect(isActionableFailure(k)).toBe(true);
    for (const k of ["cli_exited", "", undefined, "martian"]) expect(isActionableFailure(k)).toBe(false);
  });
});

describe("parseSettingsTarget", () => {
  it("reads settings:providers with an account or a provider", () => {
    expect(parseSettingsTarget("settings:providers?account=7")).toEqual({ sectionId: "providers", accountId: 7 });
    expect(parseSettingsTarget("settings:providers?provider=glm")).toEqual({ sectionId: "providers", provider: "glm" });
    expect(parseSettingsTarget("settings:providers")).toEqual({ sectionId: "providers" });
  });
  it("refuses anything that is not a settings target", () => {
    for (const t of ["profile:9", "https://x", "", undefined, "settings:", "settings:../x"]) expect(parseSettingsTarget(t)).toBeNull();
  });
});

describe("openFailureSettings", () => {
  const seen: CustomEvent[] = [];
  const on = (e: Event) => seen.push(e as CustomEvent);
  afterEach(() => {
    window.removeEventListener("smind:open-settings", on);
    seen.length = 0;
  });
  it("goes through the existing smind:open-settings event", () => {
    window.addEventListener("smind:open-settings", on);
    expect(openFailureSettings({ kind: "sign_in", target: "settings:providers?account=7", label: "Sign in" })).toBe(true);
    expect(seen).toHaveLength(1);
    expect(seen[0].detail).toEqual({ sectionId: "providers", accountId: 7 });
  });
  it("does nothing for an action that is not a settings link", () => {
    window.addEventListener("smind:open-settings", on);
    expect(openFailureSettings({ kind: "retry_now", label: "Retry" })).toBe(false);
    expect(openFailureSettings(undefined)).toBe(false);
    expect(seen).toHaveLength(0);
  });
});

describe("failureCountdown", () => {
  const act = (nextAt: string, attempt = 1, max = 2): FailureAction => ({ kind: "auto_retry", label: "x", attempt, max, nextAt });
  it("counts down to the next automatic retry", () => {
    expect(failureCountdown(act("2026-11-01T10:00:28Z"), NOW)).toBe("Retrying in 28s (1 of 2)");
    expect(failureCountdown(act("2026-11-01T10:01:30Z", 2), NOW)).toBe("Retrying in 1m 30s (2 of 2)");
  });
  it("says now once the time has come, and is null for any other action", () => {
    expect(failureCountdown(act("2026-11-01T09:59:59Z"), NOW)).toBe("Retrying now (1 of 2)");
    expect(failureCountdown({ kind: "retry_now", label: "Retry" }, NOW)).toBeNull();
    expect(failureCountdown(undefined, NOW)).toBeNull();
  });
});

describe("describeFailureCard", () => {
  const base = { failureWindow: undefined, failureResetAt: undefined, failureResetInferred: undefined };
  it("auth_expired: signed out, a Sign in control, no retry", () => {
    const v = describeFailureCard({ ...base, failureKind: "auth_expired", failureAction: { kind: "sign_in", target: "settings:providers?provider=glm", label: "Sign in again to GLM" } }, NOW);
    expect(v).toMatchObject({ tone: "danger", control: "sign_in", controlLabel: "Sign in" });
    expect(v?.message).toContain("signed out");
  });
  it("model_not_in_plan names the models the plan allows when known", () => {
    const v = describeFailureCard({ ...base, failureKind: "model_not_in_plan", failureAction: { kind: "pick_model", label: "Pick another model", models: ["sonnet", "haiku"] } }, NOW);
    expect(v).toMatchObject({ tone: "danger", control: null });
    expect(v?.message).toContain("not in your plan");
    expect(v?.hint).toContain("sonnet, haiku");
  });
  it("transient_429 retrying shows the countdown, spent offers Retry; both are warning, never a quota sentence", () => {
    const retrying = describeFailureCard({ ...base, failureKind: "transient_429", failureAction: { kind: "auto_retry", label: "x", attempt: 1, max: 2, nextAt: "2026-11-01T10:00:30Z" } }, NOW);
    expect(retrying).toMatchObject({ tone: "warning", control: null, countdown: "Retrying in 30s (1 of 2)" });
    const spent = describeFailureCard({ ...base, failureKind: "transient_429", failureAction: { kind: "retry_now", label: "Retry" } }, NOW);
    expect(spent).toMatchObject({ tone: "warning", control: "retry", controlLabel: "Retry" });
    expect(retrying?.message).not.toMatch(/quota|plan/i);
  });
  it("plan_window_exhausted and the residual are the existing rate-limit view (no card of their own)", () => {
    expect(describeFailureCard({ ...base, failureKind: "plan_window_exhausted" }, NOW)).toBeNull();
    expect(describeFailureCard({ ...base, failureKind: "rate_limited" }, NOW)).toBeNull();
    expect(describeFailureCard({ ...base, failureKind: undefined }, NOW)).toBeNull();
  });
});

