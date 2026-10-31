import { useCallback, useEffect, useRef, useState } from "react";
import { Terminal, type ITheme } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import "@xterm/xterm/css/xterm.css";

import { FindBar, type FindBarHandle } from "@/components/find/find-bar";
import { usePaneFocusWithin } from "@/components/find/use-pane-focus-within";
import { Alert } from "@/components/ui/alert";
import { TerminalKeyBar, type TerminalModifier } from "@/components/terminal-key-bar";
import { useKeyboardInset } from "@/hooks/use-keyboard-inset";
import { useTheme } from "@/hooks/use-theme";
import { useActionHandler } from "@/keyboard/keyboard-provider";
import type { ConnectionStatus } from "@/lib/reconnect";
import { encodeTerminalKey, hasPendingModifiers, resolvePendingModifierData, terminalKeyInput } from "@/lib/terminal-keys";
import { loadTerminalScrollback } from "@/lib/terminal-prefs";
import {
  bindTerminal,
  boundTerminalId,
  clearTerminalActivity,
  markTerminalActivity,
  terminalIdsBoundElsewhere,
} from "@/lib/terminal-sessions";
import { resolveSearchDecorations, resolveTerminalTheme } from "@/lib/terminal-theme";
import type { WsClientLike } from "@/lib/ws-client";
import type {
  Task,
  TerminalAttachResult,
  TerminalCreateResult,
  TerminalDataEventParams,
  TerminalSessionStatus,
} from "@/lib/types";

/** Tracks one task-selection's lifetime: guards async continuations from a superseded selection, and lets an active terminal.attach be aborted (detached, not closed) on task switch or unmount -- same pattern as use-run-timeline.ts's Session. */
interface Session {
  cancelled: boolean;
  controller: AbortController;
}

/**
 * The minimal surface TerminalPane needs from a terminal emulator widget,
 * factored out (the same way lib/ws-client.ts's WsClientLike factors out
 * the WebSocket boundary) so this component's own wiring logic --
 * terminal.create/attach/write/resize, detach-on-unmount, error handling
 * -- is unit-testable without a real xterm.js instance, which needs
 * browser canvas/layout APIs jsdom doesn't implement (see this file's
 * test suite for exactly what that does and doesn't let component tests
 * exercise). createRealTerminal below is the production implementation;
 * a real xterm.Terminal + FitAddon pair satisfies this shape.
 */
export interface TerminalHandle {
  open(container: HTMLElement): void;
  onData(callback: (data: string) => void): { dispose(): void };
  onResize(callback: (size: { cols: number; rows: number }) => void): { dispose(): void };
  write(data: string | Uint8Array): void;
  /** Resizes the terminal to fit `container`'s current dimensions (wraps FitAddon.fit()); firing onResize if the size actually changed. */
  fit(): void;
  /** The terminal's current size in cells, sent once on attach -- the mount-time fit() fires onResize before any session id exists, so that resize never reaches the PTY. Optional for the same reason setTheme is. */
  size?(): { cols: number; rows: number };
  /** Gives the terminal keyboard focus, so a freshly opened or re-activated terminal takes typing without a click first. Optional for the same reason setTheme is. */
  focus?(): void;
  /** Re-applies the terminal's chrome colors (background/foreground/cursor/selection) -- optional so FakeTerminalHandle (this file's own test suite) doesn't need to implement it; xterm.js's own `options.theme` setter triggers a redraw. */
  setTheme?(theme: ITheme): void;
  /** The currently selected text, for the Copy affordance (Item 20). Optional for the same reason setTheme is. */
  getSelection?(): string;
  /** Fires whenever the selection changes, so Copy can be disabled when there's nothing to copy. A handle that can't report this leaves Copy enabled -- a silent no-op beats a permanently-dead button. */
  onSelectionChange?(callback: () => void): { dispose(): void };
  /** Find over the scrollback (AC3, `@xterm/addon-search`). Optional so FakeTerminalHandle (this file's own test suite) doesn't need it -- a handle without `find` never shows the Find affordance at all, never a dead shortcut. */
  find?: TerminalFindHandle;
  /** Whether the shell switched the cursor keys to application mode (SS3 arrows). Optional for the same reason setTheme is; absent means normal mode. */
  applicationCursorKeys?(): boolean;
  dispose(): void;
}

