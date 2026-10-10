import { isDesktop } from "@/lib/platform";

/**
 * D2's webview-taming layer (desktop-native-feel plan): the zoom guard
 * that keeps the webview's own pinch/Ctrl+wheel zoom away from the user
 * (D2.5 -- smind's View → Zoom level is the only zoom).
 *
 * The desktop root marker itself (`html[data-desktop-os]`, which D2's
 * index.css rules and D1's desktop-chrome.css both gate on) is set by
 * main.tsx from lib/platform.ts's desktopOS, not here.
 *
 * Everything here is installed by main.tsx via
 * installDesktopWebviewDefaults, which is a no-op unless isDesktop
 * (lib/platform.ts), so the browser build is untouched.
 */

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

/** Installs D2's zoom guard; a no-op in the browser build. */
export function installDesktopWebviewDefaults(): void {
  if (!isDesktop) return;
  installDesktopZoomGuard();
}
