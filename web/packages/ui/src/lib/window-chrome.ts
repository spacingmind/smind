/**
 * Pure layout maths for the drawn window chrome (desktop-native-feel D1),
 * kept free of React and of the platform module so it is plain-unit-testable.
 *
 * macOS keeps the native traffic lights (overlay title bar), floated over the
 * web content at the window's top-left; everything here is about keeping the
 * UI out from under them. Windows/Linux draw their own caption buttons inside
 * the header row instead (`DesktopWindowControls`), so they need no inset.
 */
import type { DesktopOS } from "@/lib/platform";

/** Right edge of the traffic-light cluster plus a clear 8px: lights start at x=16 (`TRAFFIC_LIGHT_POSITION` in `window_chrome.rs`) and, measured on macOS, end at x=76. */
export const TRAFFIC_LIGHT_CLUSTER_PX = 84;

/** Width of the sidebar/content resize handle that sits between the sidebar panel and the header. */
export const SIDEBAR_HANDLE_PX = 4;

/** The header row's height (`h-12`); the traffic lights are vertically centred on it. */
export const HEADER_HEIGHT_PX = 48;

/** Native traffic lights are on screen: macOS and not in fullscreen (where macOS hides them). */
export function hasTrafficLights(os: DesktopOS | null, fullscreen: boolean): boolean {
  return os === "macos" && !fullscreen;
}

/** The drawn caption buttons are on screen: Windows/Linux. */
export function hasCaptionButtons(os: DesktopOS | null): boolean {
  return os === "windows" || os === "linux";
}

/**
 * How far the header row must pad in from its left edge to clear the traffic
 * lights. `sidebarPx` is the width of whatever sits to the header's left
 * (the expanded/collapsed sidebar panel; 0 when the sidebar is a hidden
 * sheet, i.e. the header is at the window edge). Expanded, the sidebar's own
 * top strip already holds the lights, so the header needs nothing; collapsed
 * to the 48px rail they overhang it; with no sidebar they sit on the header.
 */
export function headerLeftInsetPx(os: DesktopOS | null, fullscreen: boolean, sidebarPx: number): number {
  if (!hasTrafficLights(os, fullscreen)) return 0;
  const handle = sidebarPx > 0 ? SIDEBAR_HANDLE_PX : 0;
  return Math.max(0, TRAFFIC_LIGHT_CLUSTER_PX - sidebarPx - handle);
}
