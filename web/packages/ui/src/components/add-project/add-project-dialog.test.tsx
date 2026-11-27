import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { HostStep } from "@/components/add-project/host-step";
import { AddProjectDialog } from "@/components/add-project/add-project-dialog";
import { FleetDaemon, fleetHost } from "@/test/fleet-fixtures";
import { renderWithClientProp, renderWithDaemon } from "@/test/render-with-daemon";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { Connection } from "@/lib/platform";
import type { WsClient } from "@/lib/ws-client";

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

/** The dialog as the sidebar mounts it, on a fake client whose calls the test resolves by hand. */
function renderDialog(onProjectReady = vi.fn(), onOpenChange = vi.fn()) {
  const client = new FakeWsClient();
  const view = renderWithClientProp(
    <AddProjectDialog
      client={client as unknown as WsClient}
      open
      onOpenChange={onOpenChange}
      workspaces={[]}
      onProjectReady={onProjectReady}
      onStartTask={vi.fn()}
    />,
  );
  return { view, client, onProjectReady, onOpenChange };
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

describe("Add project dialog", () => {
  beforeEach(resetPlatform);

  it("add_project_only_open_a_folder_is_live", async () => {
    const { client, onOpenChange } = renderDialog();
    await flush();

    const open = screen.getByTestId("add-project-method-open");
    expect(open.tagName).toBe("BUTTON");
    expect(open).toHaveAttribute("aria-pressed", "true");
    for (const testId of ["add-project-method-clone", "add-project-method-new", "add-project-method-session"]) {
      const card = screen.getByTestId(testId);
      // Plain divs, not buttons: nothing that looks live is dead.
      expect(card.tagName).toBe("DIV");
      expect(card).toHaveAttribute("aria-disabled", "true");
      expect(card).not.toHaveAttribute("aria-pressed");
      expect(card).toHaveTextContent("Coming soon");
      await act(async () => {
        fireEvent.click(card);
      });
    }
    // Clicking the disabled cards changed nothing: the dialog is still open
    // and quiet, and no create was sent.
    expect(screen.getByTestId("add-project-dialog")).toBeInTheDocument();
    expect(screen.queryByTestId("add-project-error")).toBeNull();
    expect(onOpenChange).not.toHaveBeenCalled();
    expect(client.calls.filter((c) => c.method === "workspace.create")).toHaveLength(0);
  });

  it("add_project_errors_are_sentences_and_the_dialog_stays_open", async () => {
    const { client, onProjectReady } = renderDialog();
    await flush();

    fireEvent.change(screen.getByTestId("add-project-path-input"), { target: { value: "/tmp/repo" } });
    await flush();
    fireEvent.click(screen.getByTestId("add-project-submit"));

    const create = await waitFor(() => client.nth("workspace.create"));
    expect(create.params).toEqual({ path: "/tmp/repo" });
    await act(async () => {
      create.reject(new Error("workspace.create: workspace: /tmp/repo is not a git repository"));
    });
    await flush();

    // The method and package labels are dropped: the person reads one sentence.
    expect(screen.getByTestId("add-project-error")).toHaveTextContent("/tmp/repo is not a git repository.");
    // The dialog stays open with the typed path kept, ready to fix and retry.
    expect(screen.getByTestId("add-project-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("add-project-path-input")).toHaveValue("/tmp/repo");
    expect(onProjectReady).not.toHaveBeenCalled();
  });

  it("add_project_browse_home_place_lists_the_daemon_home", async () => {
    const { client } = renderDialog();
    await flush();

    fireEvent.click(screen.getByTestId("add-project-browse-button"));
    await flush();
    // The chooser opens at the daemon's home (fs.listDir with no path); the
    // Home place re-asks exactly that question.
    const first = await waitFor(() => client.nth("fs.listDir"));
    expect(first.params).toBeUndefined();
    await act(async () => {
      first.resolve({
        path: "/home/dev/repo",
        parent: "/home/dev",
        entries: [{ name: "nested", path: "/home/dev/repo/nested", isGitRepo: false }],
      });
    });
    await flush();

    fireEvent.click(screen.getByTestId("folder-picker-home"));
    const home = await waitFor(() => client.nth("fs.listDir", 1));
    expect(home.params).toBeUndefined();
  });
});

describe("Add project dialog small sizes", () => {
  const originalMatchMedia = window.matchMedia;
  const originalInnerWidth = window.innerWidth;
  const originalInnerHeight = window.innerHeight;

  function setViewport(width: number, height: number): void {
    Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: width });
    Object.defineProperty(window, "innerHeight", { writable: true, configurable: true, value: height });
  }

  // jsdom's matchMedia stub answers false to everything; answer the
  // max-width / max-height queries from the viewport like a real one would.
  function matchesQuery(query: string): boolean {
    const maxWidth = /\(max-width:\s*(\d+)px\)/.exec(query);
    if (maxWidth) return window.innerWidth <= Number(maxWidth[1]);
    const maxHeight = /\(max-height:\s*(\d+)px\)/.exec(query);
    if (maxHeight) return window.innerHeight <= Number(maxHeight[1]);
    return false;
  }

  beforeEach(() => {
    resetPlatform();
    window.matchMedia = vi.fn().mockImplementation(
      (query: string) =>
        ({
          matches: matchesQuery(query),
          media: query,
          onchange: null,
          addEventListener: () => {},
          removeEventListener: () => {},
          addListener: () => {},
          removeListener: () => {},
          dispatchEvent: () => false,
        }) as unknown as MediaQueryList,
    );
  });

  afterEach(() => {
    window.matchMedia = originalMatchMedia;
    setViewport(originalInnerWidth, originalInnerHeight);
  });

  it("add_project_small_window_is_a_full_bleed_sheet", async () => {
    setViewport(800, 520);
    const small = renderDialog();
    await flush();
    expect(screen.getByTestId("add-project-dialog")).toHaveAttribute("data-sheet", "true");
    small.onOpenChange.mockClear();

    setViewport(1280, 800);
    small.view.unmount();
    const large = renderDialog();
    await flush();
    expect(screen.getByTestId("add-project-dialog")).not.toHaveAttribute("data-sheet");
    expect(large.onOpenChange).not.toHaveBeenCalled();
  });
});
