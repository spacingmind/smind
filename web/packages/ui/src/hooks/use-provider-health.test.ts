import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useProviderHealth } from "@/hooks/use-provider-health";
import type { ProviderInfo } from "@/lib/types";
import { FakeWsClient } from "@/test/fake-ws-client";

const PROVIDERS: ProviderInfo[] = [{ id: "claude-native" }, { id: "glm", kind: "cli" }];

async function flush() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("useProviderHealth", () => {
  it("tests nothing until ensure is called, then each provider once per client", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useProviderHealth(client, PROVIDERS));
    expect(client.calls).toHaveLength(0);

    act(() => result.current.ensure());
    act(() => result.current.ensure());
    expect(client.calls.filter((c) => c.method === "provider.test")).toHaveLength(2);

    client.nth("provider.test", 0).resolve({ ok: true, detail: "found /usr/bin/claude" });
    client.nth("provider.test", 1).resolve({ ok: false, detail: '"glm" not found on PATH' });
    await flush();
    expect(result.current.health["claude-native"]).toEqual({ ok: true, detail: "found /usr/bin/claude" });
    expect(result.current.health.glm).toEqual({ ok: false, detail: '"glm" not found on PATH' });

    act(() => result.current.ensure());
    expect(client.calls.filter((c) => c.method === "provider.test")).toHaveLength(2);
  });

  it("a failed provider.test leaves the provider untested and retries on the next open", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useProviderHealth(client, [{ id: "claude-native" }]));

    act(() => result.current.ensure());
    client.nth("provider.test", 0).reject(new Error("boom"));
    await flush();
    expect(result.current.health["claude-native"]).toBeUndefined();

    act(() => result.current.ensure());
    expect(client.calls.filter((c) => c.method === "provider.test")).toHaveLength(2);
  });

  it("tests only the newly listed providers on a later open", () => {
    const client = new FakeWsClient();
    const { result, rerender } = renderHook(({ list }) => useProviderHealth(client, list), {
      initialProps: { list: [{ id: "claude-native" }] as ProviderInfo[] },
    });
    act(() => result.current.ensure());
    rerender({ list: PROVIDERS });
    act(() => result.current.ensure());
    expect(client.calls.map((c) => (c.params as { provider: string }).provider)).toEqual(["claude-native", "glm"]);
  });
});
