import { act } from "react";
import { describe, expect, it } from "vitest";

import { useFleetHostActions, useFleetHosts } from "@/hooks/use-fleet-hosts";
import { FleetDaemon, fleetHost } from "@/test/fleet-fixtures";
import { renderHookWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 10; i++) await Promise.resolve();
  });
}

describe("fleet host hooks", () => {
  it("reads fleet.host.list once and shares it", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost()];
    const { result } = renderHookWithDaemon(() => ({ a: useFleetHosts(), b: useFleetHosts() }), d, { initialProps: null });
    await flush();
    expect(result.current.a.data).toEqual({ off: false, hosts: [fleetHost()] });
    expect(d.callsOf("fleet.host.list")).toHaveLength(1);
  });

  it("reads fleet off when the daemon has no fleet methods", async () => {
    const d = new FleetDaemon();
    d.off = true;
    const { result } = renderHookWithDaemon(() => useFleetHosts(), d, { initialProps: null });
    await flush();
    expect(result.current.data).toEqual({ off: true, hosts: [] });
  });

  it("approve sends the typed sas and the grant", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost({ state: "pending", pendingApproval: true, sas: "123456" })];
    const { result } = renderHookWithDaemon(() => useFleetHostActions(), d, { initialProps: null });
    const grant = fleetHost().inbound.grant;
    await act(async () => void (await result.current.approve("h1abc", "123 456", grant)));
    expect(d.callsOf("fleet.host.approve")[0].params).toEqual({ hostId: "h1abc", sas: "123456", grant });
  });

  it("an action refetches the host list", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost()];
    const { result } = renderHookWithDaemon(() => ({ l: useFleetHosts(), a: useFleetHostActions() }), d, { initialProps: null });
    await flush();
    await act(async () => void (await result.current.a.revoke("h1abc")));
    await flush();
    expect(d.callsOf("fleet.host.list")).toHaveLength(2);
    expect(result.current.l.data?.hosts[0].state).toBe("revoked");
  });

  it("refetches on fleet.hosts.changed", async () => {
    const d = new FleetDaemon();
    renderHookWithDaemon(() => useFleetHosts(), d, { initialProps: null });
    await flush();
    d.hosts = [fleetHost()];
    await act(async () => d.emit("fleet.hosts.changed", {}));
    await flush();
    expect(d.callsOf("fleet.host.list")).toHaveLength(2);
  });
});
