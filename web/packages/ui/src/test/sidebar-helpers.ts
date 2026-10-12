import { act } from "react";
import { fireEvent, screen, within } from "@testing-library/react";

/** The per-row wrapper that owns a task row's hover/focus/menu state (see hooks/use-row-active.ts). */
export function taskItem(taskId: number): HTMLElement {
  const el = document.querySelector<HTMLElement>(`[data-testid="sidebar-task-item"][data-task-id="${taskId}"]`);
  if (!el) throw new Error(`no task row wrapper for task ${taskId}`);
  return el;
}

/** Hovers a task row so its hover-mounted ⋯ trigger exists. */
export function hoverTask(taskId: number): void {
  fireEvent.mouseEnter(taskItem(taskId));
}

export function unhoverTask(taskId: number): void {
  fireEvent.mouseLeave(taskItem(taskId));
}

/** Hovers the row of the task with this title and returns its ⋯ trigger. */
export async function revealTaskActions(title: string): Promise<HTMLElement> {
  const row = await screen.findByText(title);
  const item = row.closest<HTMLElement>('[data-testid="sidebar-task-item"]');
  if (!item) throw new Error(`"${title}" has no task row wrapper`);
  await act(async () => {
    fireEvent.mouseEnter(item);
  });
  return screen.findByRole("button", { name: `Actions for ${title}` });
}

/** Hovers the (first) row of a task and returns its ⋯ trigger. */
export function taskTrigger(taskId: number): HTMLElement {
  const item = taskItem(taskId);
  fireEvent.mouseEnter(item);
  return within(item).getByTestId("sidebar-task-actions-trigger");
}
