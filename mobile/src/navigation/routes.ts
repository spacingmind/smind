// routes.ts builds and parses the task-detail route (ADR-0036 decision 4):
// route params are strings, so the id is parsed once here.

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
