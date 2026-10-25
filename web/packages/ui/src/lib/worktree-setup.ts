import type { Task, Workspace, WorkspaceDefaultBase, WorkspaceWithScripts } from "@/lib/types";
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

/** Loads the workspace's effective default base branch (ADR-0039): the stored override, or the auto-detected ref when none. */
export function getWorkspaceDefaultBase(client: WsClientLike, workspaceId: number): Promise<WorkspaceDefaultBase> {
  return client.call<WorkspaceDefaultBase>("workspace.defaultBase", { workspaceId });
}

/** Sets/clears the default base override ("" clears, back to auto-detect); resolves with the state after the change. */
export function setWorkspaceDefaultBase(
  client: WsClientLike,
  workspaceId: number,
  branch: string,
): Promise<WorkspaceDefaultBase> {
  return client.call<WorkspaceDefaultBase>("workspace.setDefaultBase", { workspaceId, branch });
}

/** Sets/clears the workspace's commit-message style guidance (G17); resolves with the updated workspace. */
export function setWorkspaceCommitMessageStyle(
  client: WsClientLike,
  workspaceId: number,
  style: string,
): Promise<Workspace> {
  return client.call<Workspace>("workspace.setCommitMessageStyle", { workspaceId, style });
}

/** Re-runs the workspace's setup script on a finished task; resolves with the task, already back in "running". */
export function retryTaskSetup(client: WsClientLike, taskId: number): Promise<Task> {
  return client.call<Task>("task.retrySetup", { id: taskId });
}
