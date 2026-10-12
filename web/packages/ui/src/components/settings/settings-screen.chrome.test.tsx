// settings-fullscreen S1b: the settings screen's desktop window chrome.
// A separate file from settings-screen.test.tsx -- like
// desktop-chrome.test.tsx, these tests `vi.doMock` @/lib/platform, and the
// platform constants are module-load values shared by everything in a file,
// so the mock stays local to this file.
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { DesktopOS, WindowState } from "@/lib/platform";

afterEach(() => {
  vi.resetModules();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

function mockPlatform(opts: { desktop: boolean; os: DesktopOS | null }) {
  const reject = () => Promise.reject(new Error("unavailable in this test"));
  vi.doMock("@/lib/platform", () => ({
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
      onStateChange: vi.fn((cb: (s: WindowState) => void) => {
        cb({ maximized: false, fullscreen: false });
        return () => {};
      }),
    },
    desktop: {
      getCurrentConnection: reject,
      connectionVersion: reject,
      daemonStatus: reject,
      onDaemonProgress: () => () => {},
      onMenuAction: () => () => {},
      appUpdateCheck: () => Promise.resolve({ kind: "unsupported" }),
      appUpdateInstall: () => Promise.resolve(),
      onAppUpdateCheckRequested: () => () => {},
      onAppUpdateProgress: () => () => {},
    },
  }));
}

async function renderScreen() {
  const { SettingsScreen } = await import("@/components/settings/settings-screen");
  return render(<SettingsScreen client={null} events={null} onNavigateBack={() => {}} />);
}

describe("SettingsScreen desktop chrome", () => {
  it("the sidebar strip and content header are drag regions in the desktop build", async () => {
    mockPlatform({ desktop: true, os: "macos" });
    await renderScreen();

    expect(screen.getByTestId("settings-sidebar-top")).toHaveAttribute("data-tauri-drag-region", "deep");
    const header = screen.getByTestId("settings-screen-header");
    expect(header).toHaveAttribute("data-tauri-drag-region", "deep");
    // Interactive children never carry the attribute (Tauri's drag script
    // also refuses them, but they don't opt in).
    for (const el of header.querySelectorAll("button, input, [role=tab]")) {
      expect(el).not.toHaveAttribute("data-tauri-drag-region");
    }
  });

  it("renders window controls on Windows/Linux and pads the narrow header on macOS", async () => {
    // Windows/Linux desktop: caption buttons end the content header.
    mockPlatform({ desktop: true, os: "windows" });
    let rendered = await renderScreen();
    const header = screen.getByTestId("settings-screen-header");
    const controls = screen.getByTestId("desktop-window-controls");
    expect(header.lastElementChild).toBe(controls);
    expect(header.style.paddingLeft).toBe("");
    rendered.unmount();
    vi.resetModules();

    // macOS, narrow: the full-width header pads clear of the traffic lights.
    mockPlatform({ desktop: true, os: "macos" });
    const { TRAFFIC_LIGHT_CLUSTER_PX } = await import("@/lib/window-chrome");
    Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: 390 });
    window.matchMedia = vi.fn().mockImplementation(
      (query: string) =>
        ({
          matches: window.innerWidth < 768,
          media: query,
          onchange: null,
          addEventListener: () => {},
          removeEventListener: () => {},
          addListener: () => {},
          removeListener: () => {},
          dispatchEvent: () => false,
        }) as unknown as MediaQueryList,
    );
    const { act } = await import("react");
    rendered = await renderScreen();
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    const narrowHeader = screen.getByTestId("settings-screen-header");
    expect(narrowHeader.style.paddingLeft).toBe(`${8 + TRAFFIC_LIGHT_CLUSTER_PX}px`);
    expect(narrowHeader).toHaveAttribute("data-tauri-drag-region", "deep");
    expect(screen.queryByTestId("desktop-window-controls")).not.toBeInTheDocument();
    rendered.unmount();

    // Restore jsdom's default (1024) so nothing leaks within this file.
    Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: 1024 });
  });
});
