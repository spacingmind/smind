import type { FleetGrant, FleetHost } from "@/lib/fleet-types";
import { FakeUsageDaemon } from "@/test/usage-test-client";

const REVERSE_DEFAULT: FleetGrant = {
  ops: ["read"],
  workspacesRead: { all: true },
  workspacesWrite: [],
  providers: [],
  maxLiveAgents: 0,
  maxWallSeconds: 0,
  maxCostUsd: 0,
};

export function fleetHost(over: Partial<FleetHost> = {}): FleetHost {
  return {
    hostId: "h1abc",
    alias: "mac",
    label: "Mac",
    state: "approved",
    pendingApproval: false,
    link: "active",
    outboundEnabled: true,
    inbound: { enabled: false, grant: { ...REVERSE_DEFAULT } },
    createdAt: "2026-10-31T10:00:00Z",
    ...over,
  };
}

/**
 * A FakeUsageDaemon that answers the fleet.host.* surface from `hosts`: list, and each mutation recorded and applied
 * the way the daemon does. With `off` set every fleet.host.* call fails like a daemon that has fleet disabled.
 */
export class FleetDaemon extends FakeUsageDaemon {
  hosts: FleetHost[] = [];
  off = false;
  /** Answers `workspace.list` (the approve dialog's write-workspace picker); their spaces and tasks are empty. */
  workspaceRows: { ID: number; Title: string }[] = [];

  constructor() {
    super([]);
    const holder = this.client as unknown as { call: (m: string, p?: unknown) => Promise<unknown> };
    const inner = holder.call;
    holder.call = async (method, params) => {
      if (method === "workspace.list") {
        this.calls.push({ method, params });
        return this.workspaceRows;
      }
      if (!method.startsWith("fleet.host.")) return inner.call(this.client, method, params);
      this.calls.push({ method, params });
      if (this.off) throw new Error("method not found: " + method);
      const p = (params ?? {}) as Record<string, unknown>;
      const patch = (fn: (h: FleetHost) => FleetHost) => {
        this.hosts = this.hosts.map((h) => (h.hostId === p.hostId ? fn(h) : h));
        return {};
      };
      switch (method) {
        case "fleet.host.list":
          return { hosts: this.hosts };
        case "fleet.host.invite":
          return { invite: "smind-host://v1#abc", expiresAt: "2026-10-31T10:10:00Z" };
        case "fleet.host.add": {
          const host = fleetHost({ hostId: "hnew", alias: (p.alias as string) ?? "new", state: "pending", pendingApproval: true, link: "connecting", sas: "123456" });
          this.hosts = [...this.hosts, host];
          return { host };
        }
        case "fleet.host.approve":
          return patch((h) => {
            const { sas: _sas, ...rest } = h;
            return { ...rest, state: "approved", pendingApproval: false };
          });
        case "fleet.host.reject":
        case "fleet.host.rm":
          this.hosts = this.hosts.filter((h) => h.hostId !== p.hostId);
          return {};
        case "fleet.host.revoke":
          return patch((h) => ({ ...h, state: "revoked", link: "revoked" }));
        case "fleet.host.enable_reverse":
          return patch((h) => ({ ...h, inbound: { ...h.inbound, enabled: true } }));
        case "fleet.host.disable_reverse":
          return patch((h) => ({ ...h, inbound: { ...h.inbound, enabled: false } }));
        case "fleet.host.rename":
          return patch((h) => ({ ...h, alias: p.alias as string }));
        case "fleet.host.grant":
          return patch((h) => ({ ...h, inbound: { ...h.inbound, grant: p.grant as FleetGrant } }));
      }
      return {};
    };
  }

  callsOf(method: string): { method: string; params: unknown }[] {
    return this.calls.filter((c) => c.method === method);
  }
}
