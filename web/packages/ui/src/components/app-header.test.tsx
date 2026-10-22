// app-header-and-tabs AH-AC2/4/5/6/7/8/9/10: the app header. `@/lib/platform`
// is mocked per scenario (`isDesktop`/`desktopOS` are module-load constants)
// and the components are imported fresh, like desktop-chrome.test.tsx.
import { act } from "react";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { ConnectionStatus } from "@/lib/reconnect";
import type { Connection, DesktopOS, EditorInfo } from "@/lib/platform";
import type { Task } from "@/lib/types";

const LOCAL: Connection = { id: "local", kind: "local", label: "Local", baseUrl: "http://127.0.0.1:4648" };
const URL_CONN: Connection = { id: "url:example", kind: "url", label: "Tunnel", baseUrl: "http://example.com:9000" };

const EDITORS: EditorInfo[] = [
  { id: "file-manager", label: "Finder", kind: "fileManager" },
  { id: "vscode", label: "VS Code", kind: "editor" },
  { id: "zed", label: "Zed", kind: "editor" },
];

const TASK: Task = {
  ID: 42,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Fix the bug",
  Status: "active",
  WorktreePath: "/home/dev/repo-wt",
  Branch: "fix-bug",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

beforeEach(() => {
  window.localStorage.clear();
});

afterEach(() => {
  cleanup();
  window.localStorage.clear();
  vi.resetModules();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

interface PlatformOpts {
  desktop: boolean;
  os: DesktopOS | null;
  current?: Connection;
  editors?: EditorInfo[];
}

function mockPlatform(opts: PlatformOpts) {
  const openInEditor = vi.fn((_id: string, _path: string) => Promise.resolve());
  const reject = () => Promise.reject(new Error("unavailable in this test"));
  vi.doMock("@/lib/platform", async (importOriginal) => ({
    ...(await importOriginal<typeof import("@/lib/platform")>()),
    isDesktop: opts.desktop,
    desktopOS: opts.os,
    desktopWindow: {
      minimize: vi.fn(() => Promise.resolve()),
      toggleMaximize: vi.fn(() => Promise.resolve()),
      close: vi.fn(() => Promise.resolve()),
      quit: vi.fn(() => Promise.resolve()),
      destroy: vi.fn(() => Promise.resolve()),
      notifyPainted: vi.fn(() => Promise.resolve()),
      setTheme: vi.fn(() => Promise.resolve()),
      onStateChange: (cb: (s: { maximized: boolean; fullscreen: boolean }) => void) => {
        cb({ maximized: false, fullscreen: false });
        return () => {};
      },
    },
    desktop: {
      getCurrentConnection: () => Promise.resolve(opts.current ?? LOCAL),
      editorsList: () => Promise.resolve(opts.editors ?? EDITORS),
      openInEditor,
      connectionVersion: reject,
      daemonStatus: reject,
      onDaemonProgress: () => () => {},
      onMenuAction: () => () => {},
      menuAction: () => Promise.resolve(),
      appUpdateCheck: () => Promise.resolve({ kind: "unsupported" }),
      appUpdateInstall: () => Promise.resolve(),
      onAppUpdateCheckRequested: () => () => {},
      onAppUpdateProgress: () => () => {},
    },
  }));
  return openInEditor;
}

interface HeaderProps {
  task?: Task | null;
  client?: FakeWsClient | null;
  connectionStatus?: ConnectionStatus;
  statusText?: string;
  showSidebarToggle?: boolean;
  onOpenTerminal?: () => void;
  onHelp?: () => void;
}

async function mountHeader(props: HeaderProps = {}) {
  const { SidebarProvider } = await import("@/components/ui/sidebar");
  const { AppHeader } = await import("@/components/app-header");
  const { KeyboardProvider, useActionHandler } = await import("@/keyboard/keyboard-provider");
  function Claims() {
    useActionHandler("shortcuts.help", () => props.onHelp?.());
    return null;
  }
  const result = render(
    <KeyboardProvider>
      <Claims />
      <SidebarProvider>
        <AppHeader
          statusText={props.statusText ?? "Connected to daemon"}
          connectionStatus={props.connectionStatus}
          sidebarPx={240}
          showSidebarToggle={props.showSidebarToggle}
          task={props.task === undefined ? TASK : props.task}
          client={(props.client ?? null) as never}
          onOpenTerminal={props.onOpenTerminal}
        />
      </SidebarProvider>
    </KeyboardProvider>,
  );
  await flush();
  return result;
}

async function openMenu(testId: string): Promise<void> {
  fireEvent.pointerDown(screen.getByTestId(testId), { button: 0, ctrlKey: false });
  await flush();
}

function before(a: Element, b: Element): boolean {
  return Boolean(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING);
}

describe("AppHeader task identity", () => {
  it("AH-AC2 shows icon, title and menu only with a task", async () => {
    mockPlatform({ desktop: false, os: null });
    await mountHeader();
    const title = screen.getByTestId("app-header-title");
    expect(title).toHaveTextContent("Fix the bug");
    expect(title.querySelector("svg")).not.toBeNull();
    expect(screen.getByTestId("app-header-task-menu")).toBeInTheDocument();
    cleanup();

    await mountHeader({ task: null, onOpenTerminal: () => {} });
    expect(screen.getByTestId("app-header")).toBeInTheDocument();
    expect(screen.queryByTestId("app-header-title")).not.toBeInTheDocument();
    expect(screen.queryByTestId("app-header-task-menu")).not.toBeInTheDocument();
    expect(screen.queryByTestId("app-header-task")).not.toBeInTheDocument();
    expect(screen.queryByTestId("open-in-editor")).not.toBeInTheDocument();
  });

  it("AH-AC4 task menu items and archive confirm", async () => {
    mockPlatform({ desktop: false, os: null });
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    const client = new FakeWsClient();
    await mountHeader({ client });

    await openMenu("app-header-task-menu");
    const menu = screen.getByTestId("app-header-task-menu-content");
    expect(within(menu).getByText("Copy branch name")).toBeInTheDocument();
    expect(within(menu).getByText("Copy worktree path")).toBeInTheDocument();
    expect(within(menu).getByText("Archive task…")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("app-header-copy-branch"));
    await flush();
    expect(writeText).toHaveBeenCalledWith("fix-bug");

    await openMenu("app-header-task-menu");
    fireEvent.click(screen.getByTestId("app-header-copy-worktree"));
    await flush();
    expect(writeText).toHaveBeenCalledWith("/home/dev/repo-wt");

    await openMenu("app-header-task-menu");
    fireEvent.click(screen.getByTestId("app-header-archive"));
    await flush();
    expect(await screen.findByText("Archive task?")).toBeInTheDocument();
    expect(client.calls.some((c) => c.method === "task.archive")).toBe(false);

    fireEvent.click(screen.getByTestId("dialog-archive-task-confirm"));
    await flush();
    expect(client.nth("task.archive").params).toEqual({ id: 42 });
  });

  it("AH-AC4 reveal only for local desktop", async () => {
    const cases: { name: string; opts: PlatformOpts; shown: boolean }[] = [
      { name: "local desktop", opts: { desktop: true, os: "macos", current: LOCAL }, shown: true },
      { name: "web", opts: { desktop: false, os: null }, shown: false },
      { name: "url connection", opts: { desktop: true, os: "macos", current: URL_CONN }, shown: false },
    ];
    for (const { name, opts, shown } of cases) {
      vi.resetModules();
      vi.doUnmock("@/lib/platform");
      mockPlatform(opts);
      await mountHeader();
      await openMenu("app-header-task-menu");
      await waitFor(() => expect(screen.getByTestId("app-header-task-menu-content")).toBeInTheDocument());
      if (shown) {
        await waitFor(() => expect(screen.getByTestId("app-header-reveal"), name).toHaveTextContent("Reveal in Finder"));
      } else {
        expect(screen.queryByTestId("app-header-reveal"), name).not.toBeInTheDocument();
      }
      cleanup();
    }
  });
});

describe("AppHeader right cluster", () => {
  it("AH-AC5 right cluster order", async () => {
    for (const os of ["windows", "linux"] as const) {
      vi.resetModules();
      vi.doUnmock("@/lib/platform");
      mockPlatform({ desktop: true, os });
      await mountHeader({ onOpenTerminal: () => {} });
      await waitFor(() => expect(screen.getByTestId("open-in-editor")).toBeInTheDocument());
      const order = [
        screen.getByTestId("open-in-editor"),
        screen.getByTestId("app-header-help"),
        screen.getByTestId("app-header-terminal"),
        screen.getByTestId("app-menu-trigger"),
        screen.getByTestId("desktop-window-controls"),
      ];
      for (let i = 0; i < order.length - 1; i++) {
        expect(before(order[i], order[i + 1]), `${os}: ${i} before ${i + 1}`).toBe(true);
      }
      cleanup();
    }
  });

  it("AH-AC6 editor pill: gated, remembers editor, opens worktree", async () => {
    const absent: { name: string; opts: PlatformOpts; task?: Task }[] = [
      { name: "web", opts: { desktop: false, os: null } },
      { name: "url connection", opts: { desktop: true, os: "windows", current: URL_CONN } },
      { name: "no worktree", opts: { desktop: true, os: "windows" }, task: { ...TASK, WorktreePath: "" } },
      { name: "no editors", opts: { desktop: true, os: "windows", editors: [EDITORS[0]] } },
    ];
    for (const { name, opts, task } of absent) {
      vi.resetModules();
      vi.doUnmock("@/lib/platform");
      mockPlatform(opts);
      await mountHeader({ task });
      await flush();
      expect(screen.queryByTestId("open-in-editor"), name).not.toBeInTheDocument();
      cleanup();
    }

    vi.resetModules();
    vi.doUnmock("@/lib/platform");
    const openInEditor = mockPlatform({ desktop: true, os: "windows" });
    await mountHeader();
    const pill = await screen.findByTestId("open-in-editor");
    // Default: the first installed editor.
    expect(within(pill).getByTestId("open-in-editor-main")).toHaveAttribute("aria-label", "Open in VS Code");
    fireEvent.click(within(pill).getByTestId("open-in-editor-main"));
    expect(openInEditor).toHaveBeenLastCalledWith("vscode", "/home/dev/repo-wt");

    await openMenu("open-in-editor-picker");
    const items = screen.getAllByTestId("open-in-editor-item");
    expect(items.map((i) => i.getAttribute("data-editor-id"))).toEqual(["vscode", "zed"]);
    fireEvent.click(items[1]);
    await flush();
    expect(openInEditor).toHaveBeenLastCalledWith("zed", "/home/dev/repo-wt");
    expect(window.localStorage.getItem("smind:preferred-editor")).toBe(JSON.stringify("zed"));

    expect(screen.getByTestId("open-in-editor-main")).toHaveAttribute("aria-label", "Open in Zed");
    fireEvent.click(screen.getByTestId("open-in-editor-main"));
    expect(openInEditor).toHaveBeenLastCalledWith("zed", "/home/dev/repo-wt");

    // Remembered across a reload (fresh mount reads the stored id).
    cleanup();
    await mountHeader();
    expect(await screen.findByTestId("open-in-editor-main")).toHaveAttribute("aria-label", "Open in Zed");
  });

  it("AH-AC7 help runs shortcuts.help", async () => {
    mockPlatform({ desktop: false, os: null });
    const onHelp = vi.fn();
    await mountHeader({ onHelp });
    fireEvent.click(screen.getByTestId("app-header-help"));
    expect(onHelp).toHaveBeenCalledTimes(1);
  });

  it("AH-AC7 terminal button calls onOpenTerminal and is absent without a task", async () => {
    mockPlatform({ desktop: false, os: null });
    const onOpenTerminal = vi.fn();
    await mountHeader({ onOpenTerminal });
    fireEvent.click(screen.getByTestId("app-header-terminal"));
    expect(onOpenTerminal).toHaveBeenCalledTimes(1);
    cleanup();

    await mountHeader({ task: null, onOpenTerminal });
    expect(screen.queryByTestId("app-header-terminal")).not.toBeInTheDocument();
  });
});

describe("AppHeader status and toggle", () => {
  it("AH-AC8 connection text is sr-only while connected and visible when not", async () => {
    mockPlatform({ desktop: false, os: null });
    await mountHeader({ connectionStatus: "connected", statusText: "Connected to daemon" });
    let status = screen.getByRole("status");
    expect(status).toHaveTextContent("Connected to daemon");
    expect(status).toHaveClass("sr-only");
    cleanup();

    for (const connectionStatus of ["reconnecting", "disconnected"] as const) {
      await mountHeader({ connectionStatus, statusText: `Daemon ${connectionStatus}` });
      status = screen.getByRole("status");
      expect(status).toHaveTextContent(`Daemon ${connectionStatus}`);
      expect(status).not.toHaveClass("sr-only");
      cleanup();
    }
  });

  it("AH-AC9 toggle in header only when showSidebarToggle", async () => {
    mockPlatform({ desktop: false, os: null });
    await mountHeader({ showSidebarToggle: true });
    expect(within(screen.getByTestId("app-header")).getByRole("button", { name: /toggle sidebar/i })).toBeInTheDocument();
    cleanup();
    await mountHeader({ showSidebarToggle: false });
    expect(within(screen.getByTestId("app-header")).queryByRole("button", { name: /toggle sidebar/i })).not.toBeInTheDocument();
  });

  it("AH-AC10 header keeps the deep drag region, controls carry none, and window controls end the row", async () => {
    mockPlatform({ desktop: true, os: "windows" });
    await mountHeader({ onOpenTerminal: () => {} });
    const header = screen.getByTestId("app-header");
    expect(header).toHaveAttribute("data-tauri-drag-region", "deep");
    const controls = screen.getByTestId("desktop-window-controls");
    expect(header.lastElementChild).toBe(controls);
    expect(before(screen.getByTestId("app-menu-trigger"), controls)).toBe(true);
    expect(controls.hasAttribute("data-tauri-drag-region")).toBe(false);
    for (const el of controls.querySelectorAll("*")) expect(el.hasAttribute("data-tauri-drag-region")).toBe(false);
    for (const id of ["app-header-help", "app-header-terminal", "app-header-task-menu", "app-menu-trigger"]) {
      expect(screen.getByTestId(id).hasAttribute("data-tauri-drag-region"), id).toBe(false);
    }
  });
});
