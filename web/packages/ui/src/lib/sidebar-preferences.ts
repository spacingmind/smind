/**
 * Client-side persistence for the sidebar's pinned tasks, unread tasks and
 * group-by view choice (web-sidebar-attention plan's Decisions: "All
 * client-side state" -- no daemon or wire change). Same read/validate/write
 * contract as every other localStorage-backed preference (see lib/storage.ts's
 * doc comment): reads never throw and fall back to empty/default on anything
 * unexpected, writes are best-effort.
 */

import { readStored, writeStored, STORAGE_KEYS } from "@/lib/storage";

function isNumberArray(value: unknown): value is number[] {
  return Array.isArray(value) && value.every((v) => typeof v === "number");
}

/** Reads the persisted set of pinned task ids, or an empty set if unset/corrupt. */
export function readPinnedTasks(): Set<number> {
  return new Set(readStored<number[]>(STORAGE_KEYS.pinnedTasks, [], isNumberArray));
}

export function writePinnedTasks(pinned: ReadonlySet<number>): void {
  writeStored(STORAGE_KEYS.pinnedTasks, [...pinned]);
}

/** Reads the persisted set of unread task ids, or an empty set if unset/corrupt. */
export function readUnreadTasks(): Set<number> {
  return new Set(readStored<number[]>(STORAGE_KEYS.unreadTasks, [], isNumberArray));
}

export function writeUnreadTasks(unread: ReadonlySet<number>): void {
  writeStored(STORAGE_KEYS.unreadTasks, [...unread]);
}

/** Reads the persisted set of collapsed-parent task ids (task tree view, PR-U1), or an empty set if unset/corrupt. */
export function readCollapsedTasks(): Set<number> {
  return new Set(readStored<number[]>(STORAGE_KEYS.collapsedTasks, [], isNumberArray));
}

export function writeCollapsedTasks(collapsed: ReadonlySet<number>): void {
  writeStored(STORAGE_KEYS.collapsedTasks, [...collapsed]);
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((v) => typeof v === "string");
}

/**
 * The ids of the sidebar containers the user has collapsed: `ws:<id>`,
 * `space:<id>`, `ungrouped:<workspaceId>`, `pinned`, `status:<group key>`, `stage:<group key>`.
 * Collapsed (not expanded) is what is stored, so a container that did not
 * exist yet -- a new workspace, a new space -- is expanded by default without
 * anything having to be written for it. Unset/corrupt reads as "nothing
 * collapsed".
 */
export function readCollapsedNodes(): Set<string> {
  return new Set(readStored<string[]>(STORAGE_KEYS.collapsedSidebarNodes, [], isStringArray));
}

export function writeCollapsedNodes(collapsed: ReadonlySet<string>): void {
  // An empty set removes the key rather than persisting "[]".
  writeStored(STORAGE_KEYS.collapsedSidebarNodes, collapsed.size === 0 ? undefined : [...collapsed]);
}

/** The sidebar's task-list views: the default grouped workspace/space tree, a flat group-by-status view (AC6), or a flat group-by-stage view (working / ready to review / done). */
export type SidebarGroupMode = "tree" | "status" | "stage";

function isSidebarGroupMode(value: unknown): value is SidebarGroupMode {
  return value === "tree" || value === "status" || value === "stage";
}

export function readSidebarGroupMode(): SidebarGroupMode {
  return readStored<SidebarGroupMode>(STORAGE_KEYS.sidebarGroupMode, "tree", isSidebarGroupMode);
}

export function writeSidebarGroupMode(mode: SidebarGroupMode): void {
  writeStored(STORAGE_KEYS.sidebarGroupMode, mode);
}

/** Whether the Notifications settings section's sound toggle is on -- off by default (plan's Decisions). */
export function readNotificationSoundEnabled(): boolean {
  return readStored<boolean>(STORAGE_KEYS.notificationSound, false, (v): v is boolean => typeof v === "boolean");
}

export function writeNotificationSoundEnabled(enabled: boolean): void {
  writeStored(STORAGE_KEYS.notificationSound, enabled);
}
