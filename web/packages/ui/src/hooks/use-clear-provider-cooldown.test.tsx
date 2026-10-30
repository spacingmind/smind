import { act, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { activeCooldownUntil, useClearProviderCooldown } from "@/hooks/use-clear-provider-cooldown";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { FakeUsageDaemon } from "@/test/usage-test-client";
import { renderHookWithDaemon } from "@/test/render-with-daemon";
import type { ProviderInfo } from "@/lib/types";

// The catalog keeps provider.list an active query, so the hook's success invalidation re-reads it (counted by the fake).
function probe(): { clear: ReturnType<typeof useClearProviderCooldown>; loaded: boolean } {
  const clear = useClearProviderCooldown();
  const catalog = useProviderCatalog();
  return { clear, loaded: catalog.loaded };
}

function setup(providers: ProviderInfo[] = []): { daemon: FakeUsageDaemon; clear: () => Promise<void> } {
  const d = new FakeUsageDaemon([]);
  d.providers = { providers };
  const view = renderHookWithDaemon((_: unknown) => probe(), d, { initialProps: undefined });
  return { daemon: d, clear: () => view.result.current.clear.clear("glm") };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function clearCalls(d: FakeUsageDaemon): unknown[] {
  return d.calls.filter((c) => c.method === "provider.clearCooldown").map((c) => c.params);
}

describe("useClearProviderCooldown", () => {
  it("clear sends provider.clearCooldown {provider} and invalidates provider.list", async () => {
    const { daemon, clear } = setup([{ id: "glm", label: "GLM" }]);
    await flush();
    expect(daemon.countCalls("provider.list")).toBe(1);
    await act(async () => {
      await clear();
    });
    expect(clearCalls(daemon)).toEqual([{ provider: "glm" }]);
    // The daemon publishes provider.updated after a clear; the invalidation is what re-reads the list here.
    await waitFor(() => expect(daemon.countCalls("provider.list")).toBe(2));
  });

  it("surfaces the daemon's error and leaves the list alone", async () => {
    const { daemon, clear } = setup([{ id: "glm", label: "GLM" }]);
    await flush();
    daemon.failNonUsageWith = new Error("unknown provider glm");
    await expect(act(() => clear())).rejects.toThrow("unknown provider glm");
    expect(clearCalls(daemon)).toEqual([{ provider: "glm" }]);
    expect(daemon.countCalls("provider.list")).toBe(1);
  });
});

describe("activeCooldownUntil", () => {
  it("is null for an absent, past, or unparsable value; the Date when the cooldown is still ahead", () => {
    const now = new Date("2026-10-30T12:00:00Z");
    expect(activeCooldownUntil(undefined, now)).toBeNull();
    expect(activeCooldownUntil({ id: "glm" }, now)).toBeNull();
    expect(activeCooldownUntil({ id: "glm", cooldownUntil: "2026-10-30T11:59:59Z" }, now)).toBeNull();
    expect(activeCooldownUntil({ id: "glm", cooldownUntil: "2026-10-30T12:00:00Z" }, now)).toBeNull();
    expect(activeCooldownUntil({ id: "glm", cooldownUntil: "not-a-time" }, now)).toBeNull();
    expect(activeCooldownUntil({ id: "glm", cooldownUntil: "2026-10-30T15:00:00Z" }, now)).toEqual(new Date("2026-10-30T15:00:00Z"));
  });
});
