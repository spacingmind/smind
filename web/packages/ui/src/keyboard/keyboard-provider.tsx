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

import type { ActionHandler, ActionId, ActionPayload } from "@/keyboard/actions";
import { resolveFocusScope } from "@/keyboard/focus-scope";
import { isMacPlatform } from "@/keyboard/shortcut-string";
import { desktop, isDesktop } from "@/lib/platform";
import {
  clearOverride,
  readStoredOverrides,
  setOverride,
  writeStoredOverrides,
} from "@/keyboard/overrides";
import {
  CHORD_TIMEOUT_MS,
  INITIAL_CHORD_STATE,
  platformBindings,
  resolveBindings,
  resolveChordStep,
  SHORTCUT_BINDINGS,
  type ChordState,
  type ResolvedBinding,
  type ShortcutOverrides,
} from "@/keyboard/shortcuts";

/**
 * The keyboard layer's React half: one window-level `keydown` listener that
 * turns a key event into an {@link ActionId}, and a registry that maps
 * action ids to whatever component is currently able to perform them.
 *
 * ## The registration API other tracks use
 *
 * ```tsx
 * useActionHandler("composer.focus", () => textareaRef.current?.focus());
 * useActionHandler("run.interrupt", stopRun, { enabled: runIsLive });
 * ```
 *
 * Handlers are stacked per action: the **most recently registered enabled**
 * handler wins. That ordering is deliberate. React registers effects
 * child-first, so the innermost mounted component -- the composer of the
 * task actually on screen, not a stale one -- ends up last in the stack
 * and therefore claims the action. `enabled: false` keeps a handler
 * registered but skipped, so a composer with no live run passes
 * `run.interrupt` down to whoever else wants it instead of swallowing it.
 *
 * A component registering a handler needs to know nothing about keys,
 * platforms, or focus scopes, and never edits `keyboard/shortcuts.ts`.
 * That is the whole point: Tracks B/C/D claim actions; Track A owns which
 * keys reach them.
 */

interface HandlerEntry {
  token: symbol;
  handler: ActionHandler;
  enabled: boolean;
}

interface KeyboardContextValue {
  bindings: ResolvedBinding[];
  isMac: boolean;
  overrides: ShortcutOverrides;
  /** Rebinds `bindingId`. Pass `""` to unassign it; the row stays in the help dialog matching nothing. */
  rebind: (bindingId: string, combo: string) => void;
  /** Drops `bindingId`'s override, restoring its shipped combo. */
  resetBinding: (bindingId: string) => void;
  /** Drops every override. */
  resetAllBindings: () => void;
  /** Registers `handler` for `action`; returns the unregister function. Prefer {@link useActionHandler}. */
  register: (action: ActionId, handler: ActionHandler, enabled: boolean) => () => void;
  /** Runs `action`'s current handler as if its shortcut fired. The command palette invokes actions this way. */
  runAction: (action: ActionId, payload?: ActionPayload) => boolean;
  /** True while at least one dialog owns the keyboard -- no binding fires. Dialogs claim it via {@link useModalKeyboardLock}. */
  modalOpen: boolean;
  /** Takes a modal lock; call the returned function to release it. Nested/overlapping dialogs are counted, not toggled. */
  acquireModalLock: () => () => void;
}

/** How close together two runs of the same action must be for the second to count as a duplicate of the first. */
const DEDUPE_WINDOW_MS = 300;

/**
 * A standalone, fully-functional default -- the same tradeoff
 * `hooks/use-theme.tsx` documents. This codebase's component tests render
 * one component directly rather than the whole app tree, so a component
 * that claims an action (a composer, a pane) must still mount in its own
 * suite without a `<KeyboardProvider>`. Outside a provider, registration
 * is a no-op and nothing ever dispatches, which is exactly right for a
 * test that isn't exercising keyboard routing.
 */
const KeyboardContext = createContext<KeyboardContextValue>({
  bindings: resolveBindings(),
  isMac: false,
  overrides: {},
  rebind: () => {},
  resetBinding: () => {},
  resetAllBindings: () => {},
  register: () => () => {},
  runAction: () => false,
  modalOpen: false,
  acquireModalLock: () => () => {},
});

