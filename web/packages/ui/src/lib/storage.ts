/**
 * The one way this app persists a UI preference.
 *
 * Item 3's criterion is that sidebar width, open tabs and (later) pane
 * sizes all use the same mechanism rather than three hand-rolled
 * `try { localStorage } catch {}` blocks that each decide differently what
 * to do on a parse failure. Every caller gets the same three guarantees:
 *
 * - Reads never throw. Storage being unavailable (private browsing,
 *   disabled, quota), holding malformed JSON, or holding well-formed JSON
 *   of the wrong shape all produce the fallback. A layout preference must
 *   never be able to take down the app.
 * - Writes are best-effort. A failed write leaves the value applying for
 *   the rest of the session.
 * - Shapes are validated on read, not trusted. A stale build's payload is
 *   the normal case after a deploy, not an exotic one.
 *
 * Keys are namespaced `smind:` -- the daemon serves the UI from whatever
 * origin the user reached it on, which may be shared with something else.
 */

/** Every key this app persists under. One list, so a key collision is visible. */
export const STORAGE_KEYS = {
  sidebarWidth: "smind:sidebar-width",
  theme: "smind:theme",
  shortcutOverrides: "smind:shortcut-overrides",
  taskTabs: "smind:task-tabs",
  sidePaneWidth: "smind:side-pane-width",
  pinnedTasks: "smind:pinned-tasks",
  unreadTasks: "smind:unread-tasks",
  sidebarGroupMode: "smind:sidebar-group-mode",
  notificationSound: "smind:notification-sound",
} as const;

/**
 * Reads and validates a JSON value, falling back on anything unexpected.
 *
 * `validate` is a type guard, not a schema: it should check the parts the
 * caller actually reads. Returning the *parsed* value (rather than a
 * merge with the fallback) keeps "what got stored" and "what got read"
 * the same thing, which is what makes a round-trip test meaningful.
 */
export function readStored<T>(key: string, fallback: T, validate: (value: unknown) => value is T): T {
  if (typeof window === "undefined") return fallback;
  try {
    const raw = window.localStorage.getItem(key);
    if (raw === null) return fallback;
    const parsed: unknown = JSON.parse(raw);
    return validate(parsed) ? parsed : fallback;
  } catch {
    return fallback;
  }
}

/** Writes a JSON value. Pass `undefined` to remove the key. */
export function writeStored(key: string, value: unknown): void {
  if (typeof window === "undefined") return;
  try {
    if (value === undefined) window.localStorage.removeItem(key);
    else window.localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Best-effort only -- see this file's doc comment.
  }
}
