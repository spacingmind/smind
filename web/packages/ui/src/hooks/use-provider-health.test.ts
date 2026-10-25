import { act } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useProviderHealth } from "@/hooks/use-provider-health";
import type { ProviderInfo } from "@/lib/types";
import { renderHookWithDaemon } from "@/test/render-with-daemon";
import { queryKeys } from "@/lib/query-keys";
import { createQueryClient } from "@/lib/query/query-client";
import { FakeUsageDaemon } from "@/test/usage-test-client";

const PROVIDERS: ProviderInfo[] = [
  { id: "claude-native", accountProvider: "anthropic" },
  { id: "glm", kind: "cli" },
];

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

describe("useProviderHealth", () => {
  it("tests nothing until ensure is called, then each provider once per client", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.providers = { providers: PROVIDERS };
    const { result } = renderHookWithDaemon(() => useProviderHealth(), daemon, { initialProps: undefined });
    await flush();
    expect(daemon.countCalls("provider.test")).toBe(0);

    act(() => result.current.ensure());
    act(() => result.current.ensure());
    // claude-native by its own id (run readiness) and by its account id (the Providers rows' credential check), glm once.
    expect(daemon.countCalls("provider.test")).toBe(3);
    expect(daemon.calls.filter((c) => c.method === "provider.test").map((c) => (c.params as { provider: string }).provider)).toEqual([
      "claude-native",
      "anthropic",
      "glm",
    ]);

    await flush();
    expect(result.current.health["claude-native"]).toEqual({ ok: true, detail: "ok" });
    expect(result.current.health.glm).toEqual({ ok: true, detail: "ok" });
    expect(result.current.summary).toEqual({ healthy: 2, total: 2, tested: 2 });

    act(() => result.current.ensure());
    expect(daemon.countCalls("provider.test")).toBe(3);
  });

  it("asks about a provider by its own id, never its account id (claude-native runs on the CLI's login)", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.providers = { providers: [{ id: "claude-native", accountProvider: "anthropic" }] };
    // No anthropic account exists; the CLI is on PATH. Run readiness must be ok.
    daemon.testResults = { anthropic: { ok: false, detail: 'no account configured for "anthropic"' }, "claude-native": { ok: true, detail: "found /usr/local/bin/claude" } };
    const { result } = renderHookWithDaemon(() => useProviderHealth(), daemon, { initialProps: undefined });
    await flush();
    act(() => result.current.ensure());
    await flush();
    expect(result.current.health["claude-native"]).toEqual({ ok: true, detail: "found /usr/local/bin/claude" });
    expect(result.current.health["claude-native"]?.ok).toBe(true);
    // The account id is asked too, but only to fill the Providers rows: it never feeds a provider's health.
    expect(daemon.calls.filter((c) => c.method === "provider.test").map((c) => (c.params as { provider: string }).provider)).toEqual(["claude-native", "anthropic"]);
  });

  it("a failed provider.test leaves the provider untested and retries on the next open", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.providers = { providers: [{ id: "claude-native" }] };
    daemon.testResults = { "claude-native": { ok: false, detail: "not found" } };
    // A failed *RPC* (not a not-ok result): reject the call outright.
    daemon.failNonUsageWith = new Error("boom");
    const seeded = createQueryClient();
    seeded.setQueryData(queryKeys.providerList(), { providers: [{ id: "claude-native" }] });
    const { result } = renderHookWithDaemon(() => useProviderHealth(), daemon, { initialProps: undefined, queryClient: seeded });
    await flush();

    act(() => result.current.ensure());
    await flush();
    expect(result.current.health["claude-native"]).toBeUndefined();
    // For a picker it is still untested (never disabled on a guess); for a count it is "tested, not healthy".
    expect(result.current.rpcFailed.has("claude-native")).toBe(true);
    expect(result.current.summary).toEqual({ healthy: 0, total: 1, tested: 1 });

    daemon.failNonUsageWith = null;
    act(() => result.current.ensure());
    expect(daemon.countCalls("provider.test")).toBe(2);
    await flush();
    expect(result.current.health["claude-native"]).toEqual({ ok: false, detail: "not found" });
    expect(result.current.rpcFailed.has("claude-native")).toBe(false);
  });

  it("an invalidated answer is re-tested by the next ensure; a fresh one is not", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.providers = { providers: [{ id: "claude-native" }] };
    const seeded = createQueryClient();
    seeded.setQueryData(queryKeys.providerList(), { providers: [{ id: "claude-native" }] });
    const { result } = renderHookWithDaemon(() => useProviderHealth(), daemon, { initialProps: undefined, queryClient: seeded });
    await flush();
    act(() => result.current.ensure());
    await flush();
    act(() => result.current.ensure());
    expect(daemon.countCalls("provider.test")).toBe(1);

    await act(async () => void seeded.invalidateQueries({ queryKey: queryKeys.providerTest("claude-native") }));
    act(() => result.current.ensure());
    expect(daemon.countCalls("provider.test")).toBe(2);
  });

  it("never tests the fallback ids while provider.list is still loading, only once it failed", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.failNonUsageWith = null;
    let release: () => void = () => {};
    const gate = new Promise<void>((r) => (release = r));
    const call = daemon.client.call.bind(daemon.client);
    (daemon.client as { call: unknown }).call = async (method: string, params?: unknown) => {
      if (method === "provider.list") {
        await gate;
        throw new Error("provider.list down");
      }
      return call(method, params);
    };
    const { result } = renderHookWithDaemon(() => useProviderHealth(), daemon, { initialProps: undefined });
    await flush();
    act(() => result.current.ensure());
    expect(daemon.countCalls("provider.test")).toBe(0);

    release();
    await flush();
    act(() => result.current.ensure());
    // The list failed, so the picker's fallback catalog (the four built-ins) is what gets tested.
    expect(daemon.countCalls("provider.test")).toBe(4);
  });

  it("tests only the newly listed providers on a later open", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.providers = { providers: [{ id: "claude-native", accountProvider: "anthropic" }] };
    const { result, queryClient, rerender } = renderHookWithDaemon(() => useProviderHealth(), daemon, { initialProps: undefined });
    await flush();
    act(() => result.current.ensure());
    await flush();
    // provider.list answering after the first open (the cache keeps the old list until invalidated)
    daemon.providers = { providers: PROVIDERS };
    await act(async () => {
      void queryClient().invalidateQueries({ queryKey: queryKeys.providerList() });
    });
    rerender({});
    await flush();
    act(() => result.current.ensure());
    expect(daemon.calls.filter((c) => c.method === "provider.test").map((c) => (c.params as { provider: string }).provider)).toEqual([
      "claude-native",
      "anthropic",
      "glm",
    ]);
  });

  it("counts only ok results in the summary", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.providers = { providers: PROVIDERS };
    daemon.testResults = { "claude-native": { ok: false, detail: "claude not found on PATH" } };
    const { result } = renderHookWithDaemon(() => useProviderHealth(), daemon, { initialProps: undefined });
    await flush();
    act(() => result.current.ensure());
    await flush();
    expect(result.current.summary).toEqual({ healthy: 1, total: 2, tested: 2 });
  });
});
