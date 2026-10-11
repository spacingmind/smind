import { useEffect, useState } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { Task } from "@/lib/types";

/**
 * `task` kept current with the daemon's `task.updated` events. App.tsx
 * hands panes a task snapshot taken at selection time, so a field that
 * changes later -- worktree setup moving running -> ok/failed -- would
 * never reach the pane without this. A different task prop (selection
 * change, or a fresher snapshot from the parent) always wins over a
 * previously received event.
 */
export function useLiveTask(task: Task, events: DaemonEvents | null | undefined): Task {
  const [live, setLive] = useState<Task | null>(null);

  useEffect(() => {
    setLive(null);
    if (!events) return;
    return events.subscribe("task.updated", (payload) => {
      const next = (payload as { task?: Task } | null)?.task;
      if (next && next.ID === task.ID) setLive(next);
    });
  }, [events, task]);

  return live && live.ID === task.ID ? live : task;
}
