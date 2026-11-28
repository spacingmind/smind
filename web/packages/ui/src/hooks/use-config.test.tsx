import { act, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useConfigGet, useConfigKey } from "@/hooks/use-config";
import type { ConfigGetResult } from "@/lib/config-keys";
import { applyDaemonEvent } from "@/lib/query/event-bridge";
import { createQueryClient } from "@/lib/query/query-client";
import { queryKeys } from "@/lib/query-keys";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

function row(key: string, value: string) {
  return { key, value, default: "false", source: "file" as const, restart: false, writable: true };
}

/** A fake daemon whose config.get answer is mutable; every config.get call is counted. */
function configDaemon(initial: ConfigGetResult) {
  const daemon = new FakeUsageDaemon([]);
  let answer = initial;
  const base = daemon.client.call.bind(daemon.client);
  (daemon.client as { call: unknown }).call = async (method: string, params?: unknown) => {
    if (method === "config.get") {
      daemon.calls.push({ method, params });
      return answer;
    }
    return base(method, params);
  };
  return { daemon, set: (a: ConfigGetResult) => (answer = a), gets: () => daemon.calls.filter((c) => c.method === "config.get").length };
}

describe("config hooks", () => {
  it("config_get_refetches_on_config_updated", async () => {
    const d = configDaemon({ keys: [row("fleet.enabled", "false")] });
    const queryClient = createQueryClient();
    function Probe() {
      const q = useConfigGet();
      return <p data-testid="v">{q.data?.keys[0]?.value ?? "none"}</p>;
    }
    const view = renderWithDaemon(<Probe />, d.daemon, { queryClient });
    await waitFor(() => expect(view.getByTestId("v")).toHaveTextContent("false"));
    expect(d.gets()).toBe(1);

    d.set({ keys: [row("fleet.enabled", "true")] });
    const spy = vi.spyOn(queryClient, "invalidateQueries");
    act(() => applyDaemonEvent(queryClient, "config.updated", { key: "fleet.enabled" }));
    await waitFor(() => expect(d.gets()).toBe(2));
    await waitFor(() => expect(view.getByTestId("v")).toHaveTextContent("true"));
    expect(queryClient.getQueryData<ConfigGetResult>(queryKeys.configGet())?.keys[0]?.value).toBe("true");
    const keys = spy.mock.calls.map((c) => JSON.stringify(c[0]?.queryKey));
    for (const k of [queryKeys.configGet(), queryKeys.automationList(), queryKeys.pluginList(), queryKeys.learningStatus(), queryKeys.fleetHosts()]) {
      expect(keys).toContain(JSON.stringify(k));
    }
  });

  it("config_get_is_one_shared_query", async () => {
    const d = configDaemon({ keys: [row("fleet.enabled", "true"), row("plugins.enabled", "false")] });
    function A() {
      return <p data-testid="a">{useConfigKey("fleet.enabled").view?.value ?? "none"}</p>;
    }
    function B() {
      return <p data-testid="b">{useConfigKey("plugins.enabled").view?.value ?? "none"}</p>;
    }
    const view = renderWithDaemon(
      <>
        <A />
        <B />
      </>,
      d.daemon,
    );
    await waitFor(() => expect(view.getByTestId("a")).toHaveTextContent("true"));
    expect(view.getByTestId("b")).toHaveTextContent("false");
    expect(d.gets()).toBe(1);
  });
});
