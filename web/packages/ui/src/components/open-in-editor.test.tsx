// desktop-native-feel D4.1: Reveal / Open-in-editor menu items. `@/lib/platform`
// is mocked per scenario (`isDesktop` is a module-load constant) and the
// components are imported fresh, like desktop-daemon-banner.test.tsx.
import { act } from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
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

function mockPlatform(opts: { current: Connection; openInEditor?: (id: string, path: string) => Promise<void> }) {
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
    },
  }));
  return openInEditor;
}

async function mountSidebar() {
  const { AppSidebar } = await import("@/components/app-sidebar");
  const { SidebarProvider } = await import("@/components/ui/sidebar");
  const { Toaster } = await import("@/components/ui/toast");
  const client = new FakeWsClient();
  render(
    <SidebarProvider>
      <Toaster />
      <AppSidebar client={client as never} selectedTaskId={null} />
    </SidebarProvider>,
  );
  client.nth("workspace.list", 0).resolve([WORKSPACE]);
  await flush();
  client.nth("space.list", 0).resolve([]);
  client.nth("task.list", 0).resolve([TASK]);
  await flush();
  const trigger = await screen.findByTestId("sidebar-workspace-actions-trigger");
  fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false });
  await flush();
  await screen.findByText("Delete workspace");
}

async function mountExplorer() {
  const { FileExplorerPane } = await import("@/components/file-explorer-pane");
  const { Toaster } = await import("@/components/ui/toast");
  const client = new FakeWsClient();
  render(
    <>
      <Toaster />
      <FileExplorerPane client={client} task={TASK} />
    </>,
  );
  client.nth("file.list", 0).resolve([{ name: "README.md", isDir: false, size: 1 }]);
  await flush();
  fireEvent.contextMenu(await screen.findByTestId("file-row"));
  await screen.findByTestId("file-menu-copy-path");
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
      await mountExplorer();
      expect(screen.queryByText(/^Reveal in (?!diff$)/)).not.toBeInTheDocument();
      expect(screen.queryByText(/^Open in/)).not.toBeInTheDocument();
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
    await mountExplorer();
    expect(await screen.findByText("Reveal in Finder")).toBeInTheDocument();
    expect(screen.getByText("Open in VS Code")).toBeInTheDocument();
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
      openInEditor: () => Promise.reject(new Error("wslpath failed: no such distro")),
    });
    await mountSidebar();
    fireEvent.click(await screen.findByText("Open in VS Code"));

    await waitFor(() => expect(openInEditor).toHaveBeenCalled());
    expect(await screen.findByText("wslpath failed: no such distro")).toBeInTheDocument();
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
    fireEvent.click(await screen.findByText("Open in VS Code"));
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
    const client = new FakeWsClient();
    render(<FileExplorerPane client={client} task={{ ...TASK, WorktreePath: null }} />);
    client.nth("file.list", 0).resolve([{ name: "README.md", isDir: false, size: 1 }]);
    await flush();
    fireEvent.contextMenu(await screen.findByTestId("file-row"));
    await screen.findByTestId("file-menu-copy-path");
    expect(screen.queryByText(/^Reveal in (?!diff$)/)).not.toBeInTheDocument();
  });
});
