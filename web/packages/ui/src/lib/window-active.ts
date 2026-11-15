/**
 * Mirrors "is this window the key window" onto `<html data-window-active="true|false">`, which desktop-chrome.css reads
 * to dim the sidebar selection and icons while the window is in the background (the macOS convention). The webview
 * reports it through ordinary window focus and blur. A blur that only moves focus into an iframe (a preview, the
 * terminal) leaves `document.hasFocus()` true, so it is re-read on the next tick instead of trusting the event.
 * Returns the cleanup.
 */
export function trackWindowActive(root: HTMLElement = document.documentElement, win: Window = window): () => void {
  const apply = () => {
    root.dataset.windowActive = win.document.hasFocus() ? "true" : "false";
  };
  let pending: ReturnType<typeof setTimeout> | null = null;
  const settle = () => {
    if (pending !== null) clearTimeout(pending);
    pending = setTimeout(() => {
      pending = null;
      apply();
    }, 0);
  };
  apply();
  win.addEventListener("focus", settle);
  win.addEventListener("blur", settle);
  return () => {
    if (pending !== null) clearTimeout(pending);
    win.removeEventListener("focus", settle);
    win.removeEventListener("blur", settle);
    delete root.dataset.windowActive;
  };
}
