import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/hosts-section";
import { FleetDaemon, fleetHost } from "@/test/fleet-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

async function click(el: HTMLElement): Promise<void> {
  fireEvent.click(el);
  await flush();
}

async function renderSection(daemon: FleetDaemon) {
  const section = listSettingsSections().find((s) => s.id === "hosts");
  if (!section) throw new Error("hosts section did not register");
  const view = renderWithDaemon(<>{section.render({ client: daemon.client, events: daemon.events })}</>, daemon);
  await flush();
  return view;
}

describe("hosts page polish", () => {
  it("TestHostsPage_RowCards", async () => {
    const d = new FleetDaemon();
    d.hosts = [
      fleetHost(),
      fleetHost({ hostId: "h4nas", alias: "nas", label: "NAS", state: "pending", link: "connecting", sas: "123456" }),
    ];
    await renderSection(d);

    for (const alias of ["mac", "nas"]) {
      const row = screen.getByTestId("host-row-" + alias);
      expect(row.querySelectorAll('[data-slot="settings-group"]')).toHaveLength(1);
      expect(row.querySelectorAll('[data-slot="settings-row"]').length).toBeGreaterThanOrEqual(3);
      expect(screen.getByTestId("host-header-" + alias)).toBeInTheDocument();
    }
  });

  it("TestHostsPage_DirectionLabels", async () => {
    const d = new FleetDaemon();
    d.hosts = [
      fleetHost({ inbound: { enabled: true, grant: fleetHost().inbound.grant } }),
      fleetHost({
        hostId: "h4nas",
        alias: "nas",
        label: "NAS",
        state: "pending",
        link: "connecting",
        sas: "123456",
        outboundEnabled: false,
      }),
    ];
    await renderSection(d);

    expect(screen.getByTestId("host-dir-out-mac")).toHaveTextContent("This host → mac");
    expect(screen.getByTestId("host-dir-in-mac")).toHaveTextContent("mac → this host");
    expect(screen.getByTestId("host-out-state-mac")).toHaveTextContent("On");
    expect(screen.getByTestId("host-inbound-mac")).toHaveAttribute("role", "switch");
    expect(screen.getByTestId("host-out-state-nas")).toHaveTextContent("Off");
    expect(screen.getByTestId("host-in-state-nas")).toHaveTextContent("Off");
    expect(screen.queryByTestId("host-inbound-nas")).toBeNull();
    expect(document.body.textContent).not.toContain("You can use");
    expect(document.body.textContent).not.toContain("can use this host");
  });

  it("TestHostsPage_GrantChipsNoWrap", async () => {
    const d = new FleetDaemon();
    d.hosts = [
      fleetHost({
        inbound: {
          enabled: true,
          grant: {
            ops: ["read", "create"],
            workspacesRead: { all: true },
            workspacesWrite: [7],
            providers: [],
            maxLiveAgents: 2,
            maxWallSeconds: 7200,
            maxCostUsd: 5,
          },
        },
      }),
    ];
    await renderSection(d);

    const chips = Array.from(screen.getByTestId("host-grant-mac").children);
    expect(chips).toHaveLength(4);
    for (const chip of chips) {
      expect(chip.className).toContain("whitespace-nowrap");
      expect(chip.textContent).not.toContain(" ");
    }
    // the words of the cost chip are held together by non-breaking spaces, never a plain one
    expect(chips.map((c) => c.textContent)).toContain("$5.00\u00a0per\u00a024\u00a0h");
  });

  it("TestHostsPage_SasCopy", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });

    const d = new FleetDaemon();
    d.hosts = [
      fleetHost({ hostId: "h4nas", alias: "nas", label: "NAS", state: "pending", pendingApproval: false, link: "connecting", sas: "123456" }),
    ];
    await renderSection(d);

    expect(screen.getByTestId("host-sas-code-nas")).toHaveTextContent("123 456");
    expect(screen.getByTestId("host-sas-nas")).toHaveTextContent("Confirm this code on nas");
    const copy = screen.getByTestId("host-sas-copy-nas");
    await click(copy);
    expect(writeText).toHaveBeenCalledWith("123456");
    await waitFor(() => expect(copy).toHaveAttribute("aria-label", "Copied"));
  });
});
