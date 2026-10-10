import { isDesktop } from "@/lib/platform";

/**
 * D2.2 (desktop-native-feel plan): replaces the webview's default
 * right-click menu (Back / Reload / Inspect) with native-app behavior.
 * A single bubble-phase `contextmenu` listener on the document --
 * deliberately NOT in App.tsx, per the plan's D1/D2 ownership split --
 * skips events someone closer to the target already handled (a smind
 * ContextMenu's onContextMenu, a component that called preventDefault
 * itself) and events on editable surfaces, and prevents everything else.
 *
 * Installed from main.tsx via installDesktopContextMenu, a no-op unless
 * isDesktop (lib/platform.ts).
 */

/**
 * True when e's target sits inside an editable field (an input, a
 * textarea, or anything the browser considers contenteditable --
 * `isContentEditable`, which already excludes `contenteditable="false"`
 * subtrees, e.g. a read-only CodeMirror), so the platform's native
 * Cut/Copy/Paste menu should stay.
 */
function isEditableTarget(e: Event): boolean {
  if (!(e.target instanceof HTMLElement)) return false;
  if (e.target.closest("input, textarea") !== null) return true;
  if (e.target.isContentEditable) return true;
  // jsdom never computes isContentEditable; the nearest [contenteditable]
  // host's value decides there (and matches real browsers for the nested
  // contenteditable="false" case too).
  const host = e.target.closest("[contenteditable]");
  if (!host) return false;
  const value = host.getAttribute("contenteditable");
  return value !== null && value !== "false";
}

/**
 * True when the click lands on a non-empty selection, so Copy should stay
 * available. A selection anywhere else on the page must NOT re-enable the
 * default menu -- otherwise right-clicking the sidebar while timeline text
 * happens to be selected brings back Reload/Inspect.
 *
 * Primary check: the click point inside one of the range's client rects.
 * Fallback (jsdom has no layout, so getClientRects() is empty there, and
 * it also covers a zero-area rects edge case in a real browser): the range
 * intersects the event's target node.
 */
function isOverSelection(e: MouseEvent): boolean {
  const selection = window.getSelection();
  if (!selection || selection.rangeCount === 0) return false;
  for (let i = 0; i < selection.rangeCount; i++) {
    const range = selection.getRangeAt(i);
    if (range.collapsed) continue;
    // jsdom has no layout, so Range.getClientRects doesn't even exist --
    // treat it as "no rects" and fall through to intersectsNode below.
    const rects = typeof range.getClientRects === "function" ? Array.from(range.getClientRects()) : [];
    if (
      rects.some(
        (r) =>
          e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom,
      )
    ) {
      return true;
    }
    if (rects.length === 0 && e.target instanceof Node && range.intersectsNode(e.target)) {
      return true;
    }
  }
  return false;
}

/** The desktop context-menu policy itself -- exported for its unit tests. */
export function handleContextMenu(e: MouseEvent): void {
  if (e.defaultPrevented) return;
  if (isEditableTarget(e) || isOverSelection(e)) return;
  e.preventDefault();
}

/** Installs the D2.2 context-menu policy on the document; a no-op in the browser build. */
export function installDesktopContextMenu(): void {
  if (!isDesktop) return;
  document.addEventListener("contextmenu", handleContextMenu);
}
