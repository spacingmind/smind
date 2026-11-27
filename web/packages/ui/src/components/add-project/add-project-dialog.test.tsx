import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { HostStep } from "@/components/add-project/host-step";
import { AddProjectDialog } from "@/components/add-project/add-project-dialog";
import { FleetDaemon, fleetHost } from "@/test/fleet-fixtures";
import { renderWithClientProp, renderWithDaemon } from "@/test/render-with-daemon";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { Connection } from "@/lib/platform";
import type { Workspace } from "@/lib/types";
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
function renderDialog(
  onProjectReady = vi.fn(),
  onOpenChange = vi.fn(),
  workspaces: Workspace[] = [],
  onStartTask = vi.fn(),
) {
  const client = new FakeWsClient();
  const view = renderWithClientProp(
    <AddProjectDialog
      client={client as unknown as WsClient}
      open
      onOpenChange={onOpenChange}
      workspaces={workspaces}
      onProjectReady={onProjectReady}
      onStartTask={onStartTask}
    />,
  );
  return { view, client, onProjectReady, onOpenChange, onStartTask };
}

/** One current workspace, as workspace.list answers it. */
function workspaceFixture(path: string, title: string): Workspace {
  return {
    ID: 1,
    Path: path,
    Title: title,
    RoutingPolicy: "",
    CreatedAt: "2026-01-02T03:04:05Z",
    UpdatedAt: "2026-01-02T03:04:05Z",
  };
}

let hostSeq = 0;

/**
 * A desktop connection with a fresh id, so every test below owns its recents
 * key (the connection id, C1 D4): the recents module keeps a session-level
 * cache, and one key per test keeps one test's recents out of the next.
 */
