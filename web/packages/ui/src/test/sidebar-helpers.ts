import { act } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { vi } from "vitest";
import type { TaskGit } from "@/lib/types";

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

/** Opens a task row's hover card (Radix opens it after a 400ms hover delay) and returns it. */
export async function openHoverCard(taskId: number): Promise<HTMLElement> {
  const row = within(taskItem(taskId)).getByTestId("sidebar-task-row");
  vi.useFakeTimers();
  try {
    fireEvent.pointerEnter(row, { pointerId: 1, pointerType: "mouse" });
    act(() => {
      vi.advanceTimersByTime(500);
    });
  } finally {
    vi.useRealTimers();
  }
  return screen.findByTestId("task-hover-card");
}

/** Hovers a container header (workspace/space) -- its wrapper is the row button's parent -- so its ⋯ mounts. */
function hoverHeader(header: HTMLElement): void {
  fireEvent.mouseEnter(header.parentElement!);
}

/** Hovers the workspace header row and returns its ⋯ trigger. */
export async function revealWorkspaceActions(): Promise<HTMLElement> {
  hoverHeader(await screen.findByTestId("sidebar-workspace-row"));
  return screen.findByTestId("sidebar-workspace-actions-trigger");
}

/** Hovers a space header row and returns its ⋯ trigger. */
export async function revealSpaceActions(): Promise<HTMLElement> {
  hoverHeader(await screen.findByTestId("sidebar-space-row"));
  return screen.findByTestId("sidebar-space-actions-trigger");
}

/**
 * Moves focus the way Tab does. jsdom only reports `:focus-visible` after the
 * last input was a key press, so a bare `.focus()` after an earlier pointer
 * event in the same file reads as mouse focus and would not mount the row's
 * keyboard-revealed actions.
 */
export function keyboardFocus(el: HTMLElement): void {
  fireEvent.keyDown(document.body, { key: "Tab" });
  act(() => el.focus());
}

/** A `task.gitList` item: the task's branch and diff size (ADR-0039); `over` adds PR/conflict/etc. */
export function gitItem(
  taskId: number,
  branch: string,
  files: number,
  insertions: number,
  deletions: number,
  over: Partial<TaskGit> = {},
): TaskGit {
  return {
    taskId,
    branch: { name: branch, owned: true },
    diff: { files, insertions, deletions },
    untrusted: false,
    computedAt: "2026-10-22T00:00:00Z",
    ...over,
  };
}
