// newTask.ts is M6's pure submit logic for "create a task from the
// phone": validate -> task.create -> (optional first prompt) run.start,
// clamped and trimmed per the plan's payload hygiene, never throwing --
// every failure comes back as a SubmitResult the screen renders (the
// partial failure keeps the created task so a retry only re-runs run.start,
// never a second task). singleFlight guards the Create button against
// double taps. No react-native import: vitest covers it with a fake
// connection (the followUpPrompt discipline).

import { ProviderInfo, RunStartResult, StartMode, Task, createTask, startFirstRun } from './api';
import { RelayConnection } from './relay/RelayConnection';
import { wireModelMode } from './providerCatalog';

export const TITLE_MAX = 200;
export const PROMPT_MAX = 100_000;

export interface NewTaskInput {
  workspaceId: number;
  spaceId: number | null;
  title: string;
  prompt: string;
  provider: string;
  model: string;
  mode: string;
  start: StartMode;
}

export function clampTitle(s: string): string {
  return s.slice(0, TITLE_MAX);
}

export function clampPrompt(s: string): string {
  return s.slice(0, PROMPT_MAX);
}

export type NewTaskIssue = 'title-empty' | 'workspace-missing' | 'provider-missing';

/** title-empty for a blank title; workspace-missing for no workspace; provider-missing only when a prompt needs one. */
export function validateNewTask(input: NewTaskInput): NewTaskIssue[] {
  const issues: NewTaskIssue[] = [];
  if (input.title.trim().length === 0) issues.push('title-empty');
  if (input.workspaceId <= 0) issues.push('workspace-missing');
  if (input.prompt.trim().length > 0 && input.provider.trim().length === 0) issues.push('provider-missing');
  return issues;
}

export type SubmitResult =
  | { kind: 'ok'; task: Task; run?: RunStartResult }
  | { kind: 'invalid'; issues: NewTaskIssue[] }
  | { kind: 'create-failed'; message: string }
  | { kind: 'run-failed'; task: Task; message: string };

function errMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/**
 * The form's submit: validate (invalid -> no calls), task.create with the
 * title trimmed and clamped, then -- only for a non-empty prompt --
 * run.start on the new task. Never throws.
 */
export async function submitNewTask(conn: RelayConnection, input: NewTaskInput, providers: ProviderInfo[]): Promise<SubmitResult> {
  const issues = validateNewTask(input);
  if (issues.length > 0) return { kind: 'invalid', issues };
  let task: Task;
  try {
    task = await createTask(conn, {
      workspaceId: input.workspaceId,
      spaceId: input.spaceId,
      title: clampTitle(input.title.trim()),
      start: input.start,
    });
  } catch (e) {
    return { kind: 'create-failed', message: errMessage(e) };
  }
  if (input.prompt.trim().length === 0) return { kind: 'ok', task };
  const catalogEntry = providers.find((p) => p.id === input.provider);
  try {
    const run = await startFirstRun(conn, {
      taskId: task.ID,
      provider: input.provider,
      prompt: clampPrompt(input.prompt),
      ...wireModelMode(catalogEntry, { provider: input.provider, model: input.model, mode: input.mode }),
    });
    return { kind: 'ok', task, run };
  } catch (e) {
    return { kind: 'run-failed', task, message: errMessage(e) };
  }
}

/** Retry after run-failed: only run.start on the already-created task; never task.create. */
export async function retryFirstRun(conn: RelayConnection, task: Task, input: NewTaskInput, providers: ProviderInfo[]): Promise<SubmitResult> {
  const catalogEntry = providers.find((p) => p.id === input.provider);
  try {
    const run = await startFirstRun(conn, {
      taskId: task.ID,
      provider: input.provider,
      prompt: clampPrompt(input.prompt),
      ...wireModelMode(catalogEntry, { provider: input.provider, model: input.model, mode: input.mode }),
    });
    return { kind: 'ok', task, run };
  } catch (e) {
    return { kind: 'run-failed', task, message: errMessage(e) };
  }
}

/**
 * Double-tap guard: while one call is in flight, later calls return the
 * same promise's undefined (a no-op), so the underlying action runs once.
 */
export function singleFlight<A extends unknown[], R>(fn: (...a: A) => Promise<R>): (...a: A) => Promise<R | undefined> {
  let inFlight: Promise<R | undefined> | null = null;
  return (...a: A) => {
    if (inFlight) return inFlight.then(() => undefined);
    inFlight = fn(...a).finally(() => {
      inFlight = null;
    });
    return inFlight;
  };
}
