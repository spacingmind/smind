// desktop-native-feel D1: the drawn window chrome. `@/lib/platform` is
// mocked per scenario (the OS and `isDesktop` are module-load constants), and
// each test imports the components fresh, like desktop-daemon-banner.test.tsx.
import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { DesktopOS, WindowState } from "@/lib/platform";

afterEach(() => {
  vi.resetModules();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

function fakeWindow() {
  let emit: (s: WindowState) => void = () => {};
  return {
    minimize: vi.fn(() => Promise.resolve()),
    toggleMaximize: vi.fn(() => Promise.resolve()),
    close: vi.fn(() => Promise.resolve()),
    quit: vi.fn(() => Promise.resolve()),
    destroy: vi.fn(() => Promise.resolve()),
    notifyPainted: vi.fn(() => Promise.resolve()),
    setTheme: vi.fn(() => Promise.resolve()),
    onStateChange: vi.fn((cb: (s: WindowState) => void) => {
      emit = cb;
      cb({ maximized: false, fullscreen: false });
      return () => {};
    }),
    emit: (s: WindowState) => emit(s),
  };
}

function mockPlatform(opts: { desktop: boolean; os: DesktopOS | null }) {
  const win = fakeWindow();
  const reject = () => Promise.reject(new Error("unavailable in this test"));
  vi.doMock("@/lib/platform", () => ({
    isDesktop: opts.desktop,
    desktopOS: opts.os,
    desktopWindow: win,
    desktop: {
      getCurrentConnection: reject,
      connectionVersion: reject,
      daemonStatus: reject,
      onDaemonProgress: () => () => {},
      onMenuAction: () => () => {},
    },
  }));
  return win;
}

async function renderHeader(sidebarPx: number) {
  const { SidebarProvider } = await import("@/components/ui/sidebar");
  const { AppHeader } = await import("@/components/app-header");
  return render(
    <SidebarProvider>
      <AppHeader statusText="Connected" sidebarPx={sidebarPx} />
    </SidebarProvider>,
  );
}

describe("DesktopWindowControls", () => {
  it("platform-window-controls-stub: renders nothing in a non-desktop build", async () => {
    mockPlatform({ desktop: false, os: null });
    const { DesktopWindowControls } = await import("@/components/desktop-window-controls");
    const { container } = render(<DesktopWindowControls />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing on macOS, where the native traffic lights stay", async () => {
    mockPlatform({ desktop: true, os: "macos" });
    const { DesktopWindowControls } = await import("@/components/desktop-window-controls");
    const { container } = render(<DesktopWindowControls />);
    expect(container).toBeEmptyDOMElement();
  });

  it("window-controls-maximize-glyph: a maximized/restored event flips the glyph and aria-label both ways", async () => {
    const win = mockPlatform({ desktop: true, os: "windows" });
    const { DesktopWindowControls } = await import("@/components/desktop-window-controls");
    render(<DesktopWindowControls />);

    const button = () => screen.getByTestId("window-control-maximize");
    expect(button()).toHaveAttribute("aria-label", "Maximize");
    expect(button()).toHaveAttribute("data-maximized", "false");
    const maximizeSvg = button().innerHTML;

    act(() => win.emit({ maximized: true, fullscreen: false }));
    expect(button()).toHaveAttribute("aria-label", "Restore");
    expect(button()).toHaveAttribute("data-maximized", "true");
    expect(button().innerHTML).not.toBe(maximizeSvg);

    act(() => win.emit({ maximized: false, fullscreen: false }));
    expect(button()).toHaveAttribute("aria-label", "Maximize");
    expect(button().innerHTML).toBe(maximizeSvg);
  });

  it("window-controls-click-dispatch: each button calls exactly one platform method; close is close, not quit or destroy", async () => {
    const win = mockPlatform({ desktop: true, os: "linux" });
    const { DesktopWindowControls } = await import("@/components/desktop-window-controls");
    render(<DesktopWindowControls />);

    fireEvent.click(screen.getByTestId("window-control-minimize"));
    expect(win.minimize).toHaveBeenCalledTimes(1);
    expect(win.toggleMaximize).not.toHaveBeenCalled();
    expect(win.close).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("window-control-maximize"));
    expect(win.toggleMaximize).toHaveBeenCalledTimes(1);
    expect(win.minimize).toHaveBeenCalledTimes(1);
    expect(win.close).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("window-control-close"));
    expect(win.close).toHaveBeenCalledTimes(1);
    expect(win.quit).not.toHaveBeenCalled();
    expect(win.destroy).not.toHaveBeenCalled();
    expect(win.minimize).toHaveBeenCalledTimes(1);
    expect(win.toggleMaximize).toHaveBeenCalledTimes(1);
  });
});

describe("AppHeader", () => {
  it("header-drag-region-excludes-controls: the root is a drag region; buttons, inputs and tabs inside are not", async () => {
    mockPlatform({ desktop: true, os: "windows" });
    await renderHeader(240);
    const header = screen.getByTestId("app-header");
    expect(header).toHaveAttribute("data-tauri-drag-region");

    // Put an input and a tab in the header too: whatever is interactive must
    // not opt in to dragging (Tauri's drag script also refuses to start a
    // drag from any of them).
    const input = document.createElement("input");
    const tab = document.createElement("div");
    tab.setAttribute("role", "tab");
    header.append(input, tab);

    const interactive = header.querySelectorAll("button, input, [role=tab]");
    expect(interactive.length).toBeGreaterThanOrEqual(5); // sidebar trigger, input, tab, 3 caption buttons
    for (const el of interactive) expect(el).not.toHaveAttribute("data-tauri-drag-region");
  });

  it("header-reserves-traffic-light-space: macOS pads the header clear of the lights wherever the sidebar doesn't cover them", async () => {
    mockPlatform({ desktop: true, os: "macos" });
    const { TRAFFIC_LIGHT_CLUSTER_PX, SIDEBAR_HANDLE_PX } = await import("@/lib/window-chrome");

    // Sidebar expanded: the header's left edge is already past the lights --
    // the sidebar's own header row holds them (see the AppSidebar tests).
    const expanded = await renderHeader(240);
    expect(screen.getByTestId("app-header").style.paddingLeft).toBe("");
    expanded.unmount();

    // Sidebar collapsed to the 48px rail: the lights overhang it.
    const collapsed = await renderHeader(48);
    expect(screen.getByTestId("app-header").style.paddingLeft).toBe(
      `${8 + TRAFFIC_LIGHT_CLUSTER_PX - 48 - SIDEBAR_HANDLE_PX}px`,
    );
    collapsed.unmount();

    // Narrow window, sidebar sheet closed: the header is at the window edge.
    await renderHeader(0);
    expect(screen.getByTestId("app-header").style.paddingLeft).toBe(`${8 + TRAFFIC_LIGHT_CLUSTER_PX}px`);
    expect(screen.queryByTestId("desktop-window-controls")).not.toBeInTheDocument();
  });

  it("macOS fullscreen hides the lights, so the header inset goes away", async () => {
    const win = mockPlatform({ desktop: true, os: "macos" });
    await renderHeader(0);
    expect(screen.getByTestId("app-header").style.paddingLeft).not.toBe("");

    act(() => win.emit({ maximized: false, fullscreen: true }));
    expect(screen.getByTestId("app-header").style.paddingLeft).toBe("");
  });

  it("header-reserves-caption-space: Windows/Linux end the row in the caption buttons, with nothing after them", async () => {
    for (const os of ["windows", "linux"] as const) {
      mockPlatform({ desktop: true, os });
      const { unmount } = await renderHeader(240);
      const header = screen.getByTestId("app-header");
      // No left inset (there are no traffic lights) ...
      expect(header.style.paddingLeft).toBe("");
      // ... and the caption buttons are the last thing in the row, in flow,
      // so no header action can ever sit under them.
      const controls = screen.getByTestId("desktop-window-controls");
      expect(header.lastElementChild).toBe(controls);
      expect(controls).toHaveClass("shrink-0");
      unmount();
      vi.resetModules();
    }
  });

  it("web build unchanged: no drag region, inset or caption buttons when isDesktop is false", async () => {
    mockPlatform({ desktop: false, os: null });
    await renderHeader(0);
    const header = screen.getByTestId("app-header");
    expect(header).not.toHaveAttribute("data-tauri-drag-region");
    expect(header.style.paddingLeft).toBe("");
    expect(screen.queryByTestId("desktop-window-controls")).not.toBeInTheDocument();
  });
});

async function renderSidebar() {
  const { SidebarProvider } = await import("@/components/ui/sidebar");
  const { AppSidebar } = await import("@/components/app-sidebar");
  const { FakeWsClient } = await import("@/test/fake-ws-client");
  return render(
    <SidebarProvider>
      <AppSidebar client={new FakeWsClient() as never} />
    </SidebarProvider>,
  );
}

describe("AppSidebar window chrome", () => {
  it("macOS: the sidebar's own header row is the 48px title row, left-padded past the lights, draggable around its buttons", async () => {
    mockPlatform({ desktop: true, os: "macos" });
    const { TRAFFIC_LIGHT_CLUSTER_PX } = await import("@/lib/window-chrome");
    await renderSidebar();

    const row = screen.getByTestId("sidebar-expanded-header");
    expect(row).toHaveAttribute("data-tauri-drag-region", "deep");
    expect(row).toHaveClass("h-12", "border-b");
    expect(row.style.paddingLeft).toBe(`${TRAFFIC_LIGHT_CLUSTER_PX}px`);
    // Logo, theme and settings all live in that one row ...
    expect(row.querySelector("img")).not.toBeNull();
    expect(screen.getByTestId("sidebar-settings-button")).toBeInTheDocument();
    for (const el of row.querySelectorAll("button, input, [role=tab]")) {
      expect(el).not.toHaveAttribute("data-tauri-drag-region");
    }
    // ... with no separate empty strip above it.
    expect(screen.queryByTestId("sidebar-window-inset")).not.toBeInTheDocument();
    expect(screen.getByTestId("sidebar-expanded-header").parentElement?.firstElementChild).toBe(row);
  });

  it("macOS: the collapsed rail keeps its icons below the lights with a borderless, collapsed-only spacer", async () => {
    mockPlatform({ desktop: true, os: "macos" });
    await renderSidebar();
    const spacer = screen.getByTestId("sidebar-rail-inset");
    expect(spacer).toHaveAttribute("data-tauri-drag-region");
    expect(spacer).toHaveClass("hidden", "group-data-[collapsible=icon]:block", "h-12");
    expect(spacer.className).not.toMatch(/border/);
    // Expanded row is hidden in the rail, as before.
    expect(screen.getByTestId("sidebar-expanded-header")).toHaveClass("group-data-[collapsible=icon]:hidden");
  });

  it("macOS fullscreen: no lights, so the sidebar header goes back to its normal row", async () => {
    const win = mockPlatform({ desktop: true, os: "macos" });
    await renderSidebar();
    act(() => win.emit({ maximized: false, fullscreen: true }));
    const row = screen.getByTestId("sidebar-expanded-header");
    expect(row).not.toHaveAttribute("data-tauri-drag-region");
    expect(row).not.toHaveClass("h-12");
    expect(row.style.paddingLeft).toBe("");
    expect(screen.queryByTestId("sidebar-rail-inset")).not.toBeInTheDocument();
  });

  it.each([
    ["windows", true],
    ["linux", true],
    [null, false],
  ] as const)("%s: the sidebar header is untouched", async (os, desktop) => {
    mockPlatform({ desktop, os });
    await renderSidebar();
    const row = screen.getByTestId("sidebar-expanded-header");
    expect(row).not.toHaveAttribute("data-tauri-drag-region");
    expect(row).not.toHaveClass("h-12");
    expect(row.style.paddingLeft).toBe("");
    expect(screen.queryByTestId("sidebar-rail-inset")).not.toBeInTheDocument();
  });
});

describe("opaque surfaces on macOS (the body is transparent over vibrancy)", () => {
  it("the main content container, the unreachable screen, the empty state, settings and the header carry an opaque bg token", async () => {
    mockPlatform({ desktop: true, os: "macos" });
    const { App } = await import("@/App");
    render(<App connect={() => Promise.reject(new Error("no daemon"))} />);

    const main = await screen.findByTestId("app-main-content");
    expect(main).toHaveClass("bg-background");
    expect(await screen.findByTestId("desktop-unreachable")).toHaveClass("bg-background");
    expect(screen.getByTestId("app-header")).toHaveClass("bg-background");
  });

  it("the empty state and the settings screen carry an opaque bg token too", async () => {
    mockPlatform({ desktop: true, os: "macos" });
    const { App } = await import("@/App");
    // Connected-but-no-task: connection never settles, so the shell shows its empty state.
    render(<App connect={() => new Promise(() => {})} />);
    expect(await screen.findByTestId("app-empty-state")).toHaveClass("bg-background");

    const { SettingsScreen } = await import("@/components/settings/settings-screen");
    const { FakeWsClient } = await import("@/test/fake-ws-client");
    render(<SettingsScreen client={new FakeWsClient() as never} events={null} onNavigateBack={() => {}} />);
    expect(screen.getByTestId("settings-screen")).toHaveClass("bg-background");
  });
});

describe("DesktopPaintedSignal", () => {
  it("tells the shell the UI has rendered, without waiting for an animation frame (a hidden webview never gets one)", async () => {
    vi.useFakeTimers();
    const win = mockPlatform({ desktop: true, os: "macos" });
    const raf = vi.fn();
    vi.stubGlobal("requestAnimationFrame", raf);
    const { DesktopPaintedSignal } = await import("@/components/desktop-painted-signal");
    render(<DesktopPaintedSignal />);

    expect(win.notifyPainted).not.toHaveBeenCalled();
    vi.runAllTimers();
    expect(win.notifyPainted).toHaveBeenCalledTimes(1);
    expect(raf).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("does nothing in a browser build", async () => {
    vi.useFakeTimers();
    const win = mockPlatform({ desktop: false, os: null });
    const { DesktopPaintedSignal } = await import("@/components/desktop-painted-signal");
    render(<DesktopPaintedSignal />);
    vi.runAllTimers();
    expect(win.notifyPainted).not.toHaveBeenCalled();
    vi.useRealTimers();
  });
});
