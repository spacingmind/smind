import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { shortSummary } from "@/components/hosts/host-activity";
import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/hosts-section";
import type { FleetAuditEntry } from "@/lib/fleet-types";
import { FleetDaemon, fleetHost } from "@/test/fleet-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

async function renderSection(daemon: FleetDaemon) {
  const section = listSettingsSections().find((s) => s.id === "hosts");
  if (!section) throw new Error("hosts section did not register");
  const view = renderWithDaemon(<>{section.render({ client: daemon.client, events: daemon.events })}</>, daemon);
  await flush();
  return view;
}

function entry(over: Partial<FleetAuditEntry> = {}): FleetAuditEntry {
  return {
    id: 1,
    time: new Date(Date.now() - 5 * 60_000).toISOString(),
    direction: "in",
    hostId: "h1abc",
    peer: "mac",
    method: "fleet.task.list",
    decision: "ok",
    summary: "sha256=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef {}",
    latencyMs: 3,
    ...over,
  };
}

async function open(alias: string): Promise<void> {
  fireEvent.click(screen.getByTestId(`host-activity-toggle-${alias}`));
  await flush();
}

describe("host Activity row", () => {
  it("activity_closed_by_default_no_fetch", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost()];
    d.audit = [entry()];
    await renderSection(d);
    expect(screen.getByTestId("host-activity-toggle-mac")).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByTestId("host-activity-panel-mac")).toBeNull();
    expect(d.callsOf("fleet.host.audit")).toHaveLength(0);
  });

  it("activity_lists_rows_for_that_host_only", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost(), fleetHost({ hostId: "h2", alias: "linux" })];
    d.audit = [
      entry({ id: 3, method: "fleet.task.send", decision: "refused", code: "not_granted" }),
      entry({ id: 2, hostId: "h2", method: "fleet.info" }),
      entry({ id: 1, direction: "out", method: "fleet.workspace.list" }),
    ];
    await renderSection(d);
    await open("mac");

    const calls = d.callsOf("fleet.host.audit");
    expect(calls).toHaveLength(1);
    expect(calls[0].params).toEqual({ hostId: "h1abc", limit: 20 });
    expect(screen.getByTestId("host-activity-row-3")).toHaveTextContent("fleet.task.send");
    expect(screen.getByTestId("host-activity-decision-3")).toHaveTextContent("refused · not_granted");
    expect(screen.getByTestId("host-activity-dir-3")).toHaveTextContent("In");
    expect(screen.getByTestId("host-activity-dir-1")).toHaveTextContent("Out");
    expect(screen.getByTestId("host-activity-row-3")).toHaveTextContent("5m ago");
    expect(screen.queryByTestId("host-activity-row-2")).toBeNull();
    // The other host's row stays closed and unfetched.
    expect(screen.getByTestId("host-activity-toggle-linux")).toHaveAttribute("aria-expanded", "false");
  });

  it("activity_empty_error_and_refresh", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost()];
    d.auditError = "boom";
    await renderSection(d);
    await open("mac");
    expect(screen.getByTestId("host-activity-error-mac")).toHaveTextContent("boom");

    d.auditError = null;
    fireEvent.click(screen.getByTestId("host-activity-refresh-mac"));
    await flush();
    expect(screen.getByTestId("host-activity-empty-mac")).toHaveTextContent("No activity with mac yet.");

    d.audit = [entry({ id: 9 })];
    fireEvent.click(screen.getByTestId("host-activity-refresh-mac"));
    await flush();
    expect(screen.getByTestId("host-activity-row-9")).toBeInTheDocument();
  });

  it("activity_shortens_the_hash_and_renders_text_not_markup", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost()];
    d.audit = [entry({ id: 5, summary: 'sha256=aabbccddeeff00112233445566778899 {"prompt":"<9 chars>","x":"<img src=x onerror=alert(1)>"}' })];
    await renderSection(d);
    await open("mac");
    const row = screen.getByTestId("host-activity-summary-5");
    expect(row.textContent).toContain("sha256=aabbccddeeff {");
    expect(row.textContent).not.toContain("00112233445566778899");
    expect(row.textContent).toContain("<9 chars>");
    expect(row.querySelector("img")).toBeNull();
    expect(document.body.querySelector("img[src='x']")).toBeNull();
  });

  it("activity_is_offered_for_approved_and_revoked_hosts_only", async () => {
    const d = new FleetDaemon();
    d.hosts = [
      fleetHost(),
      fleetHost({ hostId: "h2", alias: "gone", state: "revoked", link: "revoked" }),
      fleetHost({ hostId: "h3", alias: "wait", state: "pending", link: "connecting", sas: "123456" }),
    ];
    await renderSection(d);
    expect(screen.getByTestId("host-activity-toggle-mac")).toBeInTheDocument();
    expect(screen.getByTestId("host-activity-toggle-gone")).toBeInTheDocument();
    expect(screen.queryByTestId("host-activity-toggle-wait")).toBeNull();
  });

  it("shortSummary keeps a non-hash summary as is", () => {
    expect(shortSummary("plain text")).toBe("plain text");
    expect(shortSummary("sha256=0123456789abcdef ")).toBe("sha256=0123456789ab");
  });
});
