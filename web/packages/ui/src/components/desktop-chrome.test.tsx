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
  vi.doMock("@/lib/platform", () => ({
    isDesktop: opts.desktop,
    desktopOS: opts.os,
    desktopWindow: win,
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

  it("header-reserves-traffic-light-space: macOS pads clear of the lights for collapsed and hidden sidebars; expanded, the sidebar's top strip holds them", async () => {
    mockPlatform({ desktop: true, os: "macos" });
    const { DesktopSidebarInset } = await import("@/components/desktop-sidebar-inset");
    const { TRAFFIC_LIGHT_CLUSTER_PX, SIDEBAR_HANDLE_PX } = await import("@/lib/window-chrome");

    // Sidebar expanded: the header's left edge is past the lights, and the
    // sidebar's own top strip is what reserves them.
    const expanded = await renderHeader(240);
    expect(screen.getByTestId("app-header").style.paddingLeft).toBe("");
    expanded.unmount();
    const strip = render(<DesktopSidebarInset />);
    expect(screen.getByTestId("sidebar-window-inset")).toBeInTheDocument();
    expect(screen.getByTestId("sidebar-window-inset")).toHaveAttribute("data-tauri-drag-region");
    strip.unmount();

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

  it("macOS fullscreen hides the lights, so the inset and the sidebar strip go away", async () => {
    const win = mockPlatform({ desktop: true, os: "macos" });
    const { DesktopSidebarInset } = await import("@/components/desktop-sidebar-inset");
    await renderHeader(0);
    render(<DesktopSidebarInset />);
    expect(screen.getByTestId("sidebar-window-inset")).toBeInTheDocument();

    act(() => win.emit({ maximized: false, fullscreen: true }));
    expect(screen.getByTestId("app-header").style.paddingLeft).toBe("");
    expect(screen.queryByTestId("sidebar-window-inset")).not.toBeInTheDocument();
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
      expect(screen.queryByTestId("sidebar-window-inset")).not.toBeInTheDocument();
      unmount();
      vi.resetModules();
    }
  });

  it("web build unchanged: no drag region, inset, caption buttons or sidebar strip when isDesktop is false", async () => {
    mockPlatform({ desktop: false, os: null });
    const { DesktopSidebarInset } = await import("@/components/desktop-sidebar-inset");
    await renderHeader(0);
    render(<DesktopSidebarInset />);
    const header = screen.getByTestId("app-header");
    expect(header).not.toHaveAttribute("data-tauri-drag-region");
    expect(header.style.paddingLeft).toBe("");
    expect(screen.queryByTestId("desktop-window-controls")).not.toBeInTheDocument();
    expect(screen.queryByTestId("sidebar-window-inset")).not.toBeInTheDocument();
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
