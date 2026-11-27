import { act } from "react";
import { describe, expect, it, vi } from "vitest";

import { useRelayDeviceActions, useRelayDevices } from "@/hooks/use-relay-devices";
import type { RelayDevice } from "@/lib/relay-devices";
import { FakeUsageDaemon } from "@/test/usage-test-client";
import { renderHookWithDaemon } from "@/test/render-with-daemon";

function device(over: Partial<RelayDevice> = {}): RelayDevice {
  return {
    device_id: "devpending01",
    device_pub: "never-rendered",
    label: "Pixel",
    first_seen: "2026-11-20T10:00:00Z",
    last_seen: "2026-11-27T09:00:00Z",
    state: "pending",
    connected: false,
    ...over,
  };
}

/** A fake daemon answering the relay.device.* surface from a mutable device list; every relay call is recorded. */
function relayDaemon(devices: RelayDevice[]) {
  const d = new FakeUsageDaemon([]);
  const holder = d.client as unknown as { call: (m: string, p?: unknown) => Promise<unknown> };
  const inner = holder.call.bind(holder);
  holder.call = async (method, params) => {
    if (!method.startsWith("relay.device.")) return inner(method, params);
    d.calls.push({ method, params });
    const p = (params ?? {}) as { id?: string };
    if (method === "relay.device.list") return { devices, window: null };
    if (method === "relay.device.approve" && p.id !== undefined) {
      const i = devices.findIndex((x) => x.device_id === p.id);
      devices[i] = { ...devices[i]!, state: "approved" };
      return devices[i]!;
    }
    return {};
  };
  return d;
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 10; i++) await Promise.resolve();
  });
}

describe("relay device hooks", () => {
  it("relay_devices_one_list_call_on_mount_and_after_a_change", async () => {
    vi.useFakeTimers();
    try {
      const d = relayDaemon([device()]);
      const { result } = renderHookWithDaemon(
        () => ({ list: useRelayDevices(true), actions: useRelayDeviceActions() }),
        d,
        { initialProps: null },
      );
      await flush();
      expect(d.calls.filter((c) => c.method === "relay.device.list")).toHaveLength(1);

      // No refetchInterval, no clock-driven re-read: time passing adds no second call.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(100);
      });
      expect(d.calls.filter((c) => c.method === "relay.device.list")).toHaveLength(1);

      await act(async () => {
        await result.current.actions.approve.mutateAsync("devpending01");
      });
      await flush();
      expect(d.calls.filter((c) => c.method === "relay.device.approve")).toHaveLength(1);
      expect(d.calls.filter((c) => c.method === "relay.device.approve")[0]!.params).toEqual({ id: "devpending01" });
      // Exactly one re-read after the change, carrying the daemon's new state.
      expect(d.calls.filter((c) => c.method === "relay.device.list")).toHaveLength(2);
      expect(result.current.list.data?.devices[0]?.state).toBe("approved");
    } finally {
      vi.useRealTimers();
    }
  });
});
