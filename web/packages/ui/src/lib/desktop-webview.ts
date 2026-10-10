import { isDesktop } from "@/lib/platform";

/**
 * D2's webview-taming root layer (desktop-native-feel plan): the
 * desktop-only markers on <html> that index.css's D2 rules hang off of,
 * plus the zoom guard that keeps the webview's own pinch/Ctrl+wheel zoom
 * away from the user (D2.5 -- smind's View → Zoom level is the only zoom).
 *
 * Everything here is installed by main.tsx via installDesktopWebviewDefaults,
 * which is a no-op unless isDesktop (lib/platform.ts), so the browser build
 * is untouched.
 */

/** The OS the desktop webview is running on, exposed on <html> as `data-smind-desktop-os`. */
export type DesktopOs = "macos" | "windows" | "linux" | "unknown";

/** Maps a user agent to the DesktopOs whose platform-specific D2 rules apply (D2.7's Windows-only scrollbars). */
export function detectDesktopOs(userAgent: string = navigator.userAgent): DesktopOs {
  if (/Windows/i.test(userAgent)) return "windows";
  if (/Mac OS X/i.test(userAgent)) return "macos";
  if (/Linux|X11/i.test(userAgent)) return "linux";
  return "unknown";
}

/** D2.8's gating hooks: sets `data-smind-desktop` and `data-smind-desktop-os` on the root element, once at startup. */
export function applyDesktopRootMarkers(root: HTMLElement = document.documentElement): void {
  root.setAttribute("data-smind-desktop", "");
  root.setAttribute("data-smind-desktop-os", detectDesktopOs());
}

/** WKWebView's non-standard trackpad-pinch events; preventing them is the only web-side way to disable pinch zoom there. */
const GESTURE_EVENT_TYPES = ["gesturestart", "gesturechange", "gestureend"] as const;

/**
 * D2.5: the desktop build's only zoom is smind's own View → Zoom level
 * (persisted Rust-side in zoom_store.rs). The webview's built-ins --
 * Ctrl/Cmd+mouse-wheel, and trackpad pinch (WebKit's gesture* events;
 * Chromium/WebView2 reports pinch as a synthetic ctrlKey wheel event, which
 * the same wheel branch covers) -- are prevented here instead of via a
 * Rust window setting, per the plan's ownership split (D2 does not touch
 * lib.rs). passive:false, because preventDefault from a passive listener
 * is ignored and browsers default document-level wheel listeners to
 * passive.
 */
export function installDesktopZoomGuard(root: HTMLElement = document.documentElement): void {
  root.addEventListener(
    "wheel",
    (event) => {
      if (event.ctrlKey || event.metaKey) event.preventDefault();
    },
    { passive: false },
  );
  for (const type of GESTURE_EVENT_TYPES) {
    root.addEventListener(
      type,
      (event) => {
        event.preventDefault();
      },
      { passive: false },
    );
  }
}

/** Installs D2's root markers and zoom guard; a no-op in the browser build. */
export function installDesktopWebviewDefaults(): void {
  if (!isDesktop) return;
  applyDesktopRootMarkers();
  installDesktopZoomGuard();
}
