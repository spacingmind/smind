/**
 * Pure layout maths for the drawn window chrome (desktop-native-feel D1),
 * kept free of React and of the platform module so it is plain-unit-testable.
 *
 * macOS keeps the native traffic lights (overlay title bar), floated over the
 * web content at the window's top-left; everything here is about keeping the
 * UI out from under them. Windows/Linux draw their own caption buttons inside
 * the header row instead (`DesktopWindowControls`), so they need no inset.
 */
import { SIDEBAR_ICON_WIDTH } from "@/hooks/use-sidebar-width";
import type { DesktopOS } from "@/lib/platform";

/** Right edge of the traffic-light cluster plus a clear 8px: lights start at x=16 (`TRAFFIC_LIGHT_POSITION` in `window_chrome.rs`) and, measured on macOS, end at x=76. */
export const TRAFFIC_LIGHT_CLUSTER_PX = 84;

/** The header row's height (`h-12`); the traffic lights are vertically centred on it. */
export const HEADER_HEIGHT_PX = 48;

/** The header's own horizontal padding (`p-2`), which an inline `paddingLeft` replaces. */
export const HEADER_BASE_PADDING_PX = 8;

/** The window-level sidebar toggle: an `icon-sm` button (`size-7`). */
export const SIDEBAR_TOGGLE_SIZE_PX = 28;

/** Clear space between the toggle and whatever follows it. */
export const SIDEBAR_TOGGLE_GAP_PX = 8;

/** One back/forward arrow button (an `icon-sm` button, same size as the toggle). */
export const NAV_BUTTON_SIZE_PX = 28;

/** Gap between the back and forward buttons. */
export const NAV_BUTTONS_GAP_PX = 4;

/** Space between the sidebar toggle and the first nav button. */
export const NAV_TOGGLE_GAP_PX = 4;

/** The back/forward pair's total width. */
export const NAV_BUTTONS_WIDTH_PX = 2 * NAV_BUTTON_SIZE_PX + NAV_BUTTONS_GAP_PX;

/** Native traffic lights are on screen: macOS and not in fullscreen (where macOS hides them). */
export function hasTrafficLights(os: DesktopOS | null, fullscreen: boolean): boolean {
  return os === "macos" && !fullscreen;
}

/** The drawn caption buttons are on screen: Windows/Linux. */
export function hasCaptionButtons(os: DesktopOS | null): boolean {
  return os === "windows" || os === "linux";
}

/**
 * Where the window-level sidebar toggle's left edge sits. It is one fixed
 * element (ZCode's top overlay / Paseo's window-level toggle), so it is at the
 * same x whether the sidebar is expanded, a rail or animating between: right
 * after the traffic lights on macOS, centred in the 48px rail otherwise
 * (Windows/Linux, and macOS fullscreen where the lights are gone).
 */
export function toggleLeftPx(os: DesktopOS | null, fullscreen: boolean): number {
  if (hasTrafficLights(os, fullscreen)) return TRAFFIC_LIGHT_CLUSTER_PX;
  return (SIDEBAR_ICON_WIDTH - SIDEBAR_TOGGLE_SIZE_PX) / 2;
}

/** Where the back/forward nav buttons' left edge sits: right after the window-level toggle. */
export function navLeftPx(os: DesktopOS | null, fullscreen: boolean): number {
  return toggleLeftPx(os, fullscreen) + SIDEBAR_TOGGLE_SIZE_PX + NAV_TOGGLE_GAP_PX;
}

/**
 * The x the first thing after the window-level cluster (toggle + nav
 * buttons) may start at.
 */
export function toggleZoneEndPx(os: DesktopOS | null, fullscreen: boolean): number {
  return navLeftPx(os, fullscreen) + NAV_BUTTONS_WIDTH_PX + SIDEBAR_TOGGLE_GAP_PX;
}

/**
 * How far the header row must pad in from its left edge, on top of
 * `HEADER_BASE_PADDING_PX`, so its content starts clear of what overlays the
 * window's top-left. `sidebarPx` is the width of whatever sits to the
 * header's left (the expanded/collapsed sidebar panel; 0 when the sidebar is
 * a hidden sheet, i.e. the header is at the window edge).
 *
 * With `reserveToggle` (the desktop shell, where the toggle is a window-level
 * overlay) the header clears the toggle zone: expanded, the sidebar's own top
 * strip already holds it so the header needs nothing; collapsed to the 48px
 * rail the toggle overhangs the header. Without it (compact, Settings) only
 * the lights need clearing, plus 8px.
 */
export function headerLeftInsetPx(
  os: DesktopOS | null,
  fullscreen: boolean,
  sidebarPx: number,
  reserveToggle = false,
): number {
  if (!reserveToggle && !hasTrafficLights(os, fullscreen)) return 0;
  const start = reserveToggle
    ? toggleZoneEndPx(os, fullscreen)
    : TRAFFIC_LIGHT_CLUSTER_PX + HEADER_BASE_PADDING_PX;
  return Math.max(0, start - sidebarPx - HEADER_BASE_PADDING_PX);
}
