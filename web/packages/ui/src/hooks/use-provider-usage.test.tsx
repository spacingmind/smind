import { act, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useProviderUsage } from "@/hooks/use-provider-usage";
import type { ProviderUsageParams, ProviderUsageRow } from "@/lib/account-usage/provider-usage-types";
import { createQueryClient } from "@/lib/query/query-client";
import { RpcError } from "@/lib/ws-client";
import { claudeAccount } from "@/test/account-usage-fixtures";
import { renderHookWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

function row(instanceId: string, usedPct = 10): ProviderUsageRow {
  const base = claudeAccount();
  const windows = base.report?.windows?.map((w) => ({ ...w, usedPct })) ?? [];
  return {
    ...base,
    accountId: 0,
    key: `instance-${instanceId}`,
    provider: "zai",
    displayName: instanceId,
    report: base.report ? { ...base.report, windows } : null,
    instanceId,
  };
}

interface Scripted {
  daemon: FakeUsageDaemon;
  requests: ProviderUsageParams[];
  rows: ProviderUsageRow[];
}

/** A daemon whose `provider.usage` answers from `rows` and records each request. */
function scripted(rows: ProviderUsageRow[] = [row("glm-a"), row("glm-b")]): Scripted {
  const daemon = new FakeUsageDaemon([]);
  const s: Scripted = { daemon, requests: [], rows };
  (daemon.client as unknown as { call: (m: string, p?: unknown) => Promise<unknown> }).call = async (method, params) => {
    if (method !== "provider.usage") return null;
    const p = (params ?? {}) as ProviderUsageParams;
    s.requests.push(p);
    const list = p.instanceIds ? s.rows.filter((r) => p.instanceIds!.includes(r.instanceId)) : s.rows;
    return { enabled: true, ttlSeconds: 300, reports: list };
  };
  return s;
}

const modes = (s: Scripted) => s.requests.map((r) => r.mode);

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function active(on: boolean): void {
  vi.spyOn(document, "hasFocus").mockReturnValue(on);
  vi.spyOn(document, "visibilityState", "get").mockReturnValue(on ? "visible" : "hidden");
}

beforeEach(() => active(true));
afterEach(() => vi.restoreAllMocks());

describe("useProviderUsage", () => {
  it("reads cached once and never again on remount", async () => {
    const s = scripted();
    const queryClient = createQueryClient();
    const first = renderHookWithDaemon(() => useProviderUsage(), s.daemon, { initialProps: undefined, queryClient });
    await waitFor(() => expect(first.result.current.phase).toBe("ready"));
    expect(first.result.current.reports).toHaveLength(2);
    first.unmount();
    const second = renderHookWithDaemon(() => useProviderUsage(), s.daemon, { initialProps: undefined, queryClient });
    await flush();
    expect(second.result.current.reports).toHaveLength(2);
    expect(modes(s)).toEqual(["cached"]);
  });

  it("unknown method disables and never re-asks", async () => {
    const s = scripted();
    (s.daemon.client as unknown as { call: unknown }).call = async (m: string, p: ProviderUsageParams) => {
      if (m !== "provider.usage") return null;
      s.requests.push(p);
      throw new RpcError("method not found: provider.usage");
    };
    const queryClient = createQueryClient();
    const first = renderHookWithDaemon(() => useProviderUsage({ touch: true }), s.daemon, { initialProps: undefined, queryClient });
    await waitFor(() => expect(first.result.current.phase).toBe("ready"));
    expect(first.result.current.supports).toBe(false);
    expect(first.result.current.reports).toEqual([]);
    first.unmount();
    renderHookWithDaemon(() => useProviderUsage({ touch: true }), s.daemon, { initialProps: undefined, queryClient });
    await flush();
    expect(s.requests.map((r) => r.mode)).toEqual(["cached"]);
  });

  it("touch sends one touch per ttl even if remounted", async () => {
    const s = scripted();
    const queryClient = createQueryClient();
    const first = renderHookWithDaemon(() => useProviderUsage({ touch: true }), s.daemon, { initialProps: undefined, queryClient });
    await waitFor(() => expect(modes(s)).toEqual(["cached", "touch"]));
    first.unmount();
    renderHookWithDaemon(() => useProviderUsage({ touch: true }), s.daemon, { initialProps: undefined, queryClient });
    await flush();
    await flush();
    expect(modes(s)).toEqual(["cached", "touch"]);
  });

  it("touch is skipped when the window is hidden", async () => {
    active(false);
    const s = scripted();
    const view = renderHookWithDaemon(() => useProviderUsage({ touch: true }), s.daemon, { initialProps: undefined });
    await waitFor(() => expect(view.result.current.phase).toBe("ready"));
    await flush();
    expect(modes(s)).toEqual(["cached"]);
  });

  it("refresh sends mode refresh for that instance only and merges the row", async () => {
    const s = scripted();
    const view = renderHookWithDaemon(() => useProviderUsage(), s.daemon, { initialProps: undefined });
    await waitFor(() => expect(view.result.current.phase).toBe("ready"));
    s.rows = [row("glm-a", 10), row("glm-b", 77)];
    await act(async () => {
      await view.result.current.refresh("glm-b");
    });
    expect(s.requests.at(-1)).toEqual({ instanceIds: ["glm-b"], mode: "refresh" });
    expect(modes(s)).toEqual(["cached", "refresh"]);
    const reports = view.result.current.reports;
    expect(reports.map((r) => r.instanceId)).toEqual(["glm-a", "glm-b"]);
    expect(reports[1].report?.windows?.[0].usedPct).toBe(77);
    expect(reports[0].report?.windows?.[0].usedPct).toBe(10);
  });

  it("refresh within 30s of the last press sends nothing", async () => {
    const s = scripted();
    const view = renderHookWithDaemon(() => useProviderUsage(), s.daemon, { initialProps: undefined });
    await waitFor(() => expect(view.result.current.phase).toBe("ready"));
    await act(async () => {
      await view.result.current.refresh("glm-a");
    });
    await act(async () => {
      await view.result.current.refresh("glm-a");
    });
    expect(modes(s)).toEqual(["cached", "refresh"]);
    expect(view.result.current.pressedAt["glm-a"]).toBeGreaterThan(0);
  });
});