export function KeyboardProvider({
  children,
  /** Overridable for tests -- the same escape hatch `App`'s `connect` prop uses. */
  readOverrides = readStoredOverrides,
  writeOverrides = writeStoredOverrides,
}: {
  children: ReactNode;
  readOverrides?: () => ShortcutOverrides;
  writeOverrides?: (overrides: ShortcutOverrides) => void;
}) {
  const [overrides, setOverrides] = useState<ShortcutOverrides>(() => readOverrides());
  // A count, not a boolean: two overlapping dialogs (the palette opening
  // the shortcuts help, say) would otherwise have the first one to close
  // release a lock the second still needs.
  const [modalCount, setModalCount] = useState(0);
  const modalOpen = modalCount > 0;

  const acquireModalLock = useCallback(() => {
    setModalCount((n) => n + 1);
    let released = false;
    return () => {
      if (released) return;
      released = true;
      setModalCount((n) => Math.max(0, n - 1));
    };
  }, []);

  // Handlers live in a ref, not state: registering one must not re-render
  // the whole subtree under the provider, and the dispatcher only ever
  // reads them at the moment a key is pressed.
  const handlers = useRef(new Map<ActionId, HandlerEntry[]>());

  // D3.1: a row gated `when.desktop` exists only on its own platform, so
  // it neither matches keys nor appears in the help list on the other one.
  const bindings = useMemo(
    () => resolveBindings(platformBindings(SHORTCUT_BINDINGS, isDesktop), overrides),
    [overrides],
  );

  // Read per dispatch rather than memoized at mount: tests reassign
  // `navigator.platform` between cases, and the cost is one regex.
  const isMac = isMacPlatform();

  const register = useCallback((action: ActionId, handler: ActionHandler, enabled: boolean) => {
    const token = Symbol("handler");
    const stack = handlers.current.get(action) ?? [];
    stack.push({ token, handler, enabled });
    handlers.current.set(action, stack);
    return () => {
      const current = handlers.current.get(action);
      if (!current) return;
      const next = current.filter((entry) => entry.token !== token);
      if (next.length === 0) handlers.current.delete(action);
      else handlers.current.set(action, next);
    };
  }, []);

  const runAction = useCallback((action: ActionId, payload: ActionPayload = null) => {
    const stack = handlers.current.get(action);
    if (!stack) return false;
    for (let i = stack.length - 1; i >= 0; i--) {
      const entry = stack[i]!;
      if (!entry.enabled) continue;
      entry.handler(payload);
      return true;
    }
    return false;
  }, []);

  // `modalOpen` is read inside a listener registered once, so it goes
  // through a ref -- re-subscribing the listener on every open/close would
  // drop and re-add a window handler on each dialog toggle for no benefit.
  const modalOpenRef = useRef(modalOpen);
  modalOpenRef.current = modalOpen;
  const bindingsRef = useRef(bindings);
  bindingsRef.current = bindings;
  const isMacRef = useRef(isMac);
  isMacRef.current = isMac;

  // A chord attempt lives in refs, not state: it must survive a re-render
  // between two chord steps (a class of bug in Paseo's own history) without
  // the listener itself being torn down and re-added.
  const chordStateRef = useRef<ChordState>(INITIAL_CHORD_STATE);
  const chordTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // D3.3, no double fire: when a native menu item carries an accelerator
  // (macOS), the OS delivers the keystroke to the menu -- but the webview
  // may *also* report the keydown to the page, so the same action can
  // arrive from both sources within a few milliseconds. Whichever source
  // runs an action first wins; a copy of the same action from the other
  // source within DEDUPE_WINDOW_MS is ignored. A time window (rather than
  // "skip keydown for menu-owned combos") holds on every platform,
  // including the Windows/Linux menu items that have no accelerator at
  // all, and stays correct even if the menu's accelerator set changes.
  const lastRunRef = useRef<{ action: ActionId; at: number; source: "keydown" | "menu" } | null>(
    null,
  );

  const claimAction = useCallback((action: ActionId, source: "keydown" | "menu") => {
    const now = Date.now();
    const last = lastRunRef.current;
    if (
      last &&
      last.action === action &&
      last.source !== source &&
      now - last.at < DEDUPE_WINDOW_MS
    ) {
      return false;
    }
    lastRunRef.current = { action, at: now, source };
    return true;
  }, []);

  useEffect(() => {
    function resetChord() {
      if (chordTimeoutRef.current !== null) {
        clearTimeout(chordTimeoutRef.current);
        chordTimeoutRef.current = null;
      }
      chordStateRef.current = INITIAL_CHORD_STATE;
    }

    function onKeyDown(event: KeyboardEvent) {
      const scope = resolveFocusScope(event.target, modalOpenRef.current);
      const resolution = resolveChordStep(
        bindingsRef.current,
        event,
        { isMac: isMacRef.current, scope },
        chordStateRef.current,
      );
      chordStateRef.current = resolution.nextChordState;

      if (resolution.nextChordState.step === 0) {
        // The attempt finished, cancelled, or a bare keydown never started
        // one -- either way nothing is pending any more.
        if (chordTimeoutRef.current !== null) {
          clearTimeout(chordTimeoutRef.current);
          chordTimeoutRef.current = null;
        }
      } else if (resolution.pending) {
        // Advanced to (or started) a new step: restart the window. A bare
        // modifier keydown mid-chord also leaves step > 0 but is not
        // `pending` -- it changes nothing, so its existing timeout is left
        // running rather than restarted here.
        if (chordTimeoutRef.current !== null) clearTimeout(chordTimeoutRef.current);
        chordTimeoutRef.current = setTimeout(resetChord, CHORD_TIMEOUT_MS);
      }

      if (resolution.pending) {
        // A chord's non-final key(s) must not reach the page (or a focused
        // textarea) even though no action has fired yet.
        event.preventDefault();
        event.stopPropagation();
        return;
      }

      if (!resolution.match) return;

      // preventDefault only once a handler actually took the event: a
      // binding with nobody listening (no task selected, so nothing claims
      // `tab.close`) must leave the browser's own Cmd+W alone rather than
      // swallowing it into a no-op.
      const stack = handlers.current.get(resolution.match.action);
      if (!stack?.some((entry) => entry.enabled)) return;

      event.preventDefault();
      event.stopPropagation();
      if (claimAction(resolution.match.action, "keydown")) {
        runAction(resolution.match.action, resolution.match.payload);
      }
    }

    // Capture phase: xterm and CodeMirror both attach their own keydown
    // handlers and stop propagation on keys they claim, so a bubble-phase
    // listener would never see (for instance) Escape typed into the
    // composer. Capture sees it first; `resolveFocusScope` is what decides
    // whether it's ours to take, not listener ordering.
    window.addEventListener("keydown", onKeyDown, true);
    return () => {
      window.removeEventListener("keydown", onKeyDown, true);
      if (chordTimeoutRef.current !== null) clearTimeout(chordTimeoutRef.current);
    };
  }, [runAction, claimAction]);

  // D3.3: a native menu item click (or its accelerator, which the OS
  // routes to the menu) arrives as one `menu-action` event and is
  // dispatched through the same registry a keystroke uses. A modal owns
  // the keyboard, so its gate applies here too. `claimAction` is what
  // keeps an accelerator that also reached the webview from firing twice.
  useEffect(() => {
    if (!isDesktop) return;
    return desktop.onMenuAction((id) => {
      if (modalOpenRef.current) return;
      if (claimAction(id, "menu")) runAction(id);
    });
  }, [runAction, claimAction]);

  const rebind = useCallback(
    (bindingId: string, combo: string) => {
      setOverrides((prev) => {
        const next = setOverride(prev, bindingId, combo);
        writeOverrides(next);
        return next;
      });
    },
    [writeOverrides],
  );

  const resetBinding = useCallback(
    (bindingId: string) => {
      setOverrides((prev) => {
        const next = clearOverride(prev, bindingId);
        if (next === prev) return prev;
        writeOverrides(next);
        return next;
      });
    },
    [writeOverrides],
  );

  const resetAllBindings = useCallback(() => {
    setOverrides((prev) => {
      if (Object.keys(prev).length === 0) return prev;
      writeOverrides({});
      return {};
    });
  }, [writeOverrides]);

  const value = useMemo<KeyboardContextValue>(
    () => ({
      bindings,
      isMac,
      overrides,
      rebind,
      resetBinding,
      resetAllBindings,
      register,
      runAction,
      modalOpen,
      acquireModalLock,
    }),
    [
      bindings,
      isMac,
      overrides,
      rebind,
      resetBinding,
      resetAllBindings,
      register,
      runAction,
      modalOpen,
      acquireModalLock,
    ],
  );

  return <KeyboardContext.Provider value={value}>{children}</KeyboardContext.Provider>;
}

export function useKeyboard(): KeyboardContextValue {
  return useContext(KeyboardContext);
}

/**
 * Claims `action` for as long as this component is mounted.
 *
 * `handler` is read through a ref, so it may be a fresh closure every
 * render without re-registering -- callers don't need `useCallback`.
 * `enabled` (default `true`) keeps the registration but makes the
 * dispatcher skip it, so an action falls through to an outer handler
 * instead of being swallowed by a component that can't currently perform
 * it.
 */
export function useActionHandler(
  action: ActionId,
  handler: ActionHandler,
  options: { enabled?: boolean } = {},
): void {
  const enabled = options.enabled ?? true;
  const { register } = useKeyboard();
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  useEffect(() => {
    return register(action, (payload) => handlerRef.current(payload), enabled);
  }, [register, action, enabled]);
}

/**
 * Suppresses every shortcut while `open` -- what a dialog calls so its own
 * Escape/Enter handling isn't competing with the global layer.
 */
export function useModalKeyboardLock(open: boolean): void {
  const { acquireModalLock } = useKeyboard();
  useEffect(() => {
    if (!open) return;
    return acquireModalLock();
  }, [open, acquireModalLock]);
}
