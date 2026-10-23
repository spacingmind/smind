import { useMutation, useQueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { Task } from "@/lib/types";
import { retryTaskSetup } from "@/lib/worktree-setup";

/**
 * `task.retrySetup` {id}: runs the setup script and, for a task from a fork PR, clears its untrusted flag (only a user
 * action does, ADR-0039 §7). Resolves with the task (back in "running") and refreshes the sidebar tree and git chips.
 */
export function useRetrySetup() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation<Task, Error, number>({
    mutationFn: async (taskId) => {
      if (!client) throw new Error("no daemon connection");
      return retryTaskSetup(client, taskId);
    },
    onSuccess: (task) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.workspaceTree() });
      void queryClient.invalidateQueries({ queryKey: queryKeys.taskGit(task.ID) });
    },
  });
}
