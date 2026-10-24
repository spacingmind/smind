import { useMutation, useQueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { Task } from "@/lib/types";

/**
 * `task.setBudget` {taskId, budgetUsd}: sets the tree budget of a ROOT task, `null` clears it (ADR-0041 A5). Resolves with
 * the task (budget fields included) and writes it into the shared budget entry, so the header chip updates at once.
 * Daemon refusals (not a root, bad amount) come back as ordinary errors for the caller to show inline.
 */
export function useSetTaskBudget() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation<Task, Error, { taskId: number; budgetUsd: number | null }>({
    mutationFn: async ({ taskId, budgetUsd }) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<Task>("task.setBudget", { taskId, budgetUsd });
    },
    onSuccess: (task) => {
      queryClient.setQueryData(queryKeys.taskBudget(task.ID), task);
      void queryClient.invalidateQueries({ queryKey: queryKeys.taskBudget(task.ID) });
    },
  });
}
