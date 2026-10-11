// Constants shared by lib/platform.ts and the local-path hook. They live
// outside platform.ts so tests that mock "@/lib/platform" wholesale don't
// have to re-export them.

/** The id of the platform file manager in `editors_list`'s result -- Reveal passes this to `openInEditor`. */
export const FILE_MANAGER_EDITOR_ID = "file-manager";

/** Fired on `window` after the saved/current connection changes (select/add/remove): switching connections doesn't reload the page, so anything derived from the current connection's kind must re-read it. */
export const CONNECTION_CHANGED_EVENT = "smind:connection-changed";