function ownHost(label: string): Connection {
  hostSeq += 1;
  return { id: `conn-${label}-${hostSeq}`, kind: "local", label: "This Mac", baseUrl: "http://127.0.0.1:7700" };
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
    expect(mac).toHaveTextContent("Not available yet: fleet hosts cannot add projects from here.");
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

describe("Add project open a folder", () => {
  beforeEach(resetPlatform);

  /** Blurs the path input with the given value, so the folder check runs (C1 D3: validation on blur). */
  async function typeAndBlur(path: string): Promise<void> {
    const input = screen.getByTestId("add-project-path-input");
    fireEvent.change(input, { target: { value: path } });
    await flush();
    fireEvent.blur(input);
    await flush();
  }

  it("add_project_non_git_hint_before_submit_and_no_git_init_action", async () => {
    platformState.desktop = true;
    platformState.current = ownHost("non-git");
    const { client } = renderDialog();
    await flush();

    await typeAndBlur("/w/plain");
    // The check asks for the PARENT of the typed path (fs.listDir says nothing about the listed dir itself).
    const parent = client.nth("fs.listDir", 0);
    expect(parent.params).toEqual({ path: "/w" });
    await act(async () => {
      parent.resolve({
        path: "/w",
        parent: "/",
        entries: [{ name: "plain", path: "/w/plain", isGitRepo: false }],
      });
    });
    await flush();

    // The hint names the problem and the next step, before submit (AC5).
    const hint = screen.getByTestId("add-project-hint-not-git");
    expect(hint).toHaveTextContent("Not a git repository");
    expect(hint).toHaveTextContent("git init");
    // A known non-repo never reaches the daemon.
    expect(screen.getByTestId("add-project-submit")).toBeDisabled();

    // No git-init action of any kind (C1 has none; that is C2): the instruction is text only.
    expect(screen.queryByRole("button", { name: /git init/i })).toBeNull();
    expect(screen.queryByRole("link", { name: /git init/i })).toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByTestId("add-project-submit"));
    });
    await flush();
    expect(client.calls.filter((call) => call.method === "workspace.create")).toHaveLength(0);
  });

  it("add_project_missing_folder_is_said_before_submit", async () => {
    platformState.desktop = true;
    platformState.current = ownHost("missing");
    const { client } = renderDialog();
    await flush();

    await typeAndBlur("/w/nope");
    const parent = client.nth("fs.listDir", 0);
    await act(async () => {
      // The parent lists another folder: the typed one is not there.
      parent.resolve({
        path: "/w",
        parent: "/",
        entries: [{ name: "plain", path: "/w/plain", isGitRepo: true }],
      });
    });
    await flush();

    // <host> is the current connection's name (useProjectHosts().current.name).
    expect(screen.getByTestId("add-project-hint-missing")).toHaveTextContent("This folder does not exist on This Mac.");
    expect(screen.getByTestId("add-project-submit")).toBeDisabled();
    expect(client.calls.filter((call) => call.method === "workspace.create")).toHaveLength(0);
  });

  it("add_project_relative_path_gets_a_hint", async () => {
    platformState.desktop = true;
    platformState.current = ownHost("relative");
    const { client } = renderDialog();
    await flush();

    await typeAndBlur("code/x");
    expect(screen.getByTestId("add-project-hint-relative")).toHaveTextContent("Use an absolute path, or start with ~.");
    expect(screen.getByTestId("add-project-submit")).toBeDisabled();
    // A relative path is judged client-side: no fs call, no create.
    expect(client.calls.filter((call) => call.method === "fs.listDir")).toHaveLength(0);
    expect(client.calls.filter((call) => call.method === "workspace.create")).toHaveLength(0);
  });

  it("add_project_duplicate_path_switches_instead_of_creating", async () => {
    platformState.desktop = true;
    platformState.current = ownHost("duplicate");
    const alpha = workspaceFixture("/w/alpha", "Alpha");
    const { client, onProjectReady, onOpenChange } = renderDialog(vi.fn(), vi.fn(), [alpha]);
    await flush();

    // The trailing-slash variant of the existing path still matches (D5: the normalised path is compared).
    await typeAndBlur("/w/alpha/");
    expect(screen.getByTestId("add-project-hint-duplicate")).toHaveTextContent("Already a project: Alpha.");
    expect(screen.getByTestId("add-project-open-existing")).toBeInTheDocument();
    expect(screen.getByTestId("add-project-submit")).toHaveTextContent("Open project");

    await act(async () => {
      fireEvent.click(screen.getByTestId("add-project-submit"));
    });
    await flush();

    // Find-or-switch: the existing project is opened, the dialog switches to
    // the done state naming it (D6) instead of closing, and nothing is created.
    expect(onProjectReady).toHaveBeenCalledWith(alpha, "existing");
    expect(screen.getByTestId("add-project-done")).toHaveTextContent("Alpha is already a project.");
    expect(screen.getByTestId("add-project-dialog")).toBeInTheDocument();
    expect(onOpenChange).not.toHaveBeenCalled();
    expect(client.calls.filter((call) => call.method === "workspace.create")).toHaveLength(0);
    // The duplicate was decided without any fs call.
    expect(client.calls.filter((call) => call.method === "fs.listDir")).toHaveLength(0);
  });

  it("add_project_after_create_selects_and_offers_start_a_task", async () => {
    platformState.desktop = true;
    platformState.current = ownHost("done");
    const { view, client, onProjectReady, onOpenChange, onStartTask } = renderDialog();
    await flush();

    await typeAndBlur("/w/new2");
    const parent = client.nth("fs.listDir", 0);
    await act(async () => {
      parent.resolve({
        path: "/w",
        parent: "/",
        entries: [{ name: "new2", path: "/w/new2", isGitRepo: true }],
      });
    });
    await flush();

    await act(async () => {
      fireEvent.click(screen.getByTestId("add-project-submit"));
    });
    const create = await waitFor(() => client.nth("workspace.create", 0));
    expect(create.params).toEqual({ path: "/w/new2" });
    const created: Workspace = { ...workspaceFixture("/w/new2", "New2"), ID: 9 };
    await act(async () => {
      create.resolve(created);
    });
    await flush();

    // The sidebar is told at once (it expands the project and scrolls it into
    // view), and the dialog switches to the done view instead of closing (D6):
    // the form's Add/Cancel footer is gone with the form.
    expect(onProjectReady).toHaveBeenCalledTimes(1);
    expect(onProjectReady).toHaveBeenCalledWith(created, "created");
    expect(screen.getByTestId("add-project-done")).toHaveTextContent("New2 is ready.");
    expect(screen.queryByTestId("add-project-cancel")).toBeNull();
    expect(screen.queryByTestId("add-project-submit")).toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByTestId("add-project-start-task"));
    });
    await flush();
    // Start a task hands the workspace over and closes the dialog.
    expect(onStartTask).toHaveBeenCalledWith(created);
    expect(onOpenChange).toHaveBeenCalledWith(false);
    view.unmount();

    // A duplicate path reaches the same done state by opening the existing
    // project: "is already a project.", and no create was ever sent.
    const alpha = workspaceFixture("/w/alpha", "Alpha");
    const dup = renderDialog(vi.fn(), vi.fn(), [alpha]);
    await flush();
    await typeAndBlur("/w/alpha/");
    await act(async () => {
      fireEvent.click(screen.getByTestId("add-project-submit"));
    });
    await flush();
    expect(screen.getByTestId("add-project-done")).toHaveTextContent("Alpha is already a project.");
    expect(dup.onProjectReady).toHaveBeenCalledWith(alpha, "existing");
    expect(dup.client.calls.filter((call) => call.method === "workspace.create")).toHaveLength(0);
    dup.view.unmount();
  });

  it("add_project_tilde_is_expanded_when_submitting_with_enter", async () => {
    platformState.desktop = true;
    platformState.current = ownHost("tilde-enter");
    const { client } = renderDialog();
    await flush();
    // Typed and submitted with Enter: the path never lost focus, so no blur check ran first.
    fireEvent.change(screen.getByTestId("add-project-path-input"), { target: { value: "~/code/y" } });
    await act(async () => {
      fireEvent.submit(screen.getByTestId("add-project-path-input").closest("form")!);
    });
    const home = await waitFor(() => client.nth("fs.listDir", 0));
    await act(async () => {
      home.resolve({ path: "/home/u", parent: "/home", entries: [] });
    });
    const parent = await waitFor(() => client.nth("fs.listDir", 1));
    await act(async () => {
      parent.resolve({ path: "/home/u/code", parent: "/home/u", entries: [{ name: "y", path: "/home/u/code/y", isGitRepo: true }] });
    });
    const create = await waitFor(() => client.nth("workspace.create", 0));
    expect(create.params).toEqual({ path: "/home/u/code/y" });
  });

  it("add_project_copy_is_plain_and_the_host_chip_is_a_chip", async () => {
    platformState.desktop = true;
    platformState.current = ownHost("copy");
    renderDialog();
    await flush();
    const dialog = screen.getByTestId("add-project-dialog");
    expect(dialog).toHaveTextContent("sMind works on git repositories. Choose a folder that already is one.");
    expect(dialog.textContent).not.toContain("--");
    expect(dialog.textContent?.toLowerCase()).not.toContain("worktree");
    // the host chip is a pill with the server icon, shrink-wrapped, not a full-width input-like bar
    const chip = screen.getByTestId("add-project-host-chip");
    expect(chip).toHaveClass("rounded-full", "w-fit", "justify-self-start");
    expect(chip.querySelector("svg")).not.toBeNull();
  });

  it("add_project_tilde_is_expanded", async () => {
    platformState.desktop = true;
    platformState.current = ownHost("tilde");
    const { client, onProjectReady } = renderDialog();
    await flush();

    await typeAndBlur("~/code/x");
    // `~` is expanded with the daemon user's home, from fs.listDir with no path (C1 D3).
    const home = client.nth("fs.listDir", 0);
    expect(home.params).toEqual({});
    await act(async () => {
      home.resolve({ path: "/home/u", parent: "/home", entries: [] });
    });
    await flush();
    // The check then judges the EXPANDED path, from its parent's listing.
    const parent = client.nth("fs.listDir", 1);
    expect(parent.params).toEqual({ path: "/home/u/code" });
    await act(async () => {
      parent.resolve({
        path: "/home/u/code",
        parent: "/home/u",
        entries: [{ name: "x", path: "/home/u/code/x", isGitRepo: true }],
      });
    });
    await flush();

    await act(async () => {
      fireEvent.click(screen.getByTestId("add-project-submit"));
    });
    const create = await waitFor(() => client.nth("workspace.create", 0));
    expect(create.params).toEqual({ path: "/home/u/code/x" });
    const created = workspaceFixture("/home/u/code/x", "x");
    await act(async () => {
      create.resolve(created);
    });
    await flush();
    expect(onProjectReady).toHaveBeenCalledWith(created, "created");
  });

  it("add_project_recents_list_and_remove", async () => {
    platformState.desktop = true;
    platformState.current = ownHost("recents");
    const gone = workspaceFixture("/w/gone", "Gone");
    const { view, client } = renderDialog(vi.fn(), vi.fn(), [gone]);
    await flush();

    // A current workspace is merged into recents on open but not listed (it is in the sidebar already).
    expect(screen.queryByTestId("add-project-recents")).toBeNull();

    // The project is later removed from the daemon: re-rendering with the new list, the recent stays (D4, AC4).
    view.rerender(
      <AddProjectDialog
        client={client as unknown as WsClient}
        open
        onOpenChange={vi.fn()}
        workspaces={[]}
        onProjectReady={vi.fn()}
        onStartTask={vi.fn()}
      />,
    );
    await flush();
    const row = screen.getByTestId("add-project-recent-0");
    expect(screen.getByTestId("add-project-recents")).toHaveTextContent("Recent");
    expect(row).toHaveTextContent("Gone");
    expect(row).toHaveTextContent("/w/gone");

    // Clicking a row fills the path (and checks it: the parent listing it asks for is a read).
    await act(async () => {
      fireEvent.click(row);
    });
    await flush();
    expect(screen.getByTestId("add-project-path-input")).toHaveValue("/w/gone");

    // The x removes the entry from recents alone, sending no RPC of any kind.
    const x = screen.getByTestId("add-project-recent-remove-0");
    expect(x).toHaveAccessibleName("Remove /w/gone from recents");
    const callsBefore = client.calls.length;
    await act(async () => {
      fireEvent.click(x);
    });
    await flush();
    expect(screen.queryByTestId("add-project-recents")).toBeNull();
    expect(client.calls).toHaveLength(callsBefore);
  });

  it("add_project_created_project_is_recorded_in_recents", async () => {
    platformState.desktop = true;
    const host = ownHost("created");
    platformState.current = host;
    const { view, client, onProjectReady } = renderDialog();
    await flush();

    await typeAndBlur("/w/new");
    const parent = client.nth("fs.listDir", 0);
    await act(async () => {
      parent.resolve({
        path: "/w",
        parent: "/",
        entries: [{ name: "new", path: "/w/new", isGitRepo: true }],
      });
    });
    await flush();

    await act(async () => {
      fireEvent.click(screen.getByTestId("add-project-submit"));
    });
    const create = await waitFor(() => client.nth("workspace.create", 0));
    expect(create.params).toEqual({ path: "/w/new" });
    const created: Workspace = { ...workspaceFixture("/w/new", "New"), ID: 9 };
    await act(async () => {
      create.resolve(created);
    });
    await flush();
    expect(onProjectReady).toHaveBeenCalledWith(created, "created");
    // The create landed on the done state (D6), so the recent row only shows
    // on the next open: closing resets the form, reopening lists it.
    expect(screen.getByTestId("add-project-done")).toHaveTextContent("New is ready.");
    const reopen = (open: boolean) => (
      <AddProjectDialog
        client={client as unknown as WsClient}
        open={open}
        onOpenChange={vi.fn()}
        workspaces={[]}
        onProjectReady={vi.fn()}
        onStartTask={vi.fn()}
      />
    );
    view.rerender(reopen(false));
    await flush();
    view.rerender(reopen(true));
    await flush();

    // The project created here is recorded as a recent, persisted for this host (D4).
    expect(screen.getByTestId("add-project-recent-0")).toHaveTextContent("New");
    expect(screen.getByTestId("add-project-recent-0")).toHaveTextContent("/w/new");
    expect(window.localStorage.getItem(`smind:project-recents:${host.id}`)).toContain("/w/new");
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
