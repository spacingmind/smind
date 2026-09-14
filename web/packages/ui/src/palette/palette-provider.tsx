import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

import { flattenSources, type Command, type CommandSource, type RankedCommand } from "@/palette/commands";

/**
 * The command registry: sources register command lists, the palette reads
 * whatever is registered.
 *
 * ## The registration API other surfaces use
 *
 * ```tsx
 * const commands = useMemo<Command[]>(
 *   () => tasks.map((t) => ({
 *     id: `task-${t.ID}`,
 *     group: "Tasks",
 *     title: t.Title,
 *     subtitle: t.Branch,
 *     run: () => selectTask(t),
 *   })),
 *   [tasks, selectTask],
 * );
 * useCommands("shell:tasks", 0, commands);
 * ```
 *
 * `components/command-palette.tsx` is never edited to add an entry --
 * that is Item 5's "contributions are registered, not hardcoded"
 * criterion, and it's what lets Item 18's worktree file index and Item
 * 13's settings entries land without touching this track's code.
 *
 * ## Why the registry lives in a ref rather than in state
 *
 * A source that rebuilds its array every render (a caller who forgot
 * `useMemo`) would, with state-backed registration, loop forever:
 * register → state change → re-render → new array → effect re-runs →
 * register. Holding registrations in a ref makes registering render-free,
 * so the worst an un-memoized caller costs is a Map write per render.
 *
 * Subscribers are still notified, but only when the registered commands'
 * *signature* (ids and titles) actually changes -- so an open palette
 * updates when a task appears, and doesn't thrash when a caller hands it
 * an equal-but-new array. The palette reads `listCommands()` at render
 * time, so the `run` closures it invokes are always the current ones,
 * never a snapshot taken when the signature last changed.
 */

interface PaletteContextValue {
  /** Registers `commands` under `sourceId`, replacing any previous set. Returns the unregister function. */
  register: (sourceId: string, groupRank: number, commands: readonly Command[]) => () => void;
  /** Every registered command, flattened and ordered. Read at render time. */
  listCommands: () => RankedCommand[];
  subscribe: (listener: () => void) => () => void;
  open: boolean;
  setOpen: (open: boolean) => void;
}

/** A standalone no-op default, so a component that registers commands still mounts bare in its own unit test -- same tradeoff as `useTheme` and `useActionHandler`. */
const PaletteContext = createContext<PaletteContextValue>({
  register: () => () => {},
  listCommands: () => [],
  subscribe: () => () => {},
  open: false,
  setOpen: () => {},
});

/** Ids + titles of everything registered, in order: what "did the visible command set change" means. */
function signature(sources: readonly CommandSource[]): string {
  return sources
    .map((s) => `${s.id}#${s.groupRank}:${s.commands.map((c) => `${c.id}|${c.title}`).join(",")}`)
    .join(";");
}

export function PaletteProvider({ children }: { children: ReactNode }) {
  const sources = useRef(new Map<string, CommandSource>());
  const listeners = useRef(new Set<() => void>());
  const lastSignature = useRef("");
  const [open, setOpen] = useState(false);

  const orderedSources = useCallback(
    () => [...sources.current.values()].sort((a, b) => a.groupRank - b.groupRank),
    [],
  );

  const notifyIfChanged = useCallback(() => {
    const next = signature(orderedSources());
    if (next === lastSignature.current) return;
    lastSignature.current = next;
    for (const listener of listeners.current) listener();
  }, [orderedSources]);

  const register = useCallback(
    (sourceId: string, groupRank: number, commands: readonly Command[]) => {
      sources.current.set(sourceId, { id: sourceId, groupRank, commands });
      notifyIfChanged();
      return () => {
        sources.current.delete(sourceId);
        notifyIfChanged();
      };
    },
    [notifyIfChanged],
  );

  const listCommands = useCallback(() => flattenSources(orderedSources()), [orderedSources]);

  const subscribe = useCallback((listener: () => void) => {
    listeners.current.add(listener);
    return () => listeners.current.delete(listener);
  }, []);

  const value = useMemo<PaletteContextValue>(
    () => ({ register, listCommands, subscribe, open, setOpen }),
    [register, listCommands, subscribe, open],
  );

  return <PaletteContext.Provider value={value}>{children}</PaletteContext.Provider>;
}

export function usePalette(): PaletteContextValue {
  return useContext(PaletteContext);
}

/**
 * Contributes `commands` to the palette for as long as this component is
 * mounted.
 *
 * Memoize `commands` (`useMemo`) so registration happens when the set
 * really changes rather than every render. Forgetting to is survivable --
 * see the provider's doc comment -- but it does re-register per render.
 */
export function useCommands(
  sourceId: string,
  groupRank: number,
  commands: readonly Command[],
): void {
  const { register } = usePalette();
  useEffect(() => register(sourceId, groupRank, commands), [register, sourceId, groupRank, commands]);
}

/** Re-renders the caller whenever the registered command *set* changes, and returns the current commands. */
export function useRegisteredCommands(): RankedCommand[] {
  const { listCommands, subscribe } = usePalette();
  const [, bump] = useState(0);

  useEffect(() => subscribe(() => bump((n) => n + 1)), [subscribe]);

  // Deliberately not memoized on the version counter: reading at render
  // time is what keeps every `run` closure current even when the
  // signature didn't change (a task's title is the same, but `selectTask`
  // closed over newer state).
  return listCommands();
}
