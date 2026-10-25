import { act } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import type { ProviderInfo } from "@/lib/types";
import { renderHookWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";
import { useProviderFamilies } from "@/stores/provider-families-store";

const SERVED: ProviderInfo[] = [
  { id: "claude-native", label: "Claude Code", runner: "claude-native" },
  { id: "claude-work", label: "Claude work", runner: "claude-native", extends: "claude-native", custom: true },
  { id: "my-acp", label: "My ACP agent", runner: "acp", custom: true },
];

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

describe("useProviderCatalog (single source + offline fallback)", () => {
  it("serves exactly provider.list once it answered: instances and custom ids in, no fallback entry merged", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.providers = { providers: SERVED };
    const { result } = renderHookWithDaemon(() => useProviderCatalog(), daemon, { initialProps: undefined });
    await flush();
    expect(result.current.loaded).toBe(true);
    expect(result.current.providers.map((p) => p.id)).toEqual(["claude-native", "claude-work", "my-acp"]);
    // The catalog is the single source: the built-ins the daemon did not list (codex, glm, kimi) are NOT back-filled.
    expect(result.current.providersOrFallback).toBe(result.current.providers);
    expect(daemon.countCalls("provider.list")).toBe(1);
  });

  it("uses the four-built-in fallback only when provider.list failed, and never lists it as loaded", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.failNonUsageWith = new Error("daemon down");
    const { result } = renderHookWithDaemon(() => useProviderCatalog(), daemon, { initialProps: undefined });
    await flush();
    expect(result.current.loaded).toBe(false);
    expect(result.current.providers).toEqual([]);
    expect(result.current.providersOrFallback.map((p) => p.id)).toEqual(["claude-native", "codex-native", "glm", "kimi"]);
  });

  it("tells the icon store which built-in an instance extends", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.providers = { providers: SERVED };
    renderHookWithDaemon(() => useProviderCatalog(), daemon, { initialProps: undefined });
    await flush();
    expect(useProviderFamilies.getState().extendsById).toEqual({ "claude-work": "claude-native" });
    useProviderFamilies.getState().setFromCatalog([]);
  });
});