/** What `@xterm/addon-search`'s `onDidChangeResults` reports: the active match's 0-based index, or -1 while nothing is selected, and the total count. */
export interface TerminalFindResult {
  resultIndex: number;
  resultCount: number;
}

export interface TerminalFindHandle {
  /** Searches forward (default) or backward for `term`; an empty `term` clears the search instead of running it (xterm's own findNext/findPrevious no-op on "", which would leave stale decorations on screen). */
  search(term: string, direction?: "next" | "previous"): void;
  clear(): void;
  onDidChangeResults(callback: (result: TerminalFindResult) => void): { dispose(): void };
}

/**
 * xterm's own default is `courier-new, courier, monospace` at 15px, which renders as a bold serif
 * Courier in a UI that is otherwise Geist. The platform's UI monospace stack at 13px (Tailwind's
 * `font-mono` stack, i.e. SF Mono / Menlo on macOS) reads like every other terminal.
 */
const TERMINAL_FONT_FAMILY = 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace';
const TERMINAL_FONT_SIZE = 13;

function createRealTerminal({ scrollback }: { scrollback: number }): TerminalHandle {
  const term = new Terminal({
    convertEol: true,
    cursorBlink: true,
    scrollback,
    fontFamily: TERMINAL_FONT_FAMILY,
    fontSize: TERMINAL_FONT_SIZE,
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  const search = new SearchAddon();
  term.loadAddon(search);
  return {
    open: (container) => term.open(container),
    onData: (callback) => term.onData(callback),
    onResize: (callback) => term.onResize(callback),
    write: (data) => term.write(data),
    fit: () => fit.fit(),
    focus: () => term.focus(),
    size: () => ({ cols: term.cols, rows: term.rows }),
    setTheme: (theme) => {
      term.options.theme = theme;
    },
    getSelection: () => term.getSelection(),
    applicationCursorKeys: () => term.modes.applicationCursorKeysMode,
    onSelectionChange: (callback) => term.onSelectionChange(callback),
    find: {
      search: (query, direction) => {
        if (!query) {
          search.clearDecorations();
          return;
        }
        const options = { decorations: resolveSearchDecorations() };
        if (direction === "previous") search.findPrevious(query, options);
        else search.findNext(query, options);
      },
      clear: () => search.clearDecorations(),
      onDidChangeResults: (callback) => search.onDidChangeResults(callback),
    },
    dispose: () => term.dispose(),
  };
}

/** How long a container's size must go quiet before fit() re-runs -- see the ResizeObserver effect below. */
const RESIZE_DEBOUNCE_MS = 100;

function base64ToBytes(b64: string): Uint8Array {
  const binary = atob(b64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

/**
 * A self-contained, independently mountable terminal pane for one task: a
 * real, interactive shell (internal/terminal, PTY-backed) rendered via
 * xterm.js, wired to terminal.create/attach/write/resize. Deliberately not
 * wired into App.tsx/task-detail.tsx yet -- see
 * docs/plans/active/web-ui-terminal.md's Decisions for why (a file
 * explorer and a diff viewer are being built in parallel against those
 * same shell files).
 *
 * Lifecycle, mirroring use-run-timeline.ts's run.list-first pattern:
 * mounting (or the task changing, or App.tsx swapping in a new
 * post-reconnect client) calls terminal.list first to discover whether a
 * still-running session for this task already exists -- reusing it via
 * terminal.attach if so, or terminal.create-then-attach if not -- so a
 * reconnect's client-reference change resyncs onto the same session
 * instead of spawning a duplicate shell. Attach streams backfill then live
 * "data" events written straight into the terminal handle. Unmounting/
 * switching tasks aborts the attach's AbortSignal, which only detaches --
 * the shell keeps running server-side -- never calling terminal.close
 * itself; closing the terminal's *tab* is what ends the session (App.tsx's
 * TerminalTabReaper), mirroring
 * TaskDetailPane's Stop button going through run.stop rather than an
 * abort.
 *
 * One pane drives exactly one session, and which one is recorded in
 * lib/terminal-sessions.ts against this pane's *tab key* -- so Item 20's
 * several-terminals-per-task works: a second terminal tab will not adopt
 * the session a first tab is already driving, and a pane re-mounting
 * (tab switch, reconnect) lands back on its own session rather than on
 * "whichever is first in the list".
 *
 * Output arriving while this pane's tab is not the active one marks the
 * tab (Item 20's activity indicator); activating it clears the mark. That
 * requires the pane to stay mounted while its tab is in the background --
 * see App.tsx's forceMount on terminal tabs, and the plan's Item 20
 * decisions for why that is the right reading of the detach-not-close
 * contract.
 */
export function TerminalPane({
  client,
  task,
  tabKey,
  active = true,
  connectionStatus = "connected",
  createTerminal = createRealTerminal,
}: {
  client: WsClientLike | null;
  task: Task;
  /** This pane's tab key -- what its session binding and activity flag are recorded against. Defaults to the base terminal tab's key, so an existing single-terminal caller behaves exactly as before. */
  tabKey?: string;
  /** Whether this pane's tab is the one in front. Output arriving while false marks the tab; flipping to true clears the mark. Defaults true for a caller that has no tab strip. */
  active?: boolean;
  /** Real-time connection status from App.tsx -- see TaskDetailPane's identical prop for why. Defaults to "connected" so every existing caller/test keeps behaving exactly as before. */
  connectionStatus?: ConnectionStatus;
  /** Overridable for tests -- see TerminalHandle's doc comment. Defaults to a real xterm.js + FitAddon instance. */
  createTerminal?: (options: { scrollback: number }) => TerminalHandle;
}) {
  const key = tabKey ?? `${task.ID}:terminal`;
  const { resolved } = useTheme();
  const keyboardInset = useKeyboardInset();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<TerminalHandle | null>(null);
  const terminalIdRef = useRef<string | null>(null);
  const sessionRef = useRef<Session | null>(null);
  const [terminalId, setTerminalId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [endedStatus, setEndedStatus] = useState<"interrupted" | "closed" | null>(null);
  // Copy is disabled only when the handle can actually report "nothing is
  // selected" -- a handle without onSelectionChange leaves it enabled, so
  // the button is never permanently dead (see TerminalHandle).
  const [hasSelection, setHasSelection] = useState(false);
  const [selectionReportable, setSelectionReportable] = useState(false);
  const [pasteError, setPasteError] = useState<string | null>(null);
  // Sticky Ctrl/Shift/Alt of the touch key bar; the ref mirrors state for the onData closure.
  const [modifiers, setModifiers] = useState({ ctrl: false, shift: false, alt: false });
  const modifiersRef = useRef(modifiers);
  modifiersRef.current = modifiers;

  // Find (AC3): open/query/result live here (not a child component) so
  // every hook that touches termRef stays inside this component's own
  // effect ordering -- a separate `TerminalFindBar` subscribing to
  // `termRef.current?.find` would run its first effect *before* this
  // component's own terminal-creation effect (children's effects commit
  // before their parent's), and see a still-null handle.
  const [findOpen, setFindOpen] = useState(false);
  const [findQuery, setFindQuery] = useState("");
  const [findResult, setFindResult] = useState<TerminalFindResult>({ resultIndex: -1, resultCount: 0 });
  const findBarRef = useRef<FindBarHandle>(null);
  const { focused: findFocused, onFocus: onFindFocus, onBlur: onFindBlur } = usePaneFocusWithin();

  // Read at data-arrival time rather than captured by the attach closure,
  // which is created once per session and would otherwise pin whichever
  // value `active` had when the tab was opened.
  const activeRef = useRef(active);
  activeRef.current = active;

  // Looking at the tab is what clears its activity mark.
  useEffect(() => {
    if (active) clearTerminalActivity(key);
  }, [active, key]);

  // A tab that goes away (closed, or its task deselected) must not leave a
  // dot behind on a strip it's no longer in.
  useEffect(() => {
    return () => clearTerminalActivity(key);
  }, [key]);

  // Create the terminal handle once per mount, and dispose it on unmount.
  // This is independent of the terminal.create/attach lifecycle below --
  // the widget itself doesn't need a live session to exist (e.g. it can
  // render while terminal.create is still in flight).
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const term = createTerminal({ scrollback: loadTerminalScrollback() });
    term.open(container);
    try {
      term.fit();
    } catch {
      // No usable layout to fit to yet (jsdom, or a container that hasn't
      // been laid out) -- best-effort only, the terminal keeps its
      // default size.
    }
    term.setTheme?.(resolveTerminalTheme());
    termRef.current = term;

    return () => {
      term.dispose();
      termRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [createTerminal]);

  // Opening a terminal tab (or coming back to one) puts the keyboard in it: focus used to stay on the
  // button that opened the tab, so typing went nowhere until the user clicked into the terminal.
  useEffect(() => {
    if (active) termRef.current?.focus?.();
  }, [active]);

  // Re-applies the terminal's chrome whenever the app's resolved theme
  // changes (a live toggle, or the OS query flipping while on "system") --
  // unlike CodeMirror's var()-based styling, xterm's theme option needs
  // literal colors re-applied on every change (see lib/terminal-theme.ts's
  // doc comment).
  useEffect(() => {
    termRef.current?.setTheme?.(resolveTerminalTheme());
  }, [resolved]);

  // Resize the terminal to fit its container whenever the container's own
  // size changes (pane resize, browser window resize). fit()'s resulting
  // resize is what fires the onResize wiring below into terminal.resize.
  //
  // Debounced (settle-based, like Paseo's terminal-resize-debouncer):
  // ResizeObserver fires on every layout tick of a drag, and a continuous
  // drag crossing many character-cell boundaries would otherwise send a
  // terminal.resize RPC (and the PTY's SIGWINCH) once per animation frame
  // for as long as the drag lasts, instead of once it settles.
  useEffect(() => {
    const container = containerRef.current;
    if (!container || typeof ResizeObserver === "undefined") {
      // jsdom doesn't implement ResizeObserver; there's no real layout to
      // react to under it anyway (see this file's test suite for what
      // jsdom can and can't exercise here).
      return;
    }
    let timeout: ReturnType<typeof setTimeout> | null = null;
    const observer = new ResizeObserver(() => {
      if (timeout !== null) clearTimeout(timeout);
      timeout = setTimeout(() => {
        timeout = null;
        try {
          termRef.current?.fit();
        } catch {
          // Container may be transiently zero-sized mid-layout; the next
          // resize observation will retry.
        }
      }, RESIZE_DEBOUNCE_MS);
    });
    observer.observe(container);
    return () => {
      observer.disconnect();
      if (timeout !== null) clearTimeout(timeout);
    };
  }, []);

  // terminal.list then either terminal.attach (a still-running session for
  // this task already exists -- e.g. this effect re-running because
  // App.tsx swapped in a new post-reconnect client, task.ID unchanged) or
  // terminal.create then terminal.attach (no session yet -- first mount
  // for this task). See the component doc comment for the
  // detach-on-unmount contract.
  //
  // This list-before-create step is what makes reconnect resync safe:
  // without it, a client-reference change alone would re-run this effect
  // and call terminal.create again, spawning a duplicate shell next to the
  // one still running server-side (see
  // docs/plans/active/daemon-restart-resync.md's Acceptance Criteria).
  useEffect(() => {
    setError(null);
    setEndedStatus(null);
    setTerminalId(null);
    terminalIdRef.current = null;

    if (!client) {
      sessionRef.current = null;
      return;
    }

    const session: Session = { cancelled: false, controller: new AbortController() };
    sessionRef.current = session;
    // The session this *tab* already owns, if any. Switching tasks changes
    // `key`, so a different task's binding is never consulted; a reconnect
    // or a re-mount keeps the same key and therefore the same session.
    const previousId = boundTerminalId(key);

    function attach(id: string): void {
      bindTerminal(key, id);
      terminalIdRef.current = id;
      setTerminalId(id);

      // The mount-time fit() already sized the widget, but its onResize
      // fired before this id existed and was dropped -- without this the
      // PTY stays at its 80x24 default until the container next resizes
      // (badly garbled under ConPTY, which repaints to its own width).
      const size = termRef.current?.size?.();
      if (size) {
        client!.call("terminal.resize", { terminalId: id, ...size }).catch(() => {
          // Best-effort, same reasoning as terminal.resize below.
        });
      }

      client!
        .callStream<TerminalAttachResult>(
          "terminal.attach",
          { terminalId: id },
          (event, params) => {
            if (session.cancelled) return;
            if (event === "data") {
              const { data } = params as TerminalDataEventParams;
              termRef.current?.write(base64ToBytes(data));
              // Output you weren't looking at marks the tab (Item 20).
              if (!activeRef.current) markTerminalActivity(key);
            }
          },
          { signal: session.controller.signal },
        )
        .catch(() => {
          // Either our own detach (unmount/task switch, the expected
          // path) or the session closing server-side -- neither needs
          // its own error surface here; a still-mounted pane for the
          // same task whose session just closed simply stops receiving
          // output, which is visible in the terminal itself.
        });
    }

    client
      .call<TerminalSessionStatus[]>("terminal.list", { taskId: task.ID })
      .then((rawSessions) => {
        const sessions = rawSessions ?? [];
        if (session.cancelled) return;

        // Reconnecting to a session we were already attached to:
        // previousId names that *exact* session, and it must be resolved
        // by that id specifically -- never by "the first running session
        // in the list", which could silently swap this pane onto a
        // *different* session server-side if more than one happens to be
        // running for the task (e.g. one started from another tab). If
        // the daemon now reports our own session as no longer running,
        // that's a real, honest outcome (the daemon restarted
        // mid-session, or it was closed) -- render it distinctly instead
        // of silently attaching (which would just backfill scrollback
        // then immediately end, looking like a bare error) or silently
        // starting a fresh replacement shell. This is a reconnect, not a
        // fresh attach, so there is deliberately no fallback to "any
        // running session"/"create new" below when previousId is set.
        if (previousId) {
          const prev = sessions.find((s) => s.ID === previousId);
          if (prev && prev.Status === "running") {
            attach(prev.ID);
          } else {
            setEndedStatus(prev?.Status === "interrupted" ? "interrupted" : "closed");
          }
          return;
        }

        // No previousId: a genuinely fresh attach for this tab (first
        // mount for this task, not a reconnect) -- fine to reuse an
        // already-running session, *except* one another terminal tab is
        // already driving, which would render the same shell twice
        // (Item 20).
        const taken = terminalIdsBoundElsewhere(key);
        const existing = sessions.find((s) => s.Status === "running" && !taken.has(s.ID));
        if (existing) {
          attach(existing.ID);
          return;
        }

        client
          .call<TerminalCreateResult>("terminal.create", { taskId: task.ID })
          .then(({ terminalId: id }) => {
            if (session.cancelled) return;
            attach(id);
          })
          .catch((err: unknown) => {
            if (!session.cancelled) setError(err instanceof Error ? err.message : String(err));
          });
      })
      .catch((err: unknown) => {
        if (!session.cancelled) setError(err instanceof Error ? err.message : String(err));
      });

    return () => {
      session.cancelled = true;
      session.controller.abort();
      if (sessionRef.current === session) sessionRef.current = null;
    };
  }, [client, key, task.ID]);

  // Keystrokes/paste -> terminal.write. Wired once the terminal handle
  // exists; terminalIdRef (not React state) is read at call time so this
  // doesn't need to be re-subscribed every time terminalId changes.
  useEffect(() => {
    const term = termRef.current;
    if (!term || !client) return;
    const disposable = term.onData((raw) => {
      let data = raw;
      const pending = modifiersRef.current;
      if (hasPendingModifiers(pending)) {
        const resolved = resolvePendingModifierData({ data: raw, pending });
        if (resolved.mode === "key") {
          data = encodeTerminalKey(terminalKeyInput(resolved.key, pending), {
            applicationCursorKeys: term.applicationCursorKeys?.() ?? false,
          });
        }
        modifiersRef.current = { ctrl: false, shift: false, alt: false };
        setModifiers(modifiersRef.current);
      }
      const id = terminalIdRef.current;
      if (!id || !data) return;
      client.call("terminal.write", { terminalId: id, data }).catch(() => {
        // Best-effort: a write failing (e.g. the session just closed)
        // doesn't need its own UI treatment beyond what attach ending
        // already surfaces.
      });
    });
    return () => disposable.dispose();
  }, [client, createTerminal]);

  // The terminal's own resize (from fit() above, or any other resize) ->
  // terminal.resize, so the shell sees the real window size.
  useEffect(() => {
    const term = termRef.current;
    if (!term || !client) return;
    const disposable = term.onResize(({ cols, rows }) => {
      const id = terminalIdRef.current;
      if (!id) return;
      client.call("terminal.resize", { terminalId: id, cols, rows }).catch(() => {
        // Best-effort, same reasoning as terminal.write above.
      });
    });
    return () => disposable.dispose();
  }, [client, createTerminal]);

  // Selection tracking, for the Copy button's enabled state. A handle
  // that can't report it (the test fake) leaves Copy enabled.
  useEffect(() => {
    const term = termRef.current;
    if (!term?.onSelectionChange) return;
    setSelectionReportable(true);
    const disposable = term.onSelectionChange(() => {
      setHasSelection(Boolean(term.getSelection?.()));
    });
    return () => disposable.dispose();
  }, [createTerminal]);

  // Find (AC3): subscribes to the addon's own result-count/index reporting.
  // Declared after the terminal-creation effect above, so on first mount
  // termRef.current is already set by the time this one runs (React runs
  // one component's own effects in source order).
  useEffect(() => {
    const disposable = termRef.current?.find?.onDidChangeResults(setFindResult);
    return () => disposable?.dispose();
  }, [createTerminal]);

  const openFind = useCallback(() => {
    if (!termRef.current?.find) return;
    setFindOpen(true);
  }, []);
  useActionHandler("pane.find", openFind, { enabled: findFocused, menuFallback: true });

  useEffect(() => {
    if (findOpen) findBarRef.current?.focus();
  }, [findOpen]);

  const searchFind = useCallback((text: string, direction?: "next" | "previous") => {
    setFindQuery(text);
    termRef.current?.find?.search(text, direction);
  }, []);
  const nextFind = useCallback(() => searchFind(findQuery, "next"), [searchFind, findQuery]);
  const previousFind = useCallback(() => searchFind(findQuery, "previous"), [searchFind, findQuery]);
  const closeFind = useCallback(() => {
    termRef.current?.find?.clear();
    setFindOpen(false);
    setFindQuery("");
    setFindResult({ resultIndex: -1, resultCount: 0 });
    containerRef.current?.querySelector("textarea")?.focus();
  }, []);

  let findStatus = "";
  if (findQuery) {
    findStatus =
      findResult.resultCount === 0
        ? "No matches"
        : findResult.resultIndex < 0
          ? `${findResult.resultCount}`
          : `${findResult.resultIndex + 1}/${findResult.resultCount}`;
  }

  /**
   * Copy/paste without relying on the browser's own terminal-unfriendly
   * defaults (Ctrl+C is an interrupt in a shell, not a copy).
   *
   * Paste goes through terminal.write rather than into the emulator's
   * local buffer: the PTY is what echoes, so writing locally would show
   * the text twice and never send it.
   */
  async function handleCopy() {
    const text = termRef.current?.getSelection?.() ?? "";
    if (!text) return;
    try {
      await navigator.clipboard?.writeText(text);
    } catch {
      // Clipboard permission denied / no secure context -- the selection
      // is still there to copy by hand; nothing worth an error surface.
    }
  }

  async function handlePaste() {
    const id = terminalIdRef.current;
    if (!client || !id) return;
    setPasteError(null);
    try {
      const text = await navigator.clipboard?.readText();
      if (!text) return;
      await client.call("terminal.write", { terminalId: id, data: text });
    } catch (err) {
      // Unlike copy, a failed paste is invisible otherwise -- nothing
      // appears, and the user can't tell whether the shell ignored it.
      setPasteError(err instanceof Error ? err.message : String(err));
    }
  }

  function clearModifiers() {
    modifiersRef.current = { ctrl: false, shift: false, alt: false };
    setModifiers(modifiersRef.current);
  }

  function toggleModifier(modifier: TerminalModifier) {
    modifiersRef.current = { ...modifiersRef.current, [modifier]: !modifiersRef.current[modifier] };
    setModifiers(modifiersRef.current);
    termRef.current?.focus?.();
  }

  function sendKey(domKey: string) {
    const id = terminalIdRef.current;
    const data = encodeTerminalKey(terminalKeyInput(domKey, modifiersRef.current), {
      applicationCursorKeys: termRef.current?.applicationCursorKeys?.() ?? false,
    });
    if (client && id && data) {
      client.call("terminal.write", { terminalId: id, data }).catch(() => {
        // Best-effort, same reasoning as the onData wiring above.
      });
    }
    clearModifiers();
    termRef.current?.focus?.();
  }

  function toggleKeyboard() {
    const textarea = containerRef.current?.querySelector("textarea");
    if (textarea && textarea === document.activeElement) textarea.blur();
    else termRef.current?.focus?.();
  }

  return (
    <div className="flex h-full flex-col" onFocus={onFindFocus} onBlur={onFindBlur}>
      {(endedStatus || !terminalId) && (
        <p
          data-testid="terminal-status"
          className="border-b px-3 py-1 text-ui-sm text-foreground-subtle"
          title={terminalId ?? undefined}
        >
          {endedStatus ? `session ${endedStatus}` : "starting terminal…"}
        </p>
      )}
      {connectionStatus === "reconnecting" && (
        <Alert
          testId="connection-banner"
          variant="warning"
          className="rounded-none border-x-0 border-t-0"
          description="Connection lost -- reconnecting to daemon…"
        />
      )}
      {endedStatus === "interrupted" && (
        <p className="px-3 py-1 text-ui-sm text-foreground-subtle" data-testid="terminal-ended">
          session ended: daemon restarted
        </p>
      )}
      {error && (
        <p className="px-3 py-1 text-ui-sm text-destructive" data-testid="terminal-error">
          {error}
        </p>
      )}
      {pasteError && (
        <p className="px-3 py-1 text-ui-sm text-destructive" data-testid="terminal-paste-error">
          paste failed: {pasteError}
        </p>
      )}
      <div data-testid="terminal-inset" className="relative min-h-0 flex-1 bg-terminal-bg p-2">
        {/* Inset lives on this wrapper, never on the fit host: FitAddon ignores the host's own padding. */}
        <div ref={containerRef} data-testid="terminal-container" className="terminal-xterm-shell h-full" />
        {findOpen && (
          <div className="absolute top-2 right-3 z-10">
            <FindBar
              ref={findBarRef}
              query={findQuery}
              status={findStatus}
              canNavigate={findResult.resultCount > 0}
              onQueryChange={(text) => searchFind(text)}
              onNext={nextFind}
              onPrevious={previousFind}
              onClose={closeFind}
            />
          </div>
        )}
      </div>
      {/*
       * Touch has neither the copy/paste keyboard shortcuts nor a hardware keyboard's Esc/Tab/Ctrl/arrows,
       * so a port of Paseo's mobile key bar sits pinned at the bottom -- hidden on pointer-fine devices
       * (Tailwind 4's `pointer-coarse:` variant). The margin lifts it above the on-screen keyboard, and
       * the safe-area padding applies only while the keyboard is closed.
       */}
      <TerminalKeyBar
        modifiers={modifiers}
        onToggleModifier={toggleModifier}
        onKey={sendKey}
        onCopy={() => void handleCopy()}
        onPaste={() => void handlePaste()}
        onToggleKeyboard={toggleKeyboard}
        copyDisabled={!terminalId || (selectionReportable && !hasSelection)}
        pasteDisabled={!terminalId}
        style={{
          marginBottom: keyboardInset,
          paddingBottom: keyboardInset > 0 ? undefined : "env(safe-area-inset-bottom)",
        }}
      />
    </div>
  );
}
