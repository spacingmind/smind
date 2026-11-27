import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { HostStep } from "@/components/add-project/host-step";
import { FleetDaemon, fleetHost } from "@/test/fleet-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";
import type { Connection } from "@/lib/platform";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 10; i++) await Promise.resolve();
  });
}

// Mutable per case: the desktop build flag, the app-shell connection state the
// mocked `desktop` answers with, and every selectConnection it was asked for
// (settings-dialog.test.tsx mocks the platform module the same way so
// `isDesktop` can flip between the web and desktop builds inside one file).
const platformState = vi.hoisted(() => ({
  desktop: false,
  current: { id: "conn-current", kind: "local", label: "This Mac", baseUrl: "http://127.0.0.1:7700" } as Connection,
  saved: [] as Connection[],
  selected: [] as string[],
}));

vi.mock("@/lib/platform", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/platform")>();
  const mocked = Object.create(null) as typeof actual;
  for (const key of Object.keys(actual) as Array<keyof typeof actual>) {
    Object.defineProperty(mocked, key, {
      enumerable: true,
      get: () => {
        if (key === "isDesktop") return platformState.desktop;
        if (key === "desktop") {
          return {
            ...actual.desktop,
            getCurrentConnection: () => Promise.resolve(platformState.current),
            listConnections: () => Promise.resolve([platformState.current, ...platformState.saved]),
            selectConnection: (id: string) => {
              platformState.selected.push(id);
              return Promise.resolve(platformState.current);
            },
          };
        }
        return actual[key];
      },
    });
  }
  return mocked;
});

function resetPlatform() {
  platformState.desktop = false;
  platformState.current = { id: "conn-current", kind: "local", label: "This Mac", baseUrl: "http://127.0.0.1:7700" };
  platformState.saved = [];
  platformState.selected = [];
}

describe("Add project host step", () => {
  beforeEach(resetPlatform);

  it("add_project_host_chip_shows_the_current_connection", async () => {
    platformState.desktop = true;
    platformState.current = { id: "conn-current", kind: "local", label: "This Mac", baseUrl: "http://127.0.0.1:7700" };
    const d = new FleetDaemon();
    renderWithDaemon(<HostStep />, d);
    await flush();

    expect(screen.getByTestId("add-project-host-chip")).toHaveTextContent("On: This Mac");
    expect(screen.queryByTestId("add-project-hosts")).toBeNull();
  });

  it("add_project_one_host_shows_only_the_chip", async () => {
    // Web build: no saved connections, no approved fleet hosts.
    const d = new FleetDaemon();
    renderWithDaemon(<HostStep />, d);
    await flush();

    expect(screen.getByTestId("add-project-host-chip")).toHaveTextContent(`On: ${window.location.host}`);
    expect(screen.queryByTestId("add-project-hosts")).toBeNull();
  });

  it("add_project_fleet_rows_are_disabled_with_the_reason", async () => {
    const d = new FleetDaemon();
    d.hosts = [
      fleetHost({ hostId: "h1abc", alias: "mac", label: "Mac", state: "approved" }),
      fleetHost({ hostId: "h2nas", alias: "", label: "Nas", state: "approved" }),
      fleetHost({ hostId: "h3pen", alias: "pen", label: "Pending", state: "pending" }),
      fleetHost({ hostId: "h4rev", alias: "rev", label: "Revoked", state: "revoked" }),
    ];
    renderWithDaemon(<HostStep />, d);
    await flush();

    expect(screen.queryByTestId("add-project-host-chip")).toBeNull();
    expect(screen.getByTestId("add-project-hosts")).toBeInTheDocument();
    const mac = screen.getByTestId("add-project-host-fleet-h1abc");
    const nas = screen.getByTestId("add-project-host-fleet-h2nas");
    expect(mac).toHaveTextContent("mac");
    expect(mac).toHaveTextContent("Not available yet");
    expect(mac).toHaveAttribute("aria-disabled", "true");
    // No alias: the host's own label is the name.
    expect(nas).toHaveTextContent("Nas");
    expect(nas).toHaveTextContent("Not available yet");
    // Pending and revoked hosts are never listed.
    expect(screen.queryByTestId("add-project-host-fleet-h3pen")).toBeNull();
    expect(screen.queryByTestId("add-project-host-fleet-h4rev")).toBeNull();
    // Plain rows, never buttons.
    expect(mac.tagName).toBe("DIV");
    expect(mac.querySelector("button")).toBeNull();

    await act(async () => {
      fireEvent.click(mac);
      fireEvent.click(nas);
    });
    // Clicking a fleet row sends nothing: the only client traffic is the
    // reads the harness itself makes (the events subscription and the host
    // list the step asked for).
    const reads = new Set(["fleet.host.list", "events.subscribe"]);
    expect(d.calls.filter((c) => !reads.has(c.method))).toEqual([]);
  });

  it("add_project_saved_connections_are_listed_not_switchable", async () => {
    platformState.desktop = true;
    platformState.saved = [
      { id: "conn-office", kind: "url", label: "Office daemon", baseUrl: "http://office:7700" },
      { id: "conn-basement", kind: "ssh", label: "Basement server", baseUrl: "http://basement:7700" },
    ];
    const d = new FleetDaemon();
    renderWithDaemon(<HostStep />, d);
    await flush();

    expect(screen.queryByTestId("add-project-host-chip")).toBeNull();
    const office = screen.getByTestId("add-project-host-saved-conn-office");
    const basement = screen.getByTestId("add-project-host-saved-conn-basement");
    expect(office).toHaveTextContent("Office daemon");
    expect(office).toHaveTextContent("Switch the connection in Settings → Connections");
    expect(basement).toHaveTextContent("Basement server");
    expect(basement).toHaveTextContent("Switch the connection in Settings → Connections");
    // The current connection is the selected row, not one of the saved ones.
    expect(screen.getByTestId("add-project-host-current")).toHaveTextContent("This Mac");
    expect(screen.getByTestId("add-project-host-current")).toHaveTextContent("This connection");
    // Plain rows, never buttons.
    expect(office.tagName).toBe("DIV");
    expect(office.querySelector("button")).toBeNull();

    await act(async () => {
      fireEvent.click(office);
      fireEvent.click(basement);
    });
    // Listing a saved connection never switches to it from inside the dialog.
    expect(platformState.selected).toEqual([]);
  });
});
