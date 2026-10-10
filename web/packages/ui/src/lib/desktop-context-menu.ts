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

/** True when e's target sits inside an editable field (input, textarea, or contenteditable), so the platform's native Cut/Copy/Paste menu should stay. */
function isEditableTarget(e: Event): boolean {
  if (!(e.target instanceof Element)) return false;
  const editable = e.target.closest(
    "input, textarea, [contenteditable], [contenteditable] *, .cm-content",
  );
  if (!editable) return false;
  return !(editable.hasAttribute("contenteditable") && editable.getAttribute("contenteditable") === "false");
}

/** True when the caret sits inside a non-empty collapsed selection, so Copy/Select All should stay available. */
function hasNonEmptySelection(): boolean {
  const selection = window.getSelection();
  if (!selection || selection.rangeCount === 0) return false;
  for (let i = 0; i < selection.rangeCount; i++) {
    if (!selection.getRangeAt(i).collapsed) return true;
  }
  return false;
}

/** The desktop context-menu policy itself -- exported for its unit tests. */
export function handleContextMenu(e: MouseEvent): void {
  if (e.defaultPrevented) return;
  if (isEditableTarget(e) || hasNonEmptySelection()) return;
  e.preventDefault();
}

/** Installs the D2.2 context-menu policy on the document; a no-op in the browser build. */
export function installDesktopContextMenu(): void {
  if (!isDesktop) return;
  document.addEventListener("contextmenu", handleContextMenu);
}
