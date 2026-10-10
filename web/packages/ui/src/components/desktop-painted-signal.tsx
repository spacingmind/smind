import { useEffect } from "react";

import { desktopWindow, isDesktop } from "@/lib/platform";

/**
 * Tells the shell the UI has rendered, so it can show the window it created
 * hidden (D2.1, no white flash). Fires on a timer after the first commit, not
 * on `requestAnimationFrame`: a hidden window's webview suspends animation
 * frames, so waiting for one would only ever hit the shell's 3 s fallback.
 * By this point the DOM and stylesheet are in place, and until the compositor
 * catches up the window shows its theme-coloured background, never white.
 */
export function DesktopPaintedSignal() {
  useEffect(() => {
    if (!isDesktop) return;
    const timer = setTimeout(() => {
      desktopWindow.notifyPainted().catch(() => {});
    }, 0);
    return () => clearTimeout(timer);
  }, []);
  return null;
}
