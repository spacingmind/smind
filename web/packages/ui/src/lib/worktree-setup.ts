import type { Task, Workspace, WorkspaceWithScripts } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/** Loads a workspace including its setup/teardown script text (workspace.get is the only method that returns it, ADR-0024). */
export function getWorkspaceScripts(client: WsClientLike, workspaceId: number): Promise<WorkspaceWithScripts> {
  return client.call<WorkspaceWithScripts>("workspace.get", { id: workspaceId });
}

/** Saves both scripts; an empty string clears one (workspace.setScripts keeps a script only when its key is omitted). */
export function setWorkspaceScripts(
  client: WsClientLike,
  workspaceId: number,
  scripts: { setupScript: string; teardownScript: string },
): Promise<Workspace> {
  return client.call<Workspace>("workspace.setScripts", { workspaceId, ...scripts });
}

/** Re-runs the workspace's setup script on a finished task; resolves with the task, already back in "running". */
export function retryTaskSetup(client: WsClientLike, taskId: number): Promise<Task> {
  return client.call<Task>("task.retrySetup", { id: taskId });
}
