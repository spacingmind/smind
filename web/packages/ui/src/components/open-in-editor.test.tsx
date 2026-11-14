// desktop-native-feel D4.1: Reveal / Open-in-editor menu items. `@/lib/platform`
// is mocked per scenario (`isDesktop` is a module-load constant) and the
// components are imported fresh, like desktop-daemon-banner.test.tsx.
import { act } from "react";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { revealWorkspaceActions } from "@/test/sidebar-helpers";
import type { Connection, EditorInfo } from "@/lib/platform";
import type { Task, Workspace } from "@/lib/types";

const LOCAL: Connection = { id: "local", kind: "local", label: "Local", baseUrl: "http://127.0.0.1:4648" };
const URL_CONN: Connection = { id: "url:example", kind: "url", label: "Tunnel", baseUrl: "http://example.com:9000" };
const RELAY: Connection = { id: "relay:ws-1@relay.test:7400", kind: "relay", label: "Relay", baseUrl: "relay://ws-1@relay.test:7400" };

const EDITORS: EditorInfo[] = [
  { id: "file-manager", label: "Finder", kind: "fileManager" },
  { id: "vscode", label: "VS Code", kind: "editor" },
  { id: "zed", label: "Zed", kind: "editor" },
];

const WORKSPACE: Workspace = {
  ID: 1,
  Path: "/home/dev/repo",
  Title: "Repo",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

const TASK: Task = {
  ID: 42,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Fix the bug",
  Status: "active",
  WorktreePath: "/home/dev/repo-wt/",
  Branch: "fix-bug",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

// The first scenario used to pay for the cold transform of the sidebar and explorer graphs inside its own 5 s test
// budget, which a loaded machine exceeds. Pull them in once, up front: later scenarios reuse the transformed modules
// (vi.resetModules only clears the registry), so every test body is just its own work.
// Only this one-off import gets a larger budget (a saturated box needs more than the 10 s hook default to
// transform the app graph cold); every test body keeps the default 5 s.
const WARMUP_TIMEOUT_MS = 120_000;
beforeAll(async () => {
  await Promise.all([
    import("@/components/app-sidebar"),
    import("@/components/file-explorer-pane"),
    import("@/components/ui/sidebar"),
    import("@/components/ui/toast"),
    import("@/test/render-with-daemon"),
  ]);
}, WARMUP_TIMEOUT_MS);

afterEach(async () => {
  const { clearToasts } = await import("@/components/ui/toast");
  clearToasts();
  vi.resetModules();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

// In the desktop build the explorer's row menu is NATIVE (DT2 a): the page sends descriptors to `contextMenuShow` and
// the chosen item comes back through `onContextMenuAction`. These hooks stand in for the Rust side.
const native: { show: ReturnType<typeof vi.fn>; deliver: (id: string) => void; listener: ((s: { open: number; id: string }) => void) | null } = {
  show: vi.fn(),
  listener: null,
  deliver(id) {
    act(() => native.listener?.({ open: 1, id }));
  },
};

/** Every item label the native menu was shown with, submenus flattened. */
function nativeLabels(): string[] {
  const entries = native.show.mock.calls.at(-1)?.[0] as Array<{ kind: string; label?: string; children?: Array<{ label?: string }> }> | undefined;
  return (entries ?? []).flatMap((e) => (e.kind === "item" ? [e.label ?? ""] : e.kind === "submenu" ? (e.children ?? []).map((c) => c.label ?? "") : []));
}

function mockPlatform(opts: { current: Connection; openInEditor?: (id: string, path: string) => Promise<void> }) {
  native.show = vi.fn(async () => 1);
  native.listener = null;
  const openInEditor = vi.fn(opts.openInEditor ?? (() => Promise.resolve()));
  vi.doMock("@/lib/platform", async (importOriginal) => ({
    ...(await importOriginal<typeof import("@/lib/platform")>()),
    isDesktop: true,
    desktop: {
      getCurrentConnection: () => Promise.resolve(opts.current),
      editorsList: () => Promise.resolve(EDITORS),
      openInEditor,
      connectionVersion: () => Promise.reject(new Error("unused")),
      daemonStatus: () => Promise.reject(new Error("unused")),
      onDaemonProgress: () => () => {},
      onMenuAction: () => () => {},
      contextMenuShow: native.show,
      onContextMenuAction: (cb: (s: { open: number; id: string }) => void) => {
        native.listener = cb;
        return Promise.resolve(() => {});
      },
    },
  }));
  return openInEditor;
}

async function mountSidebar() {
  const { AppSidebar } = await import("@/components/app-sidebar");
  const { SidebarProvider } = await import("@/components/ui/sidebar");
  const { Toaster } = await import("@/components/ui/toast");
  // Imported fresh with the rest: the harness's QueryClient/connection contexts must be the same module
  // instances the freshly imported sidebar reads (this file resets the module registry per scenario).
  const { renderWithClientProp } = await import("@/test/render-with-daemon");
  const client = new FakeWsClient();
  renderWithClientProp(
    <SidebarProvider>
      <Toaster />
      <AppSidebar client={client as never} selectedTaskId={null} />
    </SidebarProvider>,
  );
  // The tree is a query now: its fetch starts a tick after mount.
  await act(async () => {});
  client.nth("workspace.list", 0).resolve([WORKSPACE]);
  await flush();
  client.nth("space.list", 0).resolve([]);
  client.nth("task.list", 0).resolve([TASK]);
  await flush();
  const trigger = await revealWorkspaceActions();
  fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false });
  await flush();
  await screen.findByText("Delete workspace");
}

async function mountExplorer() {
  vi.resetModules(); // the warm-up above cached the explorer against the real platform module; this scenario's mock must apply
  const { FileExplorerPane } = await import("@/components/file-explorer-pane");
  const { Toaster } = await import("@/components/ui/toast");
  const { renderWithClientProp } = await import("@/test/render-with-daemon");
  const client = new FakeWsClient();
  renderWithClientProp(
    <>
      <Toaster />
      <FileExplorerPane client={client} task={TASK} />
    </>,
  );
  client.nth("file.list", 0).resolve([{ name: "README.md", isDir: false, size: 1 }]);
  await flush();
  fireEvent.contextMenu(await screen.findByTestId("file-row"));
  await waitFor(() => expect(native.show).toHaveBeenCalled());
  return nativeLabels();
}

describe("open in editor", () => {
  it("open-in-editor-hidden-for-remote: items are absent for url and relay connections, present for local", async () => {
    for (const current of [URL_CONN, RELAY]) {
      mockPlatform({ current });
      await mountSidebar();
      expect(screen.queryByText(/^Reveal in (?!diff$)/)).not.toBeInTheDocument();
      expect(screen.queryByText(/^Open in/)).not.toBeInTheDocument();
      cleanup();
      vi.resetModules();

      mockPlatform({ current });
      const labels = await mountExplorer();
      expect(labels.some((l) => /^Reveal in (?!diff$)/.test(l))).toBe(false);
      expect(labels.some((l) => /^Open in/.test(l))).toBe(false);
      cleanup();
      vi.resetModules();
    }

    mockPlatform({ current: LOCAL });
    await mountSidebar();
    expect(await screen.findByText("Reveal in Finder")).toBeInTheDocument();
    expect(screen.getByText("Open in VS Code")).toBeInTheDocument();
    expect(screen.getByText("Open in Zed")).toBeInTheDocument();
    // The file manager is Reveal's, not an "Open in" entry.
    expect(screen.queryByText("Open in Finder")).not.toBeInTheDocument();
    cleanup();
    vi.resetModules();

    mockPlatform({ current: LOCAL });
    const labels = await mountExplorer();
    expect(labels).toContain("Reveal in Finder");
    expect(labels).toContain("Open in VS Code");
  });

  it("open-in-editor-hidden-for-remote: items are absent in a non-desktop build", async () => {
    vi.doMock("@/lib/platform", async (importOriginal) => ({
      ...(await importOriginal<typeof import("@/lib/platform")>()),
      isDesktop: false,
    }));
    await mountSidebar();
    expect(screen.queryByText(/^Reveal in (?!diff$)/)).not.toBeInTheDocument();
    expect(screen.queryByText(/^Open in/)).not.toBeInTheDocument();
  });

  it("open-in-editor-error-toast: a rejected openInEditor shows the Rust error message", async () => {
    const openInEditor = mockPlatform({
      current: LOCAL,
      openInEditor: () => Promise.reject(new Error("path must be absolute: /etc/passwd is not registered for this connection")),
    });
    await mountSidebar();
    fireEvent.click(await screen.findByText("Open in VS Code"));

    await waitFor(() => expect(openInEditor).toHaveBeenCalled());
    expect(await screen.findByText("path must be absolute: /etc/passwd is not registered for this connection")).toBeInTheDocument();
    expect(screen.getByText("Couldn't open in VS Code")).toBeInTheDocument();
  });

  it("open-in-editor-passes-id-and-absolute-path: workspace row uses the workspace path", async () => {
    const openInEditor = mockPlatform({ current: LOCAL });
    await mountSidebar();
    fireEvent.click(await screen.findByText("Open in Zed"));
    await waitFor(() => expect(openInEditor).toHaveBeenCalledTimes(1));
    expect(openInEditor).toHaveBeenCalledWith("zed", "/home/dev/repo");
  });

  it("open-in-editor-passes-id-and-absolute-path: Reveal passes the file-manager id", async () => {
    const openInEditor = mockPlatform({ current: LOCAL });
    await mountSidebar();
    fireEvent.click(await screen.findByText("Reveal in Finder"));
    await waitFor(() => expect(openInEditor).toHaveBeenCalledTimes(1));
    expect(openInEditor).toHaveBeenCalledWith("file-manager", "/home/dev/repo");
  });

  it("open-in-editor-passes-id-and-absolute-path: file row joins the worktree path and the relative path", async () => {
    const openInEditor = mockPlatform({ current: LOCAL });
    await mountExplorer();
    native.deliver("open-in-editor:vscode");
    await waitFor(() => expect(openInEditor).toHaveBeenCalledTimes(1));
    // The worktree's trailing slash is trimmed so the join has exactly one.
    expect(openInEditor).toHaveBeenCalledWith("vscode", "/home/dev/repo-wt/README.md");
  });

  it("open-in-editor-hidden-for-remote: switching to a url connection drops the items without a reload", async () => {
    let current: Connection = LOCAL;
    vi.doMock("@/lib/platform", async (importOriginal) => ({
      ...(await importOriginal<typeof import("@/lib/platform")>()),
      isDesktop: true,
      desktop: {
        getCurrentConnection: () => Promise.resolve(current),
        editorsList: () => Promise.resolve(EDITORS),
        openInEditor: () => Promise.resolve(),
      },
    }));
    const { renderHook } = await import("@testing-library/react");
    const { useLocalPathActions } = await import("@/hooks/use-local-path-actions");
    const { CONNECTION_CHANGED_EVENT } = await import("@/lib/local-paths");
    const { result } = renderHook(() => useLocalPathActions());
    await flush();
    expect(result.current?.revealLabel).toBe("Reveal in Finder");

    current = URL_CONN;
    await act(async () => {
      window.dispatchEvent(new Event(CONNECTION_CHANGED_EVENT));
    });
    await flush();
    expect(result.current).toBeNull();
  });

  it("file rows have no items when the task has no worktree", async () => {
    mockPlatform({ current: LOCAL });
    const { FileExplorerPane } = await import("@/components/file-explorer-pane");
    const { renderWithClientProp } = await import("@/test/render-with-daemon");
    const client = new FakeWsClient();
    renderWithClientProp(<FileExplorerPane client={client} task={{ ...TASK, WorktreePath: null }} />);
    client.nth("file.list", 0).resolve([{ name: "README.md", isDir: false, size: 1 }]);
    await flush();
    fireEvent.contextMenu(await screen.findByTestId("file-row"));
    await waitFor(() => expect(native.show).toHaveBeenCalled());
    expect(nativeLabels().some((l) => /^Reveal in (?!diff$)/.test(l))).toBe(false);
    expect(nativeLabels()).toContain("Copy path");
  });
});
