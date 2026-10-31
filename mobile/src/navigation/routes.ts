// routes.ts builds and parses the task-detail route (ADR-0036 decision 4):
// route params are strings, so the id is parsed once here.

// `title` is only a display hint for the header (a deep link can carry any text); nothing trusts it.
export type TaskRouteParams = { id: string; title: string };

export function taskHref(task: { ID: number; Title: string }): { pathname: '/task/[id]'; params: TaskRouteParams } {
  return { pathname: '/task/[id]', params: { id: String(task.ID), title: task.Title } };
}

type Param = string | string[] | undefined;

function first(p: Param): string | undefined {
  return Array.isArray(p) ? p[0] : p;
}

export function parseTaskParams(params: { id?: Param; title?: Param }): { ok: true; id: number; title: string } | { ok: false } {
  const rawId = first(params.id);
  if (rawId === undefined || !/^\d+$/.test(rawId)) return { ok: false };
  const id = Number(rawId);
  if (!Number.isSafeInteger(id) || id <= 0) return { ok: false };
  const title = first(params.title);
  return { ok: true, id, title: title ? title : `Task ${id}` };
}

/**
 * The task screen's header title straight from the route params, so the header is right on the first
 * frame of the push (a screen-level `Stack.Screen` only applies after the screen has rendered).
 */
export function taskHeaderTitle(params: { id?: Param; title?: Param } | undefined): string {
  const parsed = parseTaskParams(params ?? {});
  return parsed.ok ? parsed.title : 'Task';
}

// The create-a-task form's route (a const, no logic).
export const NEW_TASK_HREF = '/new-task' as const;

// The read-only plugin screens (ADR-0042 PA.5): the list, and one plugin's detail by id.
export const PLUGINS_HREF = '/plugins' as const;

export function pluginHref(id: string): { pathname: '/plugin/[id]'; params: { id: string } } {
  return { pathname: '/plugin/[id]', params: { id } };
}

/** The plugin id from route params; an id is a route string only, so anything empty is refused. */
export function parsePluginParams(params: { id?: Param }): { ok: true; id: string } | { ok: false } {
  const id = first(params.id);
  return id !== undefined && id.trim() !== '' && id.length <= 128 ? { ok: true, id } : { ok: false };
}

// The read-only automation screens (ADR-0044 section 10): the list, and one automation's run history by numeric id.
export const AUTOMATIONS_HREF = '/automations' as const;

export function automationHref(id: number): { pathname: '/automation/[id]'; params: { id: string } } {
  return { pathname: '/automation/[id]', params: { id: String(id) } };
}

export function parseAutomationParams(params: { id?: Param }): { ok: true; id: number } | { ok: false } {
  const raw = first(params.id);
  if (raw === undefined || !/^\d+$/.test(raw)) return { ok: false };
  const id = Number(raw);
  return Number.isSafeInteger(id) && id > 0 ? { ok: true, id } : { ok: false };
}

// The Human inbox (ADR-0045 section 5).
export const INBOX_HREF = '/inbox' as const;

// The read-only Hosts screen (ADR-0048 F1c); `focus` (a hostId) opens that pending host's approval panel.
export const HOSTS_HREF = '/hosts' as const;
