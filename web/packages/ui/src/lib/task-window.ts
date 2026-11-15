/**
 * Whether this webview is a TASK window (desktop-multi-window Part 1): a window the shell opened for one task, labelled
 * `task-1`..`task-8`. It runs the same bundled UI as `main` but holds a smaller IPC capability (ADR-0013 amendment), so
 * the surfaces that reach for main-only commands (the connection and daemon controls, the updater, Settings) are not
 * offered there. Read from the window label Tauri injects, with no import from `platform.ts` (mocked in many tests).
 */
export function isTaskWindowLabel(label: unknown): boolean {
  return typeof label === "string" && /^task-[1-8]$/.test(label);
}

function currentWindowLabel(): unknown {
  try {
    const internals = (window as unknown as { __TAURI_INTERNALS__?: { metadata?: { currentWindow?: { label?: unknown } } } }).__TAURI_INTERNALS__;
    return internals?.metadata?.currentWindow?.label;
  } catch {
    return undefined;
  }
}

/** True only in a desktop build running in a task window. */
export const isTaskWindow: boolean = import.meta.env.VITE_SMIND_DESKTOP === "1" && typeof window !== "undefined" && isTaskWindowLabel(currentWindowLabel());
