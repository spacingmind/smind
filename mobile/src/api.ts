// api.ts is the typed façade over RelayConnection the screens use: plain
// async functions + minimal wire-shaped types, kept out of the components
// so they can be unit-tested against a fake connection (the same fake-
// transport discipline Milestone 1's crypto tests used).
//
// Field names match the daemon's JSON exactly: internal/store's structs
// carry no json tags, so Go field names are the wire keys verbatim (see
// store/types.go), and internal/runs.RunStatus likewise.

import { RelayConnection } from './relay/RelayConnection';

export interface Workspace {
  ID: number;
  Path: string;
  Title: string;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface Space {
  ID: number;
  WorkspaceID: number;
  Title: string;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface Task {
  ID: number;
  WorkspaceID: number;
  SpaceID: number | null;
  Title: string;
  Status: string;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface RunSummary {
  ID: string;
  TaskID: number;
  Provider: string;
  Prompt: string;
  Status: 'running' | 'done' | 'error' | 'stopped' | 'interrupted';
  StartedAt: string;
  FinishedAt: string | null;
}

/** workspace.list: every workspace this daemon tracks. */
export async function listWorkspaces(conn: RelayConnection): Promise<Workspace[]> {
  return (await conn.call('workspace.list')) as Workspace[];
}

/** The spaces and tasks of one workspace, in one call each. */
export async function fetchOverview(
  conn: RelayConnection,
  workspaceId: number,
): Promise<{ spaces: Space[]; tasks: Task[] }> {
  const [spaces, tasks] = await Promise.all([
    conn.call('space.list', { workspaceId }) as Promise<Space[]>,
    conn.call('task.list', { workspaceId }) as Promise<Task[]>,
  ]);
  return { spaces, tasks };
}

/** run.list, filtered to one task; most recent first (the daemon's own ordering). */
export async function listRunsForTask(conn: RelayConnection, taskId: number): Promise<RunSummary[]> {
  const all = (await conn.call('run.list')) as RunSummary[];
  return all.filter((r) => r.TaskID === taskId);
}
