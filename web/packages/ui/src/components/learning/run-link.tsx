import { createContext, useContext, type ReactNode } from "react";

import { useRunLookup } from "@/hooks/use-learning";
import { formatRoute } from "@/lib/route";

/** What a run link needs from its host: how to leave Settings. Absent (a bare render), a link only updates the hash. */
const RunLinkContext = createContext<{ closeSettings?: () => void }>({});
export const RunLinkProvider = ({ closeSettings, children }: { closeSettings?: () => void; children: ReactNode }) => (
  <RunLinkContext.Provider value={{ closeSettings }}>{children}</RunLinkContext.Provider>
);

/** "run-7f3a…" -> a short label; ids are opaque text. */
function shortRunId(runId: string): string {
  return runId.length > 10 ? `${runId.slice(0, 8)}…` : runId;
}

/**
 * A cited run (and the event seq in its log): a link to the chat the run belongs to. The app has
 * no per-event anchor in a chat, so the link opens the chat and the seq is shown as a label
 * (seq -1 is the run's prompt record). A run the registry no longer knows is shown unlinked.
 */
export function RunLink({ runId, seq, workspaceId }: { runId: string; seq?: number; workspaceId: number }) {
  const { closeSettings } = useContext(RunLinkContext);
  const run = useRunLookup()(runId);
  const where = seq === undefined ? "" : seq < 0 ? " · prompt" : ` · event ${seq}`;
  const label = `run ${shortRunId(runId)}${where}`;
  if (!run) {
    return (
      <span data-testid="run-link-unresolved" className="text-ui-sm text-foreground-subtle">
        {label}
      </span>
    );
  }
  return (
    <a
      data-testid="run-link"
      href={formatRoute({ workspaceId, taskId: run.TaskID, tab: { kind: "chat", chatId: run.ChatID } })}
      className="text-ui-sm text-foreground underline underline-offset-2 hover:text-foreground-muted"
      onClick={(e) => {
        // The hash route restores the task and chat; Settings covers the shell, so it must close first.
        e.preventDefault();
        window.location.hash = formatRoute({ workspaceId, taskId: run.TaskID, tab: { kind: "chat", chatId: run.ChatID } });
        closeSettings?.();
      }}
    >
      {label}
    </a>
  );
}
