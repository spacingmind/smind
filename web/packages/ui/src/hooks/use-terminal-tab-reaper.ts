import { useEffect, useRef } from "react";

import { collectAllTabs, type TaskLayout } from "@/lib/split-tree";
import { boundTerminalId, unbindTerminal } from "@/lib/terminal-sessions";
import type { WsClientLike } from "@/lib/ws-client";

/**
 * Ends a terminal tab's PTY session when the *tab* goes away (its x in the
 * strip) -- across every task's layout, since a terminal can be closed in
 * a task the user is not looking at. The pane itself never calls
 * terminal.close (its unmount only detaches: switching tabs or tasks must
 * keep the shell running), so this diff against the previous run's tab
 * keys is the one place a disappearing terminal tab maps to a closing
 * session. First run only records; tasks absent from the map are left
 * alone.
 */
export function useTerminalTabReaper({
  client,
  tabsByTask,
}: {
  client: WsClientLike | null;
  tabsByTask: ReadonlyMap<number, TaskLayout>;
}): void {
  const previousRef = useRef<Map<number, Set<string>>>(new Map());

  useEffect(() => {
    const previous = previousRef.current;
    for (const [taskId, layout] of tabsByTask) {
      const current = new Set(
        collectAllTabs(layout.root)
          .filter((tab) => tab.kind === "terminal")
          .map((tab) => tab.key),
      );
      const seen = previous.get(taskId);
      if (seen) {
        for (const key of seen) {
          if (current.has(key)) continue;
          const terminalId = boundTerminalId(key);
          if (terminalId === null) continue;
          void client?.call("terminal.close", { terminalId }).catch(() => {
            // Already closed server-side (or the daemon is gone) -- the
            // tab is going away either way; forget the binding below.
          });
          unbindTerminal(key);
        }
      }
      previous.set(taskId, current);
    }
  }, [client, tabsByTask]);
}

/** Renders nothing: closes disappearing terminal tabs' sessions wherever App.tsx mounts it. */
export function TerminalTabReaper({
  client,
  tabsByTask,
}: {
  client: WsClientLike | null;
  tabsByTask: ReadonlyMap<number, TaskLayout>;
}) {
  useTerminalTabReaper({ client, tabsByTask });
  return null;
}
