import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { useDefaultRunPreferences } from "@/hooks/use-default-run-preferences";

afterEach(() => {
  window.localStorage.clear();
});

/**
 * The composer's other consumer of this hook (task-detail.tsx, Track B)
 * is outside this track's ownership -- these tests cover the hook's own
 * contract: it reads what settings-preferences.ts persisted, and setting
 * a value here is what the next mount (e.g. a newly opened composer)
 * would read.
 */
describe("useDefaultRunPreferences", () => {
  it("starts at no preference (null) for both fields", () => {
    const { result } = renderHook(() => useDefaultRunPreferences());
    expect(result.current.defaultProvider).toBeNull();
    expect(result.current.defaultApprovalPolicy).toBeNull();
  });

  it("setting a value updates state and persists it for the next mount", () => {
    const { result } = renderHook(() => useDefaultRunPreferences());

    act(() => result.current.setDefaultProvider("glm"));
    act(() => result.current.setDefaultApprovalPolicy("auto-safe"));

    expect(result.current.defaultProvider).toBe("glm");
    expect(result.current.defaultApprovalPolicy).toBe("auto-safe");

    const { result: second } = renderHook(() => useDefaultRunPreferences());
    expect(second.current.defaultProvider).toBe("glm");
    expect(second.current.defaultApprovalPolicy).toBe("auto-safe");
  });

  it("setting back to null clears the preference for the next mount too", () => {
    const { result } = renderHook(() => useDefaultRunPreferences());
    act(() => result.current.setDefaultProvider("glm"));
    act(() => result.current.setDefaultProvider(null));

    const { result: second } = renderHook(() => useDefaultRunPreferences());
    expect(second.current.defaultProvider).toBeNull();
  });
});
