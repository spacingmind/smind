import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

import { Button } from "@/components/ui/button";
import type { WsClientLike } from "@/lib/ws-client";
import type { Task, TerminalAttachResult, TerminalCreateResult, TerminalDataEventParams } from "@/lib/types";

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
  dispose(): void;
}

function createRealTerminal(): TerminalHandle {
  const term = new Terminal({ convertEol: true, cursorBlink: true });
  const fit = new FitAddon();
  term.loadAddon(fit);
  return {
    open: (container) => term.open(container),
    onData: (callback) => term.onData(callback),
    onResize: (callback) => term.onResize(callback),
    write: (data) => term.write(data),
    fit: () => fit.fit(),
    dispose: () => term.dispose(),
  };
}

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
 * Lifecycle, mirroring use-run-timeline.ts's run.start/run.attach split:
 * mounting (or the task changing) calls terminal.create once, then
 * immediately begins streaming it via terminal.attach (backfill, then
 * live "data" events written straight into the terminal handle).
 * Unmounting/switching tasks aborts the attach's AbortSignal, which only
 * detaches -- the shell keeps running server-side -- never calling
 * terminal.close itself; only the explicit "Close terminal" button does
 * that, mirroring TaskDetailPane's Stop button going through run.stop
 * rather than an abort.
 *
 * Only one session is driven at a time (the backend's terminal.list/
 * multi-session support exists but this component doesn't call it this
 * pass -- see the plan's Decisions, "explicitly out of scope").
 */
export function TerminalPane({
  client,
  task,
  createTerminal = createRealTerminal,
}: {
  client: WsClientLike | null;
  task: Task;
  /** Overridable for tests -- see TerminalHandle's doc comment. Defaults to a real xterm.js + FitAddon instance. */
  createTerminal?: () => TerminalHandle;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<TerminalHandle | null>(null);
  const terminalIdRef = useRef<string | null>(null);
  const sessionRef = useRef<Session | null>(null);

  const [terminalId, setTerminalId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [closing, setClosing] = useState(false);

  // Create the terminal handle once per mount, and dispose it on unmount.
  // This is independent of the terminal.create/attach lifecycle below --
  // the widget itself doesn't need a live session to exist (e.g. it can
  // render while terminal.create is still in flight).
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const term = createTerminal();
    term.open(container);
    try {
      term.fit();
    } catch {
      // No usable layout to fit to yet (jsdom, or a container that hasn't
      // been laid out) -- best-effort only, the terminal keeps its
      // default size.
    }
    termRef.current = term;

    return () => {
      term.dispose();
      termRef.current = null;
    };
  }, [createTerminal]);

  // Resize the terminal to fit its container whenever the container's own
  // size changes (pane resize, browser window resize). fit()'s resulting
  // resize is what fires the onResize wiring below into terminal.resize.
  useEffect(() => {
    const container = containerRef.current;
    if (!container || typeof ResizeObserver === "undefined") {
      // jsdom doesn't implement ResizeObserver; there's no real layout to
      // react to under it anyway (see this file's test suite for what
      // jsdom can and can't exercise here).
      return;
    }
    const observer = new ResizeObserver(() => {
      try {
        termRef.current?.fit();
      } catch {
        // Container may be transiently zero-sized mid-layout; the next
        // resize observation will retry.
      }
    });
    observer.observe(container);
    return () => observer.disconnect();
  }, []);

  // terminal.create then terminal.attach: backfill + live "data" events
  // written straight into the terminal handle. See the component doc
  // comment for the detach-on-unmount contract.
  useEffect(() => {
    setError(null);
    setTerminalId(null);
    terminalIdRef.current = null;

    if (!client) {
      sessionRef.current = null;
      return;
    }

    const session: Session = { cancelled: false, controller: new AbortController() };
    sessionRef.current = session;

    client
      .call<TerminalCreateResult>("terminal.create", { taskId: task.ID })
      .then(({ terminalId: id }) => {
        if (session.cancelled) return;
        terminalIdRef.current = id;
        setTerminalId(id);

        client
          .callStream<TerminalAttachResult>(
            "terminal.attach",
            { terminalId: id },
            (event, params) => {
              if (session.cancelled) return;
              if (event === "data") {
                const { data } = params as TerminalDataEventParams;
                termRef.current?.write(base64ToBytes(data));
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
      })
      .catch((err: unknown) => {
        if (!session.cancelled) setError(err instanceof Error ? err.message : String(err));
      });

    return () => {
      session.cancelled = true;
      session.controller.abort();
      if (sessionRef.current === session) sessionRef.current = null;
    };
  }, [client, task.ID]);

  // Keystrokes/paste -> terminal.write. Wired once the terminal handle
  // exists; terminalIdRef (not React state) is read at call time so this
  // doesn't need to be re-subscribed every time terminalId changes.
  useEffect(() => {
    const term = termRef.current;
    if (!term || !client) return;
    const disposable = term.onData((data) => {
      const id = terminalIdRef.current;
      if (!id) return;
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

  async function handleClose() {
    const id = terminalIdRef.current;
    if (!client || !id) return;
    setClosing(true);
    try {
      await client.call("terminal.close", { terminalId: id });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setClosing(false);
    }
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between border-b px-3 py-2 text-xs text-muted-foreground">
        <span data-testid="terminal-status">{terminalId ? `terminal ${terminalId}` : "starting terminal…"}</span>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-6 px-2 text-xs"
          disabled={!terminalId || closing}
          onClick={handleClose}
        >
          Close terminal
        </Button>
      </div>
      {error && (
        <p className="px-3 py-1 text-xs text-destructive" data-testid="terminal-error">
          {error}
        </p>
      )}
      <div ref={containerRef} data-testid="terminal-container" className="min-h-0 flex-1" />
    </div>
  );
}
