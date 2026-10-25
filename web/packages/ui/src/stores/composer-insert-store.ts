import { create } from "zustand";

/**
 * "Insert command: /x" from the command palette: the palette puts a request here, the composer that last had
 * focus applies it to its draft (appended after a blank line when the draft is non-empty), focuses the textarea
 * and never sends. Not persisted. `key` names a composer (`<taskId>:<chatId>`); a composer registers itself while
 * mounted, so with none mounted the palette offers no entry.
 */
interface ComposerInsertState {
  /** The composers mounted right now. */
  mounted: readonly string[];
  /** The composer that last had focus (still mounted), or null. */
  target: string | null;
  request: Readonly<{ key: string; text: string; nonce: number }> | null;
  /** Registers a composer; returns its unregister. */
  register: (key: string) => () => void;
  setTarget: (key: string) => void;
  /** Asks the target composer to insert `text`; false when there is none. */
  insert: (text: string) => boolean;
}

export const useComposerInsertStore = create<ComposerInsertState>()((set, get) => ({
  mounted: [],
  target: null,
  request: null,
  register: (key) => {
    set((state) => ({ mounted: state.mounted.includes(key) ? state.mounted : [...state.mounted, key], target: state.target ?? key }));
    return () =>
      set((state) => {
        const mounted = state.mounted.filter((k) => k !== key);
        return { mounted, target: state.target === key ? (mounted[mounted.length - 1] ?? null) : state.target };
      });
  },
  setTarget: (key) => set({ target: key }),
  insert: (text) => {
    const { target, request } = get();
    if (target === null) return false;
    set({ request: { key: target, text, nonce: (request?.nonce ?? 0) + 1 } });
    return true;
  },
}));
